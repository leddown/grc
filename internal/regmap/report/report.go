// Package report renders the crosswalk as Markdown, JSON or CSV. It is
// framework-agnostic: everything framework-specific comes in through Input.
package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"grc/internal/regmap/mapping"
	"grc/internal/regmap/nist"
	"grc/internal/regmap/requirement"
)

// Input is everything the renderer needs.
type Input struct {
	Framework    string
	DisplayName  string
	SourceRef    string
	SourceFile   string
	GeneratedAt  string
	ApprovedOnly bool

	Requirements requirement.Set
	Mappings     mapping.Set
	Catalog      *nist.Catalog
}

// Row pairs a requirement with one of its mapping entries.
type Row struct {
	Requirement requirement.Requirement
	Mapping     mapping.Entry
	HasMapping  bool
}

// Stats is the coverage summary, scoped to one framework.
type Stats struct {
	TotalRequirements    int            `json:"totalRequirements"`
	RequirementsByStatus map[string]int `json:"requirementsByStatus"`
	ApprovedRequirements int            `json:"approvedRequirements"`
	ApprovedMapped       int            `json:"approvedMapped"`
	CoveragePercent      float64        `json:"coveragePercent"`
	MappingsByStatus     map[string]int `json:"mappingsByStatus"`
	MappingsBySource     map[string]int `json:"mappingsBySource"`
	ByCategory           []CategoryStat `json:"byCategory"`
	DistinctControls     int            `json:"distinctControls"`
}

// CategoryStat is per-category coverage.
type CategoryStat struct {
	Category string `json:"category"`
	Total    int    `json:"requirements"`
	Approved int    `json:"approvedRequirements"`
	Mapped   int    `json:"approvedMapped"`
	Unmapped int    `json:"unmapped"`
}

// Compute derives the coverage statistics for the report.
func Compute(in Input) Stats {
	st := Stats{
		RequirementsByStatus: map[string]int{},
		MappingsByStatus:     map[string]int{},
		MappingsBySource:     map[string]int{},
	}
	st.TotalRequirements = len(in.Requirements)
	for status, n := range in.Requirements.CountByStatus() {
		st.RequirementsByStatus[string(status)] = n
	}
	st.ApprovedRequirements = st.RequirementsByStatus[string(requirement.StatusApproved)]

	for _, e := range in.Mappings {
		st.MappingsByStatus[string(e.Status)]++
		st.MappingsBySource[string(e.Source)]++
	}

	controls := map[string]bool{}
	catIdx := map[string]*CategoryStat{}
	var order []string
	for _, r := range in.Requirements {
		cat := r.Category
		if cat == "" {
			cat = requirement.Unclassified
		}
		cs, ok := catIdx[cat]
		if !ok {
			cs = &CategoryStat{Category: cat}
			catIdx[cat] = cs
			order = append(order, cat)
		}
		cs.Total++
		if r.Status != requirement.StatusApproved {
			continue
		}
		cs.Approved++
		if approvedMapping(in.Mappings, r.ID, controls) {
			cs.Mapped++
			st.ApprovedMapped++
		} else {
			cs.Unmapped++
		}
	}
	if st.ApprovedRequirements > 0 {
		st.CoveragePercent = float64(st.ApprovedMapped) / float64(st.ApprovedRequirements) * 100
	}
	st.DistinctControls = len(controls)

	sort.Strings(order)
	for _, c := range order {
		st.ByCategory = append(st.ByCategory, *catIdx[c])
	}
	return st
}

// approvedMapping reports whether reqID has an approved mapping carrying at
// least one control, recording those controls in seen.
func approvedMapping(entries mapping.Set, reqID string, seen map[string]bool) bool {
	found := false
	for _, e := range entries {
		if e.ReqID != reqID || e.Status != requirement.StatusApproved || e.IsUnmapped() {
			continue
		}
		found = true
		for _, c := range e.NISTControlIDs {
			seen[c] = true
		}
	}
	return found
}

// Rows flattens requirements and mappings into report rows, applying the
// --approved-only filter.
func Rows(in Input) []Row {
	var rows []Row
	for _, r := range in.Requirements {
		if in.ApprovedOnly && r.Status != requirement.StatusApproved {
			continue
		}
		entries := in.Mappings.ForRequirement(r.ID)
		var kept mapping.Set
		for _, e := range entries {
			if in.ApprovedOnly && e.Status != requirement.StatusApproved {
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == 0 {
			if in.ApprovedOnly {
				continue
			}
			rows = append(rows, Row{Requirement: r})
			continue
		}
		for _, e := range kept {
			rows = append(rows, Row{Requirement: r, Mapping: e, HasMapping: true})
		}
	}
	return rows
}

// Render writes the report in the requested format.
func Render(format string, in Input, w io.Writer) error {
	switch strings.ToLower(format) {
	case "md", "markdown", "":
		return renderMarkdown(in, w)
	case "json":
		return renderJSON(in, w)
	case "csv":
		return renderCSV(in, w)
	default:
		return fmt.Errorf("unknown report format %q (want md, json or csv)", format)
	}
}

func (in Input) title() string {
	if in.DisplayName != "" {
		return in.DisplayName
	}
	return in.Framework
}

func renderMarkdown(in Input, w io.Writer) error {
	st := Compute(in)
	p := func(format string, args ...any) { fmt.Fprintf(w, format, args...) }

	p("# %s → NIST SP 800-53 Rev 5 crosswalk\n\n", in.title())
	p("| | |\n|---|---|\n")
	p("| Source framework | %s |\n", in.title())
	p("| Framework id | `%s` |\n", in.Framework)
	if in.SourceRef != "" {
		p("| Source reference | %s |\n", in.SourceRef)
	}
	if in.SourceFile != "" {
		p("| Source document | `%s` |\n", in.SourceFile)
	}
	p("| Generated | %s |\n", in.GeneratedAt)
	scope := "all items (approved, draft and rejected)"
	if in.ApprovedOnly {
		scope = "human-approved items only"
	}
	p("| Scope | %s |\n\n", scope)

	p("## Coverage\n\n")
	p("- Requirements: **%d** total\n", st.TotalRequirements)
	for _, s := range []requirement.Status{requirement.StatusApproved, requirement.StatusDraft, requirement.StatusRejected} {
		p("  - %s: %d\n", s, st.RequirementsByStatus[string(s)])
	}
	p("- Approved requirements with an approved mapping: **%d / %d (%.1f%%)**\n",
		st.ApprovedMapped, st.ApprovedRequirements, st.CoveragePercent)
	p("- Distinct 800-53 controls referenced by approved mappings: **%d**\n", st.DistinctControls)
	p("- Mappings by status: ")
	p("%s\n", kvList(st.MappingsByStatus))
	p("- Mappings by source: %s\n\n", kvList(st.MappingsBySource))

	p("## Coverage by category\n\n")
	p("| Category | Requirements | Approved | Mapped | Unmapped |\n|---|---:|---:|---:|---:|\n")
	for _, c := range st.ByCategory {
		p("| %s | %d | %d | %d | %d |\n", c.Category, c.Total, c.Approved, c.Mapped, c.Unmapped)
	}
	p("\n")

	approved, drafts, rejected, unmapped := partition(in)

	p("## Approved crosswalk\n\n")
	if len(approved) == 0 {
		p("_No mappings have been approved yet. Run `regmap review --gate 2`._\n\n")
	} else {
		writeRowTable(w, in, approved)
	}

	if !in.ApprovedOnly {
		p("## Unmapped (approved requirements with no approved control mapping)\n\n")
		if len(unmapped) == 0 {
			p("_None._\n\n")
		} else {
			for _, r := range unmapped {
				p("- **%s** — %s (%s)\n", r.ID, orDash(r.Title), r.Category)
			}
			p("\n")
		}

		p("## Draft / machine-suggested (not approved)\n\n")
		if len(drafts) == 0 {
			p("_None._\n\n")
		} else {
			writeRowTable(w, in, drafts)
		}

		p("## Rejected\n\n")
		if len(rejected) == 0 {
			p("_None._\n\n")
		} else {
			for _, row := range rejected {
				note := row.Requirement.Notes
				if row.HasMapping && row.Mapping.Status == requirement.StatusRejected {
					note = "mapping rejected"
				}
				p("- **%s** — %s%s\n", row.Requirement.ID, orDash(row.Requirement.Title), suffix(note))
			}
			p("\n")
		}
	}

	p("---\n\n")
	p("Every approved row above was signed off by a named reviewer at a review gate.\n")
	p("Draft and machine-suggested rows carry no human sign-off and must not be treated as an assessment.\n")
	return nil
}

func writeRowTable(w io.Writer, in Input, rows []Row) {
	fmt.Fprintf(w, "| Requirement | Section | Category | 800-53 controls | Confidence | Source | Status | Reviewed by | Reviewed at | Rationale |\n")
	fmt.Fprintf(w, "|---|---|---|---|---|---|---|---|---|---|\n")
	for _, row := range rows {
		r, e := row.Requirement, row.Mapping
		controls := "—"
		if row.HasMapping {
			controls = e.Controls()
			if row.HasMapping && !e.IsUnmapped() && in.Catalog != nil {
				var parts []string
				for _, id := range e.NISTControlIDs {
					parts = append(parts, fmt.Sprintf("%s (%s)", id, in.Catalog.Title(id)))
				}
				controls = strings.Join(parts, "; ")
			}
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			r.ID, md(r.Section), md(r.Category), md(controls),
			orDash(e.Confidence), orDash(string(e.Source)), orDash(string(e.Status)),
			orDash(e.ReviewedBy), orDash(e.ReviewedAt), md(orDash(e.Rationale)))
	}
	fmt.Fprintf(w, "\n")
}

func partition(in Input) (approved, drafts []Row, rejected []Row, unmapped requirement.Set) {
	for _, row := range Rows(in) {
		switch {
		case row.HasMapping && row.Mapping.Status == requirement.StatusApproved:
			approved = append(approved, row)
		case row.HasMapping && row.Mapping.Status == requirement.StatusRejected:
			rejected = append(rejected, row)
		case row.Requirement.Status == requirement.StatusRejected:
			rejected = append(rejected, row)
		default:
			drafts = append(drafts, row)
		}
	}
	seen := map[string]bool{}
	for _, r := range in.Requirements {
		if r.Status != requirement.StatusApproved {
			continue
		}
		if approvedMapping(in.Mappings, r.ID, seen) {
			continue
		}
		unmapped = append(unmapped, r)
	}
	return approved, drafts, rejected, unmapped
}

type jsonReport struct {
	Framework    string    `json:"framework"`
	DisplayName  string    `json:"displayName,omitempty"`
	SourceRef    string    `json:"sourceRef,omitempty"`
	SourceFile   string    `json:"sourceFile,omitempty"`
	GeneratedAt  string    `json:"generatedAt"`
	ApprovedOnly bool      `json:"approvedOnly"`
	Stats        Stats     `json:"coverage"`
	Rows         []jsonRow `json:"crosswalk"`
	Unmapped     []string  `json:"unmapped"`
}

type jsonRow struct {
	ReqID          string   `json:"reqID"`
	Section        string   `json:"section"`
	Title          string   `json:"title"`
	Category       string   `json:"category"`
	RequirementSt  string   `json:"requirementStatus"`
	NISTControlIDs []string `json:"nistControlIDs"`
	ControlTitles  []string `json:"nistControlTitles,omitempty"`
	Rationale      string   `json:"rationale,omitempty"`
	Confidence     string   `json:"confidence,omitempty"`
	Source         string   `json:"source,omitempty"`
	MappingStatus  string   `json:"mappingStatus,omitempty"`
	ReviewedBy     string   `json:"reviewedBy,omitempty"`
	ReviewedAt     string   `json:"reviewedAt,omitempty"`
	SeedGroup      string   `json:"seedGroup,omitempty"`
	Model          string   `json:"model,omitempty"`
}

func renderJSON(in Input, w io.Writer) error {
	rep := jsonReport{
		Framework:    in.Framework,
		DisplayName:  in.DisplayName,
		SourceRef:    in.SourceRef,
		SourceFile:   in.SourceFile,
		GeneratedAt:  in.GeneratedAt,
		ApprovedOnly: in.ApprovedOnly,
		Stats:        Compute(in),
		Rows:         []jsonRow{},
		Unmapped:     []string{},
	}
	for _, row := range Rows(in) {
		r, e := row.Requirement, row.Mapping
		jr := jsonRow{
			ReqID:          r.ID,
			Section:        r.Section,
			Title:          r.Title,
			Category:       r.Category,
			RequirementSt:  string(r.Status),
			NISTControlIDs: e.NISTControlIDs,
			Rationale:      e.Rationale,
			Confidence:     e.Confidence,
			Source:         string(e.Source),
			MappingStatus:  string(e.Status),
			ReviewedBy:     e.ReviewedBy,
			ReviewedAt:     e.ReviewedAt,
			SeedGroup:      e.SeedGroup,
			Model:          e.Model,
		}
		if jr.NISTControlIDs == nil {
			jr.NISTControlIDs = []string{}
		}
		if in.Catalog != nil {
			for _, id := range e.NISTControlIDs {
				jr.ControlTitles = append(jr.ControlTitles, in.Catalog.Title(id))
			}
		}
		rep.Rows = append(rep.Rows, jr)
	}
	_, _, _, unmapped := partition(in)
	for _, r := range unmapped {
		rep.Unmapped = append(rep.Unmapped, r.ID)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		return fmt.Errorf("encode json report: %w", err)
	}
	return nil
}

func renderCSV(in Input, w io.Writer) error {
	cw := csv.NewWriter(w)
	header := []string{
		"framework", "sourceRef", "reqID", "section", "title", "category",
		"requirementStatus", "nistControlIDs", "rationale", "confidence",
		"source", "mappingStatus", "reviewedBy", "reviewedAt", "seedGroup", "model",
	}
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}
	for _, row := range Rows(in) {
		r, e := row.Requirement, row.Mapping
		controls := ""
		if row.HasMapping {
			if e.IsUnmapped() {
				controls = "UNMAPPED"
			} else {
				controls = strings.Join(e.NISTControlIDs, " ")
			}
		}
		rec := []string{
			in.Framework, in.SourceRef, r.ID, r.Section, r.Title, r.Category,
			string(r.Status), controls, e.Rationale, e.Confidence,
			string(e.Source), string(e.Status), e.ReviewedBy, e.ReviewedAt, e.SeedGroup, e.Model,
		}
		if err := cw.Write(rec); err != nil {
			return fmt.Errorf("write csv row %s: %w", r.ID, err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}
	return nil
}

func kvList(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// md escapes the characters that would break a Markdown table cell.
func md(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func suffix(s string) string {
	if s == "" {
		return ""
	}
	return " — " + s
}

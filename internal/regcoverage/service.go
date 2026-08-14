package regcoverage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"grc/internal/aiprovider"
	"grc/internal/regmap/profile"
)

// Service orchestrates ingestion, analysis, reporting and revision.
type Service struct {
	repo      Repository
	nfrs      NFRLister
	asker     Asker
	retriever Retriever
	renderer  Renderer
	now       func() time.Time
}

func NewService(repo Repository, nfrs NFRLister, asker Asker) *Service {
	return &Service{
		repo:      repo,
		nfrs:      nfrs,
		asker:     asker,
		retriever: NewLexicalRetriever(),
		now:       time.Now,
	}
}

// Configured reports whether analysis can run. Upload, browsing and PDF export
// work without a provider; only the analysis and chat steps need one.
func (s *Service) Configured() bool { return s.asker != nil && s.asker.Available() }

// Model describes what will serve an analysis, for the UI.
func (s *Service) Model() string {
	if s.asker == nil {
		return ""
	}
	return s.asker.Describe()
}

// Frameworks lists the profiles an upload can be pinned to.
func (s *Service) Frameworks() ([]Framework, error) {
	registry, err := LoadProfiles()
	if err != nil {
		return nil, err
	}
	out := []Framework{}
	for _, p := range registry.All() {
		out = append(out, Framework{
			ID:        p.ID,
			Name:      p.DisplayName,
			SourceRef: p.SourceRef,
			Generic:   p.ID == GenericProfileID,
		})
	}
	return out, nil
}

// Framework is one selectable profile.
type Framework struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SourceRef string `json:"source_ref"`
	Generic   bool   `json:"generic"`
}

// ---- ingestion ----

// Upload reads, segments and stores a regulation. It does not analyse it:
// analysis is many model calls and is triggered separately, so an upload that
// segmented badly can be deleted before any spend.
func (s *Service) Upload(in UploadInput) (Regulation, error) {
	ingested, err := Ingest(in)
	if err != nil {
		return Regulation{}, err
	}

	if existing, err := s.repo.RegulationBySHA(ingested.Regulation.SHA256); err == nil {
		return Regulation{}, invalidf("this document is already uploaded as %q (#%d)",
			existing.Title, existing.ID)
	} else if !isNotFound(err) {
		return Regulation{}, err
	}

	ingested.Regulation.CreatedAt = s.timestamp()
	return s.repo.CreateRegulation(ingested.Regulation, ingested.Text, ingested.Sections, ingested.Source)
}

func (s *Service) ListRegulations() ([]Regulation, error) { return s.repo.ListRegulations() }

func (s *Service) GetRegulation(id int64) (Regulation, error) { return s.repo.GetRegulation(id) }

func (s *Service) DeleteRegulation(id int64) error { return s.repo.DeleteRegulation(id) }

// Source returns the original upload, for viewing the regulation as published.
func (s *Service) Source(id int64) (Source, error) {
	if _, err := s.repo.GetRegulation(id); err != nil {
		return Source{}, err
	}
	return s.repo.GetSource(id)
}

func (s *Service) ListSections(id int64) ([]Section, error) { return s.repo.ListSections(id) }

// ---- analysis ----

// AnalysisResult reports what a run produced.
type AnalysisResult struct {
	Regulation Regulation `json:"regulation"`
	Version    Version    `json:"version"`
	Stats      Stats      `json:"stats"`
	// Failures names sections whose analysis errored. A run continues past
	// them: one bad section should not cost the other ninety.
	Failures []string `json:"failures,omitempty"`
	Log      []string `json:"log,omitempty"`
}

// Analyze runs the full pipeline over every section and writes version 1 (or
// the next version, if the regulation is re-analysed).
//
// It is synchronous and long: one model call per section plus a summary call.
// The handler bounds it with a generous timeout rather than backgrounding it,
// which matches how NFR enrichment runs and keeps the failure visible to the
// person who asked for it.
func (s *Service) Analyze(ctx context.Context, id int64, actor string) (*AnalysisResult, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}

	reg, err := s.repo.GetRegulation(id)
	if err != nil {
		return nil, err
	}
	sections, err := s.repo.ListSections(id)
	if err != nil {
		return nil, err
	}
	if len(sections) == 0 {
		return nil, invalid("this regulation has no sections to analyse")
	}

	cat, err := LoadCatalog(s.nfrs)
	if err != nil {
		return nil, err
	}
	prof, err := s.profileFor(reg)
	if err != nil {
		return nil, err
	}

	if err := s.repo.SetStatus(id, StatusAnalyzing, "", ""); err != nil {
		return nil, err
	}

	controls, nfrs := cat.Size()
	result := &AnalysisResult{Log: []string{
		fmt.Sprintf("%d section(s) to analyse against %d controls and %d NFRs", len(sections), controls, nfrs),
		fmt.Sprintf("framework profile: %s (%s)", reg.FrameworkName, detectedWord(reg.Detected)),
	}}

	findings := make([]Finding, 0, len(sections))
	skipped := 0
	for _, section := range sections {
		if err := ctx.Err(); err != nil {
			_ = s.repo.SetStatus(id, StatusFailed, "analysis was cancelled", "")
			return nil, err
		}
		// An inline cross-reference ("...designated pursuant to Article 20")
		// looks like a heading to the segmenter and produces a section with
		// almost no body. Analysing one costs a model call and can only produce
		// an ungrounded finding, so it is left unanalysed and visible in the
		// report as such — the reader can see the artifact rather than a
		// confident analysis of nothing.
		if len(strings.TrimSpace(section.Body)) < minAnalyzableBody {
			skipped++
			continue
		}
		finding, err := s.analyzeSection(ctx, reg, section, cat, seedMappings(prof, section, cat))
		if err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", section.Ref, err))
			continue
		}
		finding.CreatedAt = s.timestamp()
		findings = append(findings, finding)
	}

	if len(findings) == 0 {
		detail := "every section failed to analyse"
		if len(result.Failures) > 0 {
			detail = result.Failures[0]
		}
		_ = s.repo.SetStatus(id, StatusFailed, detail, "")
		return nil, fmt.Errorf("analysis produced nothing: %s", detail)
	}

	if err := s.repo.ReplaceFindings(id, findings); err != nil {
		return nil, err
	}

	stored, err := s.repo.CurrentFindings(id)
	if err != nil {
		return nil, err
	}
	stats := computeStats(sections, stored)

	summary, err := s.summarize(ctx, reg, sections, stored, stats)
	if err != nil {
		// A missing summary is a worse report, not a failed one: the mappings
		// are the deliverable and they are already written.
		result.Log = append(result.Log, "executive summary unavailable: "+err.Error())
	}

	note := "Initial analysis"
	if reg.LatestVersion > 0 {
		note = "Re-analysis of the full regulation"
	}
	version, err := s.cutVersion(id, summary, note, actor)
	if err != nil {
		return nil, err
	}

	if err := s.repo.SetStatus(id, StatusAnalyzed, "", s.timestamp()); err != nil {
		return nil, err
	}
	reg, err = s.repo.GetRegulation(id)
	if err != nil {
		return nil, err
	}

	result.Regulation = reg
	result.Version = version
	result.Stats = stats
	if skipped > 0 {
		result.Log = append(result.Log,
			fmt.Sprintf("%d section(s) had no body to analyse — most likely inline cross-references the segmenter read as headings", skipped))
	}
	if len(result.Failures) > 0 {
		result.Log = append(result.Log,
			fmt.Sprintf("%d section(s) failed and are missing from the report", len(result.Failures)))
	}
	return result, nil
}

// profileFor resolves the framework profile a regulation was segmented with, so
// its curated crosswalk can be offered to the model as a prior.
func (s *Service) profileFor(reg Regulation) (*profile.Profile, error) {
	registry, err := LoadProfiles()
	if err != nil {
		return nil, err
	}
	prof, err := registry.Get(reg.Framework)
	if err != nil {
		// A regulation stored under a profile that has since been removed is
		// still analysable; it just loses its curated prior.
		return nil, nil //nolint:nilnil // absence is the meaningful result here
	}
	return prof, nil
}

// seedMappings returns the curated crosswalk entries for a section.
func seedMappings(prof *profile.Profile, section Section, cat *Catalog) []Mapping {
	if prof == nil {
		return nil
	}
	key := sectionKey(section)
	entry, ok := prof.Seed(key, section.Title+"\n"+section.Body)
	if !ok {
		return nil
	}
	out := make([]Mapping, 0, len(entry.Controls))
	for _, ref := range entry.Controls {
		m := Mapping{
			Kind:       KindControl,
			Ref:        strings.ToUpper(strings.TrimSpace(ref)),
			Rationale:  entry.Rationale,
			Confidence: ConfidenceHigh,
			Source:     SourceSeed,
		}
		if cand, ok := cat.Lookup(KindControl, m.Ref); ok {
			m.Title = cand.Title
			m.Known = true
		}
		out = append(out, m)
	}
	return out
}

// sectionKey recovers the profile's section key ("17") from a minted ref
// ("DORA-ART-17"), which is what the seed selectors match on.
func sectionKey(section Section) string {
	ref := section.Ref
	if i := strings.LastIndexByte(ref, '-'); i >= 0 && i+1 < len(ref) {
		return ref[i+1:]
	}
	return ref
}

const summarySystemPrompt = `You are a compliance analyst writing the executive summary of a regulation coverage report.

You are given the regulation, its coverage statistics, and a condensed list of its security-relevant sections with what each maps to.

Write 3 to 5 short paragraphs of plain prose for a CISO who has not read the regulation:
1. What this regulation requires of the organisation, in substance.
2. Where the existing control catalog already answers it.
3. Where it does not — name the specific gaps, and be direct about them.
4. What to do next, in priority order.

Do not restate the statistics as numbers the reader can already see. Do not use markdown, headings, bullets or code fences. No preamble.`

// summarize asks for the executive summary once the mappings exist.
func (s *Service) summarize(ctx context.Context, reg Regulation, sections []Section, findings []Finding, stats Stats) (string, error) {
	prompt := SummaryPrompt(reg, sections, findings, stats)
	resp, err := s.asker.Ask(ctx, aiprovider.Request{
		System:    summarySystemPrompt,
		Prompt:    prompt,
		MaxTokens: summaryTokens,
	})
	if err != nil {
		return "", err
	}
	if resp.Refused {
		return "", fmt.Errorf("the model declined to write the summary")
	}
	return strings.TrimSpace(resp.Text), nil
}

// SummaryPrompt renders the executive-summary turn. Exported for testing.
func SummaryPrompt(reg Regulation, sections []Section, findings []Finding, stats Stats) string {
	var b strings.Builder
	b.WriteString("# Regulation\n\n")
	writeField(&b, "Title", reg.Title)
	writeField(&b, "Framework", reg.FrameworkName)
	writeField(&b, "Reference", reg.SourceRef)

	b.WriteString("\n# Coverage\n\n")
	b.WriteString(fmt.Sprintf("%d sections, %d security-relevant, %d of those mapped to at least one catalog item, %d unmapped.\n",
		stats.Sections, stats.Relevant, stats.Mapped, stats.Unmapped))
	b.WriteString(fmt.Sprintf("%d distinct controls and %d distinct Security NFRs are referenced.\n",
		stats.DistinctControls, stats.DistinctNFRs))

	titles := map[int64]Section{}
	for _, sec := range sections {
		titles[sec.ID] = sec
	}

	b.WriteString("\n# Security-relevant sections\n\n")
	for _, f := range findings {
		if !f.Relevant {
			continue
		}
		sec := titles[f.SectionID]
		b.WriteString(fmt.Sprintf("## %s %s\n", firstNonEmpty(sec.Label, f.SectionRef), sec.Title))
		if f.Requirement != "" {
			b.WriteString("Requires: " + collapse(f.Requirement) + "\n")
		}
		refs := make([]string, 0, len(f.Mappings))
		for _, m := range f.Mappings {
			refs = append(refs, m.Ref)
		}
		if len(refs) == 0 {
			b.WriteString("Maps to: nothing in the catalog\n")
		} else {
			b.WriteString("Maps to: " + strings.Join(refs, ", ") + "\n")
		}
		if f.Gaps != "" {
			b.WriteString("Gap: " + collapse(f.Gaps) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Write the executive summary.\n")
	return b.String()
}

// ---- reports ----

// Report assembles the report at a version. Version 0 means the latest.
//
// A version other than the latest is served from its snapshot, which is the
// point of taking one: a report that has been sent to a client has to keep
// saying what it said, even after a revision changed the live findings.
func (s *Service) Report(id int64, version int) (*Report, error) {
	reg, err := s.repo.GetRegulation(id)
	if err != nil {
		return nil, err
	}

	latest, err := s.repo.LatestVersion(id)
	if err != nil && !isNotFound(err) {
		return nil, err
	}
	if version > 0 && latest.Number > 0 && version != latest.Number {
		return s.snapshotReport(id, version)
	}

	sections, err := s.repo.ListSections(id)
	if err != nil {
		return nil, err
	}
	findings, err := s.repo.CurrentFindings(id)
	if err != nil {
		return nil, err
	}
	return buildReport(reg, latest, sections, findings, s.timestamp()), nil
}

func (s *Service) snapshotReport(id int64, version int) (*Report, error) {
	v, err := s.repo.GetVersion(id, version)
	if err != nil {
		return nil, err
	}
	var report Report
	if err := json.Unmarshal([]byte(v.Snapshot), &report); err != nil {
		return nil, fmt.Errorf("version %d snapshot is unreadable: %w", version, err)
	}
	return &report, nil
}

func (s *Service) ListVersions(id int64) ([]Version, error) { return s.repo.ListVersions(id) }

// cutVersion snapshots the current report and appends it as the next version.
func (s *Service) cutVersion(id int64, summary, note, actor string) (Version, error) {
	reg, err := s.repo.GetRegulation(id)
	if err != nil {
		return Version{}, err
	}
	sections, err := s.repo.ListSections(id)
	if err != nil {
		return Version{}, err
	}
	findings, err := s.repo.CurrentFindings(id)
	if err != nil {
		return Version{}, err
	}

	// The summary carries forward when a revision does not supply a new one, so
	// a section-level correction does not silently blank the summary.
	if strings.TrimSpace(summary) == "" {
		if previous, err := s.repo.LatestVersion(id); err == nil {
			summary = previous.Summary
		} else if !isNotFound(err) {
			return Version{}, err
		}
	}

	version := Version{
		RegulationID: id,
		Summary:      summary,
		Note:         note,
		CreatedAt:    s.timestamp(),
		CreatedBy:    actor,
	}

	// The snapshot is built with the version's own number, which cutVersion
	// only learns on insert, so it is written in two steps: insert to claim the
	// number, then fill the snapshot in.
	created, err := s.repo.CreateVersion(version)
	if err != nil {
		return Version{}, err
	}
	report := buildReport(reg, created, sections, findings, created.CreatedAt)
	snapshot, err := json.Marshal(report)
	if err != nil {
		return Version{}, fmt.Errorf("snapshot version %d: %w", created.Number, err)
	}
	created.Snapshot = string(snapshot)
	if err := s.repo.SaveVersionSnapshot(created.ID, created.Snapshot); err != nil {
		return Version{}, err
	}
	return created, nil
}

func buildReport(reg Regulation, version Version, sections []Section, findings []Finding, generatedAt string) *Report {
	bySection := map[int64]Finding{}
	for _, f := range findings {
		bySection[f.SectionID] = f
	}

	entries := make([]Entry, 0, len(sections))
	for _, sec := range sections {
		entry := Entry{Section: sec}
		if f, ok := bySection[sec.ID]; ok {
			found := f
			entry.Finding = &found
		}
		entries = append(entries, entry)
	}

	return &Report{
		Regulation:  reg,
		Version:     version,
		Sections:    entries,
		Stats:       computeStats(sections, findings),
		GeneratedAt: generatedAt,
	}
}

func computeStats(sections []Section, findings []Finding) Stats {
	stats := Stats{Sections: len(sections)}
	controls := map[string]bool{}
	nfrs := map[string]bool{}

	for _, f := range findings {
		stats.Analyzed++
		if !f.Grounded {
			stats.Ungrounded++
		}
		if f.Confidence == ConfidenceLow {
			stats.LowConfidence++
		}
		if !f.Relevant {
			stats.NotRelevant++
			continue
		}
		stats.Relevant++
		if len(f.Mappings) == 0 {
			stats.Unmapped++
		} else {
			stats.Mapped++
		}
		for _, m := range f.Mappings {
			if !m.Known {
				stats.UnknownRefs++
			}
			if m.Kind == KindNFR {
				stats.NFRMappings++
				nfrs[m.Ref] = true
				continue
			}
			stats.ControlMappings++
			controls[m.Ref] = true
		}
	}
	stats.DistinctControls = len(controls)
	stats.DistinctNFRs = len(nfrs)
	return stats
}

// SortedRefs lists the distinct catalog references a report touches, for the
// coverage appendix.
func (r *Report) SortedRefs(kind string) []string {
	seen := map[string]bool{}
	for _, entry := range r.Sections {
		if entry.Finding == nil {
			continue
		}
		for _, m := range entry.Finding.Mappings {
			if m.Kind == kind {
				seen[m.Ref] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func (s *Service) timestamp() string { return s.now().UTC().Format(time.RFC3339) }

func detectedWord(detected bool) string {
	if detected {
		return "detected from the document"
	}
	return "generic fallback"
}

func isNotFound(err error) bool {
	var nf ErrNotFound
	return errors.As(err, &nf)
}

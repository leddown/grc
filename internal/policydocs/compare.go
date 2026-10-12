package policydocs

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Comparing a document with an approved version of it: what changed since the
// client signed it off. Either side is an approved version or the current text;
// sections are aligned by heading, their content as text units (a paragraph, a
// list item, a table row), and a changed unit gets a word-level diff.

// Diff operations.
const (
	DiffSame    = "same"
	DiffAdded   = "added"
	DiffRemoved = "removed"
	DiffChanged = "changed"
)

// CurrentRevision names the document as it stands now.
const CurrentRevision = "current"

// DiffWord is a run of text with one operation.
type DiffWord struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

// DiffUnit is one text unit.
type DiffUnit struct {
	Op     string     `json:"op"`
	Before string     `json:"before,omitempty"`
	After  string     `json:"after,omitempty"`
	Words  []DiffWord `json:"words,omitempty"`
}

// DiffSection is one section of the comparison.
type DiffSection struct {
	Op      string     `json:"op"`
	Heading string     `json:"heading"`
	Before  string     `json:"heading_before,omitempty"`
	Units   []DiffUnit `json:"units"`
}

// RevisionInfo says what one side of a comparison is.
type RevisionInfo struct {
	Ref        string `json:"ref"`
	Label      string `json:"label"`
	ApprovedAt string `json:"approved_at,omitempty"`
	ApprovedBy string `json:"approved_by,omitempty"`
	// Verified reports that an approved version still matches the hash taken
	// at approval; a comparison against one that does not is still shown,
	// but the reader is told.
	Verified bool `json:"verified"`
}

// Comparison is the whole result.
type Comparison struct {
	DocumentID int64         `json:"document_id"`
	Title      string        `json:"title"`
	From       RevisionInfo  `json:"from"`
	To         RevisionInfo  `json:"to"`
	Sections   []DiffSection `json:"sections"`
	// Counts are the number of sections added, removed and changed.
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Changed int `json:"changed"`
}

type revSection struct {
	heading string
	units   []string
}

// unitsFromBlocks flattens rendered blocks into text units.
func unitsFromBlocks(blocks []Block) []string {
	var out []string
	var walk func(bs []Block, prefix string)
	runs := func(rs []Run) string {
		var b strings.Builder
		for _, r := range rs {
			if r.Break {
				b.WriteString(" ")
				continue
			}
			b.WriteString(r.Text)
		}
		return strings.TrimSpace(b.String())
	}
	walk = func(bs []Block, prefix string) {
		for _, b := range bs {
			switch b.Type {
			case "paragraph", "heading":
				if t := runs(b.Runs); t != "" {
					out = append(out, prefix+t)
				}
			case "bullet_list", "ordered_list":
				for i, item := range b.Items {
					marker := "• "
					if b.Type == "ordered_list" {
						marker = strconv.Itoa(b.Start+i) + ". "
					}
					walk(item, prefix+marker)
				}
			case "table":
				for _, row := range b.Rows {
					cells := make([]string, 0, len(row.Cells))
					for _, cell := range row.Cells {
						var parts []string
						for _, cb := range cell {
							parts = append(parts, runs(cb.Runs))
						}
						cells = append(cells, strings.Join(parts, " "))
					}
					out = append(out, prefix+strings.Join(cells, " | "))
				}
			case "blockquote":
				walk(b.Blocks, prefix+"> ")
			case "callout":
				label := "Note: "
				if b.Kind == "important" {
					label = "Important: "
				}
				walk(b.Blocks, prefix+label)
			}
		}
	}
	walk(blocks, "")
	return out
}

// unitsFromBody splits a Markdown body into units: one per non-empty line,
// with list markers kept, so a legacy body compares as it reads.
func unitsFromBody(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || t == "_No content._" {
			continue
		}
		// A Markdown bullet reads as the blocks' bullet, so a version kept as
		// Markdown compares equal to the same list held as blocks.
		for _, marker := range []string{"- ", "* ", "+ "} {
			if strings.HasPrefix(t, marker) {
				t = "• " + strings.TrimSpace(t[len(marker):])
				break
			}
		}
		out = append(out, t)
	}
	return out
}

func sectionsFromRows(rows []Section) []revSection {
	out := make([]revSection, 0, len(rows))
	for _, r := range rows {
		units := unitsFromBlocks(decodeBlocks(r.BlocksJSON))
		if units == nil {
			units = unitsFromBody(r.Body)
		}
		out = append(out, revSection{heading: r.Heading, units: units})
	}
	return out
}

// sectionsFromVersion reads an approved version: its structured content when
// it has one, otherwise the Markdown snapshot, whose sections are its "## "
// headings between the document-control and control-mapping tables.
func sectionsFromVersion(v Version) []revSection {
	if strings.TrimSpace(v.ContentJSON) != "" {
		var approved []approvedSection
		if json.Unmarshal([]byte(v.ContentJSON), &approved) == nil {
			out := make([]revSection, 0, len(approved))
			for _, a := range approved {
				units := unitsFromBlocks(a.Blocks)
				if units == nil {
					units = unitsFromBody(a.Body)
				}
				out = append(out, revSection{heading: a.Heading, units: units})
			}
			return out
		}
	}
	var out []revSection
	for _, chunk := range strings.Split("\n"+v.Snapshot, "\n## ")[1:] {
		heading, body, _ := strings.Cut(chunk, "\n")
		heading = strings.TrimSpace(heading)
		if heading == "Document Control" || heading == "Control Mapping" {
			continue
		}
		var lines []string
		for _, line := range strings.Split(body, "\n") {
			if t := strings.TrimSpace(line); strings.HasPrefix(t, "_Satisfies:") {
				continue
			}
			lines = append(lines, line)
		}
		out = append(out, revSection{heading: heading, units: unitsFromBody(strings.Join(lines, "\n"))})
	}
	return out
}

func headingKey(h string) string { return strings.ToLower(strings.Join(strings.Fields(h), " ")) }

// lcs returns the index pairs of a longest common subsequence of a and b
// under eq. Inputs here are sections and paragraphs, tens to a few hundred
// long, so the quadratic table is fine; maxLCS bounds it for a pathological
// document.
func lcs(n, m int, eq func(i, j int) bool) [][2]int {
	const maxLCS = 4_000_000
	if n*m > maxLCS {
		return nil
	}
	table := make([][]int, n+1)
	for i := range table {
		table[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if eq(i, j) {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	var out [][2]int
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case eq(i, j):
			out = append(out, [2]int{i, j})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			i++
		default:
			j++
		}
	}
	return out
}

// tokens splits text into words and the whitespace and punctuation between
// them, so a diff of the tokens reassembles into the original text.
func tokens(s string) []string {
	var out []string
	var cur strings.Builder
	kind := -1
	for _, r := range s {
		k := 0
		switch {
		case unicode.IsSpace(r):
			k = 1
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			k = 2
		}
		if cur.Len() > 0 && (k != kind || k == 2) {
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
		kind = k
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func appendWord(out []DiffWord, op, text string) []DiffWord {
	if n := len(out); n > 0 && out[n-1].Op == op {
		out[n-1].Text += text
		return out
	}
	return append(out, DiffWord{Op: op, Text: text})
}

// diffWords is a word-level diff of two versions of one unit.
func diffWords(before, after string) []DiffWord {
	a, b := tokens(before), tokens(after)
	pairs := lcs(len(a), len(b), func(i, j int) bool { return a[i] == b[j] })
	if pairs == nil && (len(a) > 0 || len(b) > 0) {
		return []DiffWord{{Op: DiffRemoved, Text: before}, {Op: DiffAdded, Text: after}}
	}
	var out []DiffWord
	i, j := 0, 0
	for _, p := range append(pairs, [2]int{len(a), len(b)}) {
		for ; i < p[0]; i++ {
			out = appendWord(out, DiffRemoved, a[i])
		}
		for ; j < p[1]; j++ {
			out = appendWord(out, DiffAdded, b[j])
		}
		if p[0] < len(a) {
			out = appendWord(out, DiffSame, a[p[0]])
			i, j = p[0]+1, p[1]+1
		}
	}
	return out
}

// diffUnits aligns two sections' units. A run of removed units followed by a
// run of added ones is paired off as changed units, each with its word diff.
func diffUnits(a, b []string) []DiffUnit {
	pairs := lcs(len(a), len(b), func(i, j int) bool { return a[i] == b[j] })
	var out []DiffUnit
	i, j := 0, 0
	flush := func(toI, toJ int) {
		removed, added := a[i:toI], b[j:toJ]
		k := 0
		for ; k < len(removed) && k < len(added); k++ {
			out = append(out, DiffUnit{Op: DiffChanged, Before: removed[k], After: added[k], Words: diffWords(removed[k], added[k])})
		}
		for _, r := range removed[k:] {
			out = append(out, DiffUnit{Op: DiffRemoved, Before: r})
		}
		for _, ad := range added[k:] {
			out = append(out, DiffUnit{Op: DiffAdded, After: ad})
		}
		i, j = toI, toJ
	}
	for _, p := range pairs {
		flush(p[0], p[1])
		out = append(out, DiffUnit{Op: DiffSame, Before: a[p[0]], After: b[p[1]]})
		i, j = p[0]+1, p[1]+1
	}
	flush(len(a), len(b))
	if out == nil {
		out = []DiffUnit{}
	}
	return out
}

func compareSections(a, b []revSection) ([]DiffSection, int, int, int) {
	pairs := lcs(len(a), len(b), func(i, j int) bool { return headingKey(a[i].heading) == headingKey(b[j].heading) })
	var out []DiffSection
	var added, removed, changed int
	i, j := 0, 0
	emit := func(toI, toJ int) {
		for ; i < toI; i++ {
			units := make([]DiffUnit, 0, len(a[i].units))
			for _, u := range a[i].units {
				units = append(units, DiffUnit{Op: DiffRemoved, Before: u})
			}
			out = append(out, DiffSection{Op: DiffRemoved, Heading: a[i].heading, Units: units})
			removed++
		}
		for ; j < toJ; j++ {
			units := make([]DiffUnit, 0, len(b[j].units))
			for _, u := range b[j].units {
				units = append(units, DiffUnit{Op: DiffAdded, After: u})
			}
			out = append(out, DiffSection{Op: DiffAdded, Heading: b[j].heading, Units: units})
			added++
		}
	}
	for _, p := range pairs {
		emit(p[0], p[1])
		units := diffUnits(a[p[0]].units, b[p[1]].units)
		op := DiffSame
		for _, u := range units {
			if u.Op != DiffSame {
				op = DiffChanged
				break
			}
		}
		sec := DiffSection{Op: op, Heading: b[p[1]].heading, Units: units}
		if a[p[0]].heading != b[p[1]].heading {
			sec.Op, sec.Before = DiffChanged, a[p[0]].heading
		}
		if sec.Op == DiffChanged {
			changed++
		}
		out = append(out, sec)
		i, j = p[0]+1, p[1]+1
	}
	emit(len(a), len(b))
	if out == nil {
		out = []DiffSection{}
	}
	return out, added, removed, changed
}

// Compare compares two revisions of a document. from and to are an approved
// version's id or CurrentRevision.
func (s *Service) Compare(documentID int64, from, to string) (Comparison, error) {
	doc, err := s.repo.GetDocument(documentID)
	if err != nil {
		return Comparison{}, mapNotFound(err)
	}
	versions, err := s.repo.ListVersions(documentID)
	if err != nil {
		return Comparison{}, err
	}
	side := func(ref string) (RevisionInfo, []revSection, error) {
		ref = strings.TrimSpace(ref)
		if ref == "" || ref == CurrentRevision {
			rows, err := s.repo.ListSections(documentID)
			if err != nil {
				return RevisionInfo{}, nil, err
			}
			return RevisionInfo{Ref: CurrentRevision, Label: "Current text (" + doc.Status + ")", Verified: true}, sectionsFromRows(rows), nil
		}
		id, err := strconv.ParseInt(ref, 10, 64)
		if err != nil {
			return RevisionInfo{}, nil, invalid("compare with an approved version's id or %q", CurrentRevision)
		}
		for _, v := range versions {
			if v.ID == id {
				return RevisionInfo{Ref: ref, Label: "Version " + v.VersionLabel, ApprovedAt: v.ApprovedAt, ApprovedBy: v.ApprovedBy, Verified: v.Verify()},
					sectionsFromVersion(v), nil
			}
		}
		return RevisionInfo{}, nil, fmt.Errorf("version %s of document %d: %w", ref, documentID, ErrNotFound)
	}
	fromInfo, a, err := side(from)
	if err != nil {
		return Comparison{}, err
	}
	toInfo, b, err := side(to)
	if err != nil {
		return Comparison{}, err
	}
	c := Comparison{DocumentID: doc.ID, Title: doc.Title, From: fromInfo, To: toInfo}
	c.Sections, c.Added, c.Removed, c.Changed = compareSections(a, b)
	return c, nil
}

package assuranceplan

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The population arrives as CSV text pasted into the page and posted as JSON.
// It is structured data typed or exported by a person, not a document: there
// is no upload and nothing is extracted from a file.

const (
	maxPopulationRows   = 50000
	maxPopulationErrors = 200
	maxItemField        = 500
)

// populationFields are the CSV columns, in the order the page writes them.
var populationFields = []string{
	"item_ref", "name", "stratum", "cif", "business_line", "hosting", "internet_facing", "owner",
	"wave", "attested_on", "dq_status", "dq_reviewed_on", "topup_quarter", "topup_rationale",
	"last_tested_on", "source_kind", "source_ref",
}

var headerAliases = map[string]string{
	"ref": "item_ref", "item": "item_ref", "id": "item_ref", "asset": "item_ref", "asset_id": "item_ref", "asset_ref": "item_ref",
	"item_name": "name", "asset_name": "name",
	"tier": "stratum", "stratum_code": "stratum",
	"critical_important_function": "cif", "critical_or_important_function": "cif",
	"internet":     "internet_facing",
	"asset_owner":  "owner",
	"wave_quarter": "wave", "1lod_wave": "wave", "lod1_wave": "wave",
	"attestation_date": "attested_on", "attested": "attested_on", "1lod_attestation_date": "attested_on",
	"dq_review_status": "dq_status", "2lod_dq_status": "dq_status",
	"dq_review_date": "dq_reviewed_on",
	"top_up_quarter": "topup_quarter", "topup": "topup_quarter", "top_up": "topup_quarter",
	"top_up_rationale": "topup_rationale",
	"last_tested":      "last_tested_on", "last_test": "last_tested_on", "last_test_date": "last_tested_on", "last_tested_date": "last_tested_on",
}

func canonicalColumn(h string) string {
	var b strings.Builder
	underscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(h)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			underscore = false
		} else if !underscore && b.Len() > 0 {
			b.WriteByte('_')
			underscore = true
		}
	}
	key := strings.TrimSuffix(b.String(), "_")
	if alias, ok := headerAliases[key]; ok {
		return alias
	}
	return key
}

// PopulationImport is the paste: the CSV text, whether rows it leaves out are
// kept (merge) or removed, and the population digest the preview was made
// against, so an apply does not land on a population that changed meanwhile.
type PopulationImport struct {
	CSV   string `json:"csv"`
	Merge bool   `json:"merge"`
	Base  string `json:"base"`
}

type RowError struct {
	Line    int    `json:"line"`
	ItemRef string `json:"item_ref"`
	Message string `json:"message"`
}

type ItemChange struct {
	ItemRef string   `json:"item_ref"`
	Fields  []string `json:"fields"`
}

// PopulationDiff is what an import would do, or did.
type PopulationDiff struct {
	Rows      int            `json:"rows"`
	Columns   []string       `json:"columns"`
	Ignored   []string       `json:"ignored_columns"`
	Errors    []RowError     `json:"errors"`
	Added     []string       `json:"added"`
	Changed   []ItemChange   `json:"changed"`
	Removed   []string       `json:"removed"`
	Unchanged int            `json:"unchanged"`
	ByStratum map[string]int `json:"by_stratum"`
	Base      string         `json:"base"`
	Applied   bool           `json:"applied"`
}

type parsedRow struct {
	line    int
	item    PopulationItem
	present map[string]bool
}

type parsedCSV struct {
	rows    []parsedRow
	cols    []string
	ignored []string
	errs    []RowError
	total   int
}

// parsePopulationCSV reads the paste. A structural problem (no header, no
// reference column, unreadable CSV) is an error; a bad row is a RowError, so
// the preview can list every one of them at once.
func parsePopulationCSV(text string, strata []Stratum) (parsedCSV, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	if strings.TrimSpace(text) == "" {
		return parsedCSV{}, invalid("the paste is empty: put the CSV text, with a header row, in the box")
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = sniffDelimiter(text)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return parsedCSV{}, invalid("the header row cannot be read: %v", err)
	}
	index := map[string]int{}
	var cols, ignored []string
	for i, h := range header {
		key := canonicalColumn(h)
		switch {
		case key == "":
			continue
		case slices.Contains(populationFields, key):
			if _, dup := index[key]; dup {
				return parsedCSV{}, invalid("the column %q appears twice", key)
			}
			index[key] = i
			cols = append(cols, key)
		default:
			ignored = append(ignored, strings.TrimSpace(h))
		}
	}
	if _, ok := index["item_ref"]; !ok {
		return parsedCSV{}, invalid("the header has no item reference column (item_ref, ref or asset_id); columns are: %s", strings.Join(populationFields, ", "))
	}

	byCode := map[string]string{}
	for _, s := range strata {
		byCode[strings.ToLower(s.Code)] = s.Code
		if s.Name != "" {
			byCode[strings.ToLower(s.Name)] = s.Code
		}
	}

	var rows []parsedRow
	var errs []RowError
	seen := map[string]int{}
	count := 0
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return parsedCSV{}, invalid("the paste cannot be read as CSV: %v", err)
		}
		line, _ := r.FieldPos(0)
		get := func(key string) (string, bool) {
			i, ok := index[key]
			if !ok {
				return "", false
			}
			if i >= len(rec) {
				return "", true
			}
			return strings.TrimSpace(rec[i]), true
		}
		blank := true
		for _, v := range rec {
			if strings.TrimSpace(v) != "" {
				blank = false
				break
			}
		}
		if blank {
			continue
		}
		if count++; count > maxPopulationRows {
			return parsedCSV{}, invalid("the paste has more than %d rows", maxPopulationRows)
		}
		row := parsedRow{line: line, present: map[string]bool{}}
		ref, _ := get("item_ref")
		row.item.ItemRef = ref
		fail := func(format string, args ...any) {
			errs = append(errs, RowError{Line: line, ItemRef: ref, Message: fmt.Sprintf(format, args...)})
		}
		if ref == "" {
			fail("no item reference")
			continue
		}
		if len(ref) > maxItemField {
			fail("the item reference is longer than %d characters", maxItemField)
			continue
		}
		if prev, dup := seen[ref]; dup {
			fail("%s is listed twice (also on line %d)", ref, prev)
			continue
		}
		seen[ref] = line
		ok := true
		for _, key := range cols {
			v, _ := get(key)
			row.present[key] = true
			if len(v) > maxItemField {
				fail("%s is longer than %d characters", key, maxItemField)
				ok = false
				continue
			}
			it := &row.item
			switch key {
			case "name":
				it.Name = v
			case "stratum":
				code, found := byCode[strings.ToLower(v)]
				if !found {
					fail("stratum %q is not one of the plan's strata (%s)", v, strataCodes(strata))
					ok = false
				}
				it.StratumCode = code
			case "cif":
				it.CIF = v
			case "business_line":
				it.BusinessLine = v
			case "hosting":
				it.Hosting = v
			case "internet_facing":
				b, valid := parseYesNo(v)
				if !valid {
					fail("internet-facing %q is not yes or no", v)
					ok = false
				}
				it.InternetFacing = b
			case "owner":
				it.Owner = v
			case "wave", "topup_quarter":
				q, valid := parseQuarter(v)
				if !valid {
					fail("%s %q is not a quarter (Q1 to Q4, or blank)", strings.ReplaceAll(key, "_", " "), v)
					ok = false
				}
				if key == "wave" {
					it.WaveQuarter = q
				} else {
					it.TopUpQuarter = q
				}
			case "attested_on", "dq_reviewed_on", "last_tested_on":
				if v != "" {
					if _, valid := parseDate(v); !valid {
						fail("%s %q is not a date (YYYY-MM-DD)", strings.ReplaceAll(key, "_", " "), v)
						ok = false
					}
				}
				switch key {
				case "attested_on":
					it.AttestedOn = v
				case "dq_reviewed_on":
					it.DQReviewedOn = v
				default:
					it.LastTestedOn = v
				}
			case "source_kind":
				it.SourceKind = v
			case "source_ref":
				it.SourceRef = v
			case "dq_status":
				status, valid := parseDQStatus(v)
				if !valid {
					fail("DQ status %q is not one of %s", v, strings.Join(DQStatuses, ", "))
					ok = false
				}
				it.DQStatus = status
			case "topup_rationale":
				it.TopUpRationale = v
			}
		}
		if ok {
			rows = append(rows, row)
		}
	}
	return parsedCSV{rows: rows, cols: cols, ignored: ignored, errs: errs, total: count}, nil
}

// sniffDelimiter picks the separator from the header line: a comma, or the
// semicolon a European spreadsheet writes, or a tab when copied from a grid.
func sniffDelimiter(text string) rune {
	line, _, _ := strings.Cut(text, "\n")
	best, count := ',', strings.Count(line, ",")
	for _, d := range []rune{';', '\t'} {
		if n := strings.Count(line, string(d)); n > count {
			best, count = d, n
		}
	}
	return best
}

func parseYesNo(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "no", "n", "false", "0", "-":
		return false, true
	case "yes", "y", "true", "1", "x":
		return true, true
	}
	return false, false
}

func parseQuarter(v string) (int, bool) {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" || v == "-" || v == "0" || v == "NONE" {
		return 0, true
	}
	n, err := strconv.Atoi(strings.TrimPrefix(v, "Q"))
	if err != nil || n < 1 || n > 4 {
		return 0, false
	}
	return n, true
}

func parseDQStatus(v string) (string, bool) {
	if strings.TrimSpace(v) == "" {
		return "", true
	}
	for _, s := range DQStatuses {
		if strings.EqualFold(s, strings.TrimSpace(v)) {
			return s, true
		}
	}
	if strings.EqualFold(strings.TrimSpace(v), "returned") {
		return DQReturned, true
	}
	return "", false
}

func strataCodes(strata []Stratum) string {
	if len(strata) == 0 {
		return "none defined yet"
	}
	codes := make([]string, len(strata))
	for i, s := range strata {
		codes[i] = s.Code
	}
	return strings.Join(codes, ", ")
}

func quarterLabel(q int) string {
	if q == 0 {
		return "none"
	}
	return "Q" + strconv.Itoa(q)
}

// itemChanges lists what an edit changes on an item, field by field.
func itemChanges(a, b PopulationItem) []string {
	var out []string
	cmp := func(label, x, y string) {
		if x != y {
			out = append(out, fmt.Sprintf("%s: %q → %q", label, clip(x, 40), clip(y, 40)))
		}
	}
	cmp("name", a.Name, b.Name)
	cmp("stratum", a.StratumCode, b.StratumCode)
	cmp("CIF", a.CIF, b.CIF)
	cmp("business line", a.BusinessLine, b.BusinessLine)
	cmp("hosting", a.Hosting, b.Hosting)
	cmp("internet-facing", strconv.FormatBool(a.InternetFacing), strconv.FormatBool(b.InternetFacing))
	cmp("owner", a.Owner, b.Owner)
	cmp("wave", quarterLabel(a.WaveQuarter), quarterLabel(b.WaveQuarter))
	cmp("attested on", a.AttestedOn, b.AttestedOn)
	cmp("DQ status", a.DQStatus, b.DQStatus)
	cmp("DQ reviewed on", a.DQReviewedOn, b.DQReviewedOn)
	cmp("top-up", quarterLabel(a.TopUpQuarter), quarterLabel(b.TopUpQuarter))
	cmp("top-up rationale", a.TopUpRationale, b.TopUpRationale)
	cmp("last tested on", a.LastTestedOn, b.LastTestedOn)
	cmp("source", a.SourceKind+" "+a.SourceRef, b.SourceKind+" "+b.SourceRef)
	return out
}

// mergeRow lays the columns the paste carries over the existing item; a
// column the paste does not have leaves that field as it was.
func mergeRow(base PopulationItem, row parsedRow) PopulationItem {
	it := base
	src := row.item
	set := func(key string, apply func()) {
		if row.present[key] {
			apply()
		}
	}
	it.ItemRef = src.ItemRef
	set("name", func() { it.Name = src.Name })
	set("stratum", func() { it.StratumCode = src.StratumCode })
	set("cif", func() { it.CIF = src.CIF })
	set("business_line", func() { it.BusinessLine = src.BusinessLine })
	set("hosting", func() { it.Hosting = src.Hosting })
	set("internet_facing", func() { it.InternetFacing = src.InternetFacing })
	set("owner", func() { it.Owner = src.Owner })
	set("wave", func() { it.WaveQuarter = src.WaveQuarter })
	set("attested_on", func() { it.AttestedOn = src.AttestedOn })
	set("dq_status", func() { it.DQStatus = src.DQStatus })
	set("dq_reviewed_on", func() { it.DQReviewedOn = src.DQReviewedOn })
	set("topup_quarter", func() { it.TopUpQuarter = src.TopUpQuarter })
	set("topup_rationale", func() { it.TopUpRationale = src.TopUpRationale })
	set("last_tested_on", func() { it.LastTestedOn = src.LastTestedOn })
	set("source_kind", func() { it.SourceKind = src.SourceKind })
	set("source_ref", func() { it.SourceRef = src.SourceRef })
	return it
}

// scopeRules decide which population edits the plan's state allows.
// Membership (adding or removing an item) and an item's stratum and wave are
// what the sampling is drawn from; the rest describes the item or records its
// execution and stays editable until the plan closes.
type scopeRules struct {
	membershipOK bool
	reason       string
	selectedQ    map[int]bool
	// cycle holds the multi-year strata, where an item's last test decides
	// whether it is due and so whether it is in the sampled population.
	cycle map[string]bool
}

func rulesFor(p Plan) scopeRules {
	r := scopeRules{membershipOK: true, selectedQ: map[int]bool{}, cycle: map[string]bool{}}
	for _, st := range p.Strata {
		if st.CycleYears > 1 {
			r.cycle[st.Code] = true
		}
	}
	for _, q := range p.Quarters {
		if q.SelectedAt != "" {
			r.selectedQ[q.Quarter] = true
		}
	}
	switch {
	case p.Status == StatusInReview || p.RevisionStatus == StatusInReview:
		r.membershipOK, r.reason = false, "the plan is in review"
	case p.SelectionSeed != "" && p.RevisionStatus != StatusDraft:
		r.membershipOK, r.reason = false, "the population is frozen: adding, removing or moving an item needs a change request"
	}
	return r
}

// check returns why an edit is refused, if it is. prev is nil for an added
// item and next is nil for a removed one.
func (r scopeRules) check(prev, next *PopulationItem) []string {
	var out []string
	membership := prev == nil || next == nil || prev.StratumCode != next.StratumCode || prev.WaveQuarter != next.WaveQuarter ||
		(prev.LastTestedOn != next.LastTestedOn && (r.cycle[prev.StratumCode] || r.cycle[next.StratumCode]))
	if membership && !r.membershipOK {
		out = append(out, r.reason)
	}
	if prev != nil && prev.SelectedQuarter > 0 && (next == nil || prev.StratumCode != next.StratumCode) {
		if next == nil {
			out = append(out, fmt.Sprintf("it was selected in Q%d (%s) and stays in the population", prev.SelectedQuarter, prev.SelectionBasis))
		} else {
			out = append(out, fmt.Sprintf("it was selected in Q%d (%s), so its stratum cannot change", prev.SelectedQuarter, prev.SelectionBasis))
		}
	}
	frozenWave := func(q int) bool { return q > 0 && r.selectedQ[q] }
	switch {
	case prev == nil && frozenWave(next.WaveQuarter):
		out = append(out, fmt.Sprintf("Q%d has been selected, so its wave cannot gain an item", next.WaveQuarter))
	case next == nil && frozenWave(prev.WaveQuarter) && prev.SelectedQuarter == 0:
		out = append(out, fmt.Sprintf("Q%d has been selected, so its wave cannot lose an item", prev.WaveQuarter))
	case prev != nil && next != nil && (prev.WaveQuarter != next.WaveQuarter || prev.StratumCode != next.StratumCode):
		if frozenWave(prev.WaveQuarter) {
			out = append(out, fmt.Sprintf("Q%d has been selected, so an item cannot leave its wave or stratum", prev.WaveQuarter))
		} else if frozenWave(next.WaveQuarter) {
			out = append(out, fmt.Sprintf("Q%d has been selected, so its wave cannot gain an item", next.WaveQuarter))
		}
	}
	if next != nil {
		oldTop := 0
		if prev != nil {
			oldTop = prev.TopUpQuarter
		}
		if next.TopUpQuarter != oldTop {
			if oldTop > 0 && r.selectedQ[oldTop] {
				out = append(out, fmt.Sprintf("its Q%d top-up has been selected and cannot be withdrawn", oldTop))
			}
			if next.TopUpQuarter > 0 {
				if r.selectedQ[next.TopUpQuarter] {
					out = append(out, fmt.Sprintf("Q%d has been selected; a top-up is flagged before its quarter's selection", next.TopUpQuarter))
				}
				if prev != nil && prev.SelectedQuarter > 0 {
					out = append(out, fmt.Sprintf("it is already a %s pick of Q%d; a top-up must be another item", strings.ToLower(prev.SelectionBasis), prev.SelectedQuarter))
				}
			}
		}
		if next.TopUpQuarter > 0 && strings.TrimSpace(next.TopUpRationale) == "" {
			out = append(out, "a risk-based top-up needs its rationale")
		}
	}
	return out
}

// diffPopulation works out an import against the current population and the
// plan's rules. The change is only meaningful when diff.Errors is empty.
func diffPopulation(p Plan, current []PopulationItem, rows []parsedRow, merge bool) (PopulationChange, PopulationDiff) {
	diff := PopulationDiff{Errors: []RowError{}, Added: []string{}, Changed: []ItemChange{}, Removed: []string{}, ByStratum: map[string]int{}}
	rules := rulesFor(p)
	byRef := map[string]PopulationItem{}
	for _, it := range current {
		byRef[it.ItemRef] = it
	}
	var change PopulationChange
	inPaste := map[string]bool{}
	refuse := func(line int, ref string, reasons []string) {
		for _, msg := range reasons {
			diff.Errors = append(diff.Errors, RowError{Line: line, ItemRef: ref, Message: msg})
		}
	}
	final := map[string]PopulationItem{}
	for _, row := range rows {
		ref := row.item.ItemRef
		inPaste[ref] = true
		prev, exists := byRef[ref]
		if !exists {
			next := mergeRow(PopulationItem{}, row)
			if !row.present["stratum"] || next.StratumCode == "" {
				refuse(row.line, ref, []string{"a new item needs a stratum"})
				continue
			}
			refuse(row.line, ref, rules.check(nil, &next))
			change.Add = append(change.Add, next)
			diff.Added = append(diff.Added, ref)
			final[ref] = next
			continue
		}
		next := mergeRow(prev, row)
		final[ref] = next
		fields := itemChanges(prev, next)
		if len(fields) == 0 {
			diff.Unchanged++
			continue
		}
		refuse(row.line, ref, rules.check(&prev, &next))
		change.Update = append(change.Update, next)
		diff.Changed = append(diff.Changed, ItemChange{ItemRef: ref, Fields: fields})
	}
	for _, it := range current {
		if inPaste[it.ItemRef] {
			continue
		}
		if merge {
			final[it.ItemRef] = it
			continue
		}
		prev := it
		refuse(0, it.ItemRef, rules.check(&prev, nil))
		change.Remove = append(change.Remove, it.ID)
		diff.Removed = append(diff.Removed, it.ItemRef)
	}
	for _, it := range final {
		diff.ByStratum[it.StratumCode]++
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	sort.Slice(diff.Changed, func(i, j int) bool { return diff.Changed[i].ItemRef < diff.Changed[j].ItemRef })
	return change, diff
}

// populationDigest is the SHA-256 of the population, one line per item in
// reference order. With membershipOnly it covers what the draw depends on —
// reference, stratum and wave — and is the fingerprint recorded at the freeze.
func populationDigest(items []PopulationItem, membershipOnly bool) string {
	sorted := slices.Clone(items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ItemRef < sorted[j].ItemRef })
	h := sha256.New()
	for _, it := range sorted {
		fields := []string{it.ItemRef, it.StratumCode, strconv.Itoa(it.WaveQuarter)}
		if !membershipOnly {
			fields = append(fields, it.Name, it.CIF, it.BusinessLine, it.Hosting, strconv.FormatBool(it.InternetFacing), it.Owner,
				it.AttestedOn, it.DQStatus, it.DQReviewedOn, strconv.Itoa(it.TopUpQuarter), it.TopUpRationale,
				it.LastTestedOn, it.SourceKind, it.SourceRef)
		}
		h.Write([]byte(strings.Join(fields, "\x1f") + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// populationCSV writes items in the import's own format, so a population can
// be copied out, edited in a spreadsheet and pasted back.
func populationCSV(items []PopulationItem) string {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(populationFields)
	yes := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	q := func(n int) string {
		if n == 0 {
			return ""
		}
		return "Q" + strconv.Itoa(n)
	}
	for _, it := range items {
		_ = w.Write([]string{it.ItemRef, it.Name, it.StratumCode, it.CIF, it.BusinessLine, it.Hosting, yes(it.InternetFacing), it.Owner,
			q(it.WaveQuarter), it.AttestedOn, it.DQStatus, it.DQReviewedOn, q(it.TopUpQuarter), it.TopUpRationale,
			it.LastTestedOn, it.SourceKind, it.SourceRef})
	}
	w.Flush()
	return buf.String()
}

// examplePopulation makes up a population for trying a plan out: each
// stratum's planned size (20 when it has none), spread over its waves by the
// wave percentages. Every item is fictitious and named as such.
func examplePopulation(strata []Stratum) []PopulationItem {
	systems := []string{"Payments gateway", "Core banking ledger", "Card authorisation", "Online banking portal",
		"Mobile banking API", "Treasury dealing", "Customer data hub", "Identity provider", "Loan origination",
		"Fraud monitoring", "SWIFT messaging", "General ledger", "Data warehouse", "HR and payroll",
		"Document management", "Collateral management", "AML screening", "Branch teller", "Contact centre", "Market data feed"}
	lines := []string{"Retail banking", "Corporate banking", "Markets", "Group functions"}
	hosting := []string{"On-premises", "Private cloud", "Public cloud (IaaS)", "SaaS"}
	functions := []string{"CIF-01 Payments", "CIF-02 Deposits", "CIF-03 Lending", "CIF-04 Trading", "CIF-05 Customer access"}
	var out []PopulationItem
	n := 0
	for _, s := range strata {
		size := s.PlannedSize
		if size <= 0 {
			size = 20
		}
		waves, err := ReviewsDue(size, s.Waves)
		if err != nil {
			waves = [4]int{size, 0, 0, 0}
		}
		critical := strings.Contains(strings.ToLower(s.Definition+" "+s.Name), "critical")
		i := 0
		for q, count := range waves {
			for range count {
				i++
				n++
				it := PopulationItem{
					ItemRef:        fmt.Sprintf("%s-%03d", s.Code, i),
					Name:           fmt.Sprintf("Example %s %02d", systems[n%len(systems)], (n/len(systems))+1),
					StratumCode:    s.Code,
					BusinessLine:   lines[n%len(lines)],
					Hosting:        hosting[(n/3)%len(hosting)],
					InternetFacing: n%4 == 0,
					Owner:          "Head of " + lines[n%len(lines)] + " IT",
					WaveQuarter:    q + 1,
				}
				if critical {
					it.CIF = functions[n%len(functions)]
				}
				out = append(out, it)
			}
		}
	}
	return out
}

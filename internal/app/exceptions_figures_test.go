package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/exceptionbook"
)

// figuresHarness runs after the pages' script and a line that sets data: the
// catalog as the pages fetch it, and the exercises. For each exercise it
// prints the lines the pages would show, worked out by the pages' own
// functions.
const figuresHarness = `
const threatDisplayOrder = ['Spoofing', 'Tampering', 'Repudiation', 'Information Disclosure', 'Denial of Service', 'Elevation of Privilege'];

// As the pages build them from /security-nfrs/links/data and /controls/data.
const links = new Map();
data.links.forEach((row) => {
  if (!row || !row.matched) return;
  const key = String(row.nfr_key || '').trim();
  const id = String(row.control_id || '').trim();
  if (!key || !id) return;
  if (!links.has(key)) links.set(key, new Set());
  links.get(key).add(id);
});
const byID = new Map();
data.controls.forEach((control) => {
  const id = String(control.id || '').trim();
  if (id) byID.set(id, control);
});

const out = data.exercises.map((exercise) => {
  const keys = catalogKeys(data.nfrs);
  const scope = exercise.scoped ? new Set(keys.filter((key) => exercise.scope.includes(key))) : new Set(keys);
  const scopeItems = data.nfrs.filter((item) => scope.has(nfrKey(item)));
  // An NFR out of scope is not an exception, and only an exception is partial.
  const selected = data.nfrs.filter((item) => scope.has(nfrKey(item)) && exercise.selected.includes(nfrKey(item)));
  const partial = new Set(exercise.partial.filter((key) => selected.some((item) => nfrKey(item) === key)));

  const coverage = computeWeightedCoverage(scopeItems, selected, partial, links, byID);
  const weights = weightSummaryLines(computeWeightSummary(scopeItems, selected, partial));
  return [scopeLine(scope, data.nfrs), ciaWeightLine(coverage), threatWeightLine(coverage)].concat(weights);
});
console.log(JSON.stringify(out));
`

// TestPageFiguresAreTheWorkbooksUnderNode holds the pages to the workbook. The
// figures are worked out three times: by the pages' script, by
// internal/exceptionbook in Go, and by the workbook's formulas. LibreOffice
// holds the last two together; this runs the first under node on the seeded
// catalog and compares every line with the second. It needs node on PATH and
// is skipped without it.
func TestPageFiguresAreTheWorkbooksUnderNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on PATH: the pages' script is not run")
	}

	gin.SetMode(gin.TestMode)
	conn := sqliteNoopDB(t)
	_, _, _, controlHandler, nfrHandler, linkHandler, _, _, err := buildHandlers(conn)
	if err != nil {
		t.Fatalf("buildHandlers: %v", err)
	}
	catalog, err := loadExceptionsWorkbookData(nfrHandler.Service(), linkHandler.Service(), controlHandler.Service())
	if err != nil || len(catalog.NFRs) < 30 {
		t.Fatalf("no seeded catalog: %v", err)
	}

	// A spread of weights, so that no figure is a multiple of three.
	var every3, every9, every18, firstDomain []string
	for i := range catalog.NFRs {
		catalog.NFRs[i].Weight = 1 + i%5
		key := catalog.NFRs[i].Key
		if i%3 != 1 {
			every3 = append(every3, key)
		}
		if i%9 == 0 {
			every9 = append(every9, key)
		}
		if i%18 == 0 {
			every18 = append(every18, key)
		}
		if catalog.NFRs[i].Domain == catalog.NFRs[len(catalog.NFRs)/2].Domain {
			firstDomain = append(firstDomain, key)
		}
	}
	// Keys 1, 4, 7 ... are outside every3: selected there, they are not excepted.
	outside := []string{catalog.NFRs[1].Key, catalog.NFRs[4].Key}

	exercises := []exceptionbook.Options{
		{},
		{Selected: every9},
		{Selected: every9, Partial: every18},
		{Selected: every9, Partial: every9},
		{Scope: every3, Scoped: true, Selected: append(append([]string{}, every9...), outside...), Partial: append(append([]string{}, every18...), outside...)},
		{Scope: firstDomain, Scoped: true, Selected: firstDomain, Partial: firstDomain[:len(firstDomain)/2]},
		{Scope: firstDomain, Scoped: true},
		{Scoped: true, Selected: every9},
		{Selected: every3, Partial: every9},
	}

	type exercise struct {
		Scoped   bool     `json:"scoped"`
		Scope    []string `json:"scope"`
		Selected []string `json:"selected"`
		Partial  []string `json:"partial"`
	}
	payload := struct {
		NFRs      any        `json:"nfrs"`
		Links     any        `json:"links"`
		Controls  any        `json:"controls"`
		Exercises []exercise `json:"exercises"`
	}{NFRs: catalog.NFRs, Links: catalog.Links, Controls: catalog.Controls}
	for _, opts := range exercises {
		payload.Exercises = append(payload.Exercises, exercise{
			Scoped:   opts.Scoped,
			Scope:    append([]string{}, opts.Scope...),
			Selected: append([]string{}, opts.Selected...),
			Partial:  append([]string{}, opts.Partial...),
		})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "figures.js")
	script := exceptionScopeScript + exceptionWeightScript + "\nconst data = " + string(raw) + ";\n" + figuresHarness
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, node, path).Output()
	if err != nil {
		t.Fatalf("the pages' script failed under node: %v\n%s", err, output)
	}
	var onPage [][]string
	if err := json.Unmarshal(output, &onPage); err != nil || len(onPage) != len(exercises) {
		t.Fatalf("the pages' script gave %d exercises, want %d: %v\n%.300s", len(onPage), len(exercises), err, output)
	}

	touched := 0
	for i, opts := range exercises {
		result := exceptionbook.Evaluate(catalog, opts)
		// The workbook writes these three as cells; they are worded here as the
		// pages word them, from the figures internal/exceptionbook gives.
		share := 0
		if result.ScopeWeight > 0 {
			share = int(result.SelectedWeight*100/float64(result.ScopeWeight) + 0.5)
		}
		average := "-"
		if result.SelectedNFRs > 0 {
			average = strconv.FormatFloat(float64(int(result.SelectedWeight*100/float64(result.SelectedNFRs)+0.5))/100, 'f', 2, 64)
		}
		byWeight := make([]string, 0, len(result.ByWeight))
		for _, count := range result.ByWeight {
			byWeight = append(byWeight, fmt.Sprintf("%d: %d of %d", count.Weight, count.Selected, count.Scope))
		}
		want := []string{
			result.ScopeLine(),
			result.CIAWeightLine(),
			result.ThreatWeightLine(),
			"Weight of the selection: " + strconv.FormatFloat(result.SelectedWeight, 'f', -1, 64) + " of " + strconv.Itoa(result.ScopeWeight) + " (" + strconv.Itoa(share) + "%)",
			"Average weight of a selected NFR: " + average,
			"By weight (selected of scope): " + strings.Join(byWeight, " | "),
		}
		if len(onPage[i]) != len(want) {
			t.Errorf("exercise %d: the pages give %d lines, want %d", i, len(onPage[i]), len(want))
			continue
		}
		for line := range want {
			if onPage[i][line] != want[line] {
				t.Errorf("exercise %d, line %d:\n  pages:    %s\n  workbook: %s", i, line+1, onPage[i][line], want[line])
			}
		}
		if result.ConfidentialityWeight.Excepted > 0 {
			touched++
		}
	}
	// The comparison has to be of real figures, halves among them.
	if touched < 5 {
		t.Errorf("only %d of the exercises touch confidentiality: the catalog is not linked as expected", touched)
	}
	if !strings.Contains(string(output), ".5 of ") {
		t.Error("no exercise gives a half weight: the partial exceptions are not exercised")
	}
}

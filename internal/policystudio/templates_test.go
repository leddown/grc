package policystudio

import (
	"strings"
	"testing"

	"grc/internal/clientprofile"
	"grc/internal/policydocs"
)

func TestTemplatesAreValid(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 4 || templates[0].ID != "ict-infosec-policy" || !templates[0].Default {
		t.Fatalf("expected the ICT policy first and three skeletons, got %d", len(templates))
	}
	for _, tpl := range templates {
		for _, s := range tpl.Sections {
			lower := strings.ToLower(s.Content)
			for _, weak := range []string{"will ", "strive", "endeavour", "where possible", "as appropriate", "is encouraged"} {
				if strings.Contains(lower, weak) {
					t.Errorf("%s / %s uses %q", tpl.ID, s.Heading, weak)
				}
			}
		}
	}
}

func TestTemplateValidationRefusesBadTemplates(t *testing.T) {
	good := func() Template {
		return Template{ID: "t-one", Version: "1", Title: "T", DocType: policydocs.TypeWorkInstruction,
			Facts: []TemplateFact{{Key: "k", Label: "K"}},
			Sections: []TemplateSection{
				{UIDSeed: "a", Heading: "Purpose", Kind: policydocs.KindPurpose, Content: "Uses {{fact:k}}."},
				{UIDSeed: "b", Heading: "Steps", Kind: policydocs.KindProcedureSteps, Content: "1. Do it."},
			}}
	}
	if err := good().validate(); err != nil {
		t.Fatalf("a good template was refused: %v", err)
	}
	cases := map[string]func(*Template){
		"undeclared fact": func(t *Template) { t.Sections[1].Content = "Uses {{fact:other}}." },
		"unused fact":     func(t *Template) { t.Facts = append(t.Facts, TemplateFact{Key: "spare", Label: "S"}) },
		"full coverage": func(t *Template) {
			t.Sections[0].ProposedMappings = []ProposedMapping{{ControlID: "AC-1", Coverage: "full"}}
		},
		"missing mandatory kind":  func(t *Template) { t.Sections = t.Sections[:1] },
		"unknown framework":       func(t *Template) { t.Frameworks = []string{"Made Up"} },
		"duplicate seed":          func(t *Template) { t.Sections[1].UIDSeed = "a" },
		"unknown document type":   func(t *Template) { t.DocType = "memo" },
		"invalid section content": func(t *Template) { t.Sections[1].Kind = "shell" },
	}
	for name, mutate := range cases {
		tpl := good()
		mutate(&tpl)
		if err := tpl.validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The Phase 1 acceptance: a document created from the default template for a
// client yields every section, only the facts the client has not recorded as
// blocking findings, and valid draft mappings (none claiming full coverage).
func TestCreateFromTheDefaultTemplate(t *testing.T) {
	f := newStudio(t)
	clients := clientprofile.NewService(f.conn)
	f.studio.clients = clients
	clients.OnFactsChanged(f.studio.reprojectClient)
	for _, c := range []string{"PM-1", "PM-2", "PM-9", "AC-5"} { // PS-8 deliberately absent from this catalog
		if _, err := f.conn.Exec(`INSERT INTO rcsa_controls (control_id, name, family, control_type, in_low, in_moderate, in_high, in_privacy, mapping_baselines_json, threats_json)
			VALUES (?, ?, ?, 'control', 1, 1, 1, 0, '[]', '[]')`, c, c+" name", c[:2]); err != nil {
			t.Fatal(err)
		}
	}
	client, err := clients.Create(clientprofile.Profile{Name: "Example Bank AG"})
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]string{
		"legal_entity_name": "Example Bank AG", "dor_strategy_reference": "DOR Strategy 2026", "scope_entities": "Example Bank AG",
		"scope_exclusions": "None", "management_body_name": "Management Board", "ciso_role_title": "Chief Information Security Officer",
		"ict_risk_function_name": "ICT Risk Management", "internal_audit_function_name": "Group Internal Audit",
		"policy_kpi_set": "the ICT risk dashboard", "sanctions_reference": "the Staff Disciplinary Procedure",
		"policy_owner_role": "Chief Information Security Officer",
	}
	for k, v := range known {
		if _, err := clients.SetFact(client.ID, k, v, "text", "alice"); err != nil {
			t.Fatal(err)
		}
	}

	res, err := f.studio.CreateFromTemplate(CreateRequest{TemplateID: "ict-infosec-policy", ClientProfileID: client.ID}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Document
	if doc.EditorFormat != policydocs.EditorStudio || doc.TemplateID != "ict-infosec-policy" || doc.ClientName != "Example Bank AG" || doc.OwnerRole != "Chief Information Security Officer" {
		t.Fatalf("document: %+v", doc)
	}
	rows := f.sections(doc.ID)
	if len(rows) != 10 {
		t.Fatalf("%d sections, want 10", len(rows))
	}
	for _, r := range rows {
		if r.Provenance != policydocs.ProvenanceTemplate || !strings.HasPrefix(r.ProvenanceDetail, "ict-infosec-policy@1.0.0#") || f.studio.guidanceFor(r) == "" {
			t.Fatalf("section %q lacks template provenance or guidance", r.Heading)
		}
	}
	if !strings.Contains(rows[0].Body, "Example Bank AG protects") {
		t.Fatalf("known facts must render by value: %q", rows[0].Body)
	}

	// Only the two facts the client has not recorded block approval.
	findings, err := f.policies.Lint(doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var blocking []string
	for _, fd := range policydocs.BlockingFindings(findings) {
		blocking = append(blocking, fd.Message)
	}
	if len(blocking) != 2 || !strings.Contains(strings.Join(blocking, "|"), "exception_approval_authority") || !strings.Contains(strings.Join(blocking, "|"), "policy_review_events") {
		t.Fatalf("blocking findings: %v", blocking)
	}
	for _, fd := range findings {
		if fd.Severity == policydocs.SeverityWarning && !strings.Contains(fd.Message, "maps no controls") {
			t.Errorf("the template produces an advisory finding: %s — %s", fd.Heading, fd.Message)
		}
	}

	refs, _ := f.policies.ListControlRefsForDocument(doc.ID)
	if len(refs) != 5 {
		t.Fatalf("%d mappings, want 5 (PS-8 is not in this catalog)", len(refs))
	}
	for _, r := range refs {
		if r.Coverage == policydocs.CoverageFull || !r.Known {
			t.Fatalf("mapping %s: %s, known %v", r.ControlID, r.Coverage, r.Known)
		}
	}
	if len(res.SkippedMappings) != 1 || !strings.HasPrefix(res.SkippedMappings[0], "PS-8") {
		t.Fatalf("skipped: %v", res.SkippedMappings)
	}

	// Filling in the missing facts resolves every token without editing text.
	f.openRoom(doc.ID)
	if _, err := clients.SetFact(client.ID, "exception_approval_authority", "the Chief Information Security Officer", "text", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := clients.SetFact(client.ID, "policy_review_events", "a major ICT-related incident", "list", "alice"); err != nil {
		t.Fatal(err)
	}
	findings, _ = f.policies.Lint(doc.ID)
	if b := policydocs.BlockingFindings(findings); len(b) != 0 {
		t.Fatalf("still blocked after filling the facts: %+v; problems %+v", b, f.studio.Problems(doc.ID))
	}
	for _, r := range f.sections(doc.ID) {
		if strings.Contains(r.Body, "UNRESOLVED") || strings.Contains(r.BlocksJSON, `"unresolved":true`) {
			t.Fatalf("section %q still carries an unresolved token", r.Heading)
		}
	}
}

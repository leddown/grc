package policystudio

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"grc/internal/policydocs"
)

func workInstruction(id string) Template {
	return Template{ID: id, Version: "draft", Title: "Badge access", DocType: policydocs.TypeWorkInstruction,
		Facts: []TemplateFact{{Key: "site_name", Label: "Site", ValueType: "text"}},
		Sections: []TemplateSection{
			{UIDSeed: "purpose", Heading: "Purpose", Kind: policydocs.KindPurpose, Content: "Staff at {{fact:site_name}} must badge in.",
				Guidance: "Name the site.", ProposedMappings: []ProposedMapping{{ControlID: "PE-3", Coverage: "partial"}}},
			{UIDSeed: "steps", Heading: "Steps", Kind: policydocs.KindProcedureSteps, Content: "1. Present the badge.\n2. Wait for the light."},
		}}
}

// Problems lists every problem at once, so a reviewer fixes them in one pass.
func TestProblemsListsEverything(t *testing.T) {
	bad := workInstruction("x")
	bad.ID = "Bad Id"
	bad.DocType = "memo"
	bad.Sections[0].Content = "Staff will badge in at {{fact:gate}} where possible."
	bad.Sections[1].Kind = "nonsense"
	bad.Sections[0].ProposedMappings[0].Coverage = policydocs.CoverageFull
	problems := strings.Join(bad.Problems(), "\n")
	for _, want := range []string{`the id "Bad Id"`, `unknown doc_type "memo"`, `unknown kind "nonsense"`, `says "will"`, `says "where possible"`,
		"{{fact:gate}}, which the template does not declare", `fact "site_name" is declared but never used`, "partial or supporting coverage only"} {
		if !strings.Contains(problems, want) {
			t.Errorf("missing %q in:\n%s", want, problems)
		}
	}
	if p := workInstruction("badge-access").Problems(); len(p) != 0 {
		t.Fatalf("a good template: %v", p)
	}
}

func TestAppTemplateLifecycle(t *testing.T) {
	f := newStudio(t)
	s := f.studio

	// An id already in use gets a free variant; a built-in id is in use.
	a, err := s.CreateAppTemplate(NewAppTemplate{Template: workInstruction("ict-infosec-policy"), Origin: OriginImport}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if a.TemplateID != "ict-infosec-policy-2" || a.Status != TemplateDraft || len(a.Problems) != 0 || a.Draft.Default {
		t.Fatalf("created: %+v", a)
	}
	if _, ok := s.Template(a.TemplateID); ok {
		t.Fatal("a draft that was never published is offered")
	}

	// The id may change while it is a draft, to one not in use.
	w := a.Draft
	w.ID = "standard-skeleton"
	var conflict ErrConflict
	if _, err := s.SaveAppTemplate(a.ID, w, "alice"); !errors.As(err, &conflict) {
		t.Fatalf("renaming onto a built-in id: %v", err)
	}
	w.ID = "badge-access"
	w.Sections[0].Content = "Staff will badge in."
	a, err = s.SaveAppTemplate(a.ID, w, "alice")
	if err != nil || a.TemplateID != "badge-access" || len(a.Problems) == 0 {
		t.Fatalf("saved: %+v %v", a, err)
	}
	var problems ErrTemplateProblems
	if _, err := s.PublishAppTemplate(a.ID, "alice"); !errors.As(err, &problems) {
		t.Fatalf("publishing with problems: %v", err)
	}
	w.Sections[0].Content = "Staff at {{fact:site_name}} must badge in."
	if a, err = s.SaveAppTemplate(a.ID, w, "alice"); err != nil {
		t.Fatal(err)
	}
	a, err = s.PublishAppTemplate(a.ID, "bob")
	if err != nil || a.Status != TemplatePublished || a.PublishedVersion != "1.0.0" || a.Changed || a.PublishedBy != "bob" {
		t.Fatalf("published: %+v %v", a, err)
	}
	offered := false
	for _, tpl := range s.Templates() {
		offered = offered || tpl.ID == "badge-access"
	}
	if !offered {
		t.Fatal("a published template is not offered")
	}

	// Documents come from the published copy, and keep its version.
	res, err := s.CreateFromTemplate(CreateRequest{TemplateID: "badge-access"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if res.Document.TemplateVersion != "1.0.0" || len(f.sections(res.Document.ID)) != 2 || s.guidanceFor(f.sections(res.Document.ID)[0]) != "Name the site." {
		t.Fatalf("document from an app template: %+v", res.Document)
	}

	// Editing a published template changes nothing until it is published
	// again, as the next version; its id is fixed now.
	w.Sections[1].Content = "1. Present the badge.\n2. Wait for the green light."
	w.ID = "renamed"
	if _, err := s.SaveAppTemplate(a.ID, w, "alice"); err == nil {
		t.Fatal("a published template's id changed")
	}
	w.ID = "badge-access"
	if a, err = s.SaveAppTemplate(a.ID, w, "alice"); err != nil || !a.Changed {
		t.Fatalf("an edit after publishing: %+v %v", a, err)
	}
	if tpl, _ := s.Template("badge-access"); strings.Contains(tpl.Sections[1].Content, "green") {
		t.Fatal("an unpublished edit reached New document")
	}
	if a, err = s.PublishAppTemplate(a.ID, "alice"); err != nil || a.PublishedVersion != "1.1.0" || a.Changed {
		t.Fatalf("republished: %+v %v", a, err)
	}

	// Retired: no longer offered or creatable, still resolvable for guidance,
	// and not deletable while a document uses it.
	if a, err = s.RetireAppTemplate(a.ID, "alice"); err != nil || a.Status != TemplateRetired {
		t.Fatalf("retired: %+v %v", a, err)
	}
	for _, tpl := range s.Templates() {
		if tpl.ID == "badge-access" {
			t.Fatal("a retired template is offered")
		}
	}
	if _, err := s.CreateFromTemplate(CreateRequest{TemplateID: "badge-access"}, "alice"); err == nil {
		t.Fatal("a document was made from a retired template")
	}
	if s.guidanceFor(f.sections(res.Document.ID)[0]) != "Name the site." {
		t.Fatal("a document made from a retired template lost its guidance")
	}
	if err := s.DeleteAppTemplate(a.ID); !errors.As(err, &conflict) {
		t.Fatalf("deleting a template a document uses: %v", err)
	}
	if a, err = s.RestoreAppTemplate(a.ID, "alice"); err != nil || a.Status != TemplatePublished || a.PublishedVersion != "1.1.0" {
		t.Fatalf("restored: %+v %v", a, err)
	}
}

func TestImportCopyAndDelete(t *testing.T) {
	f := newStudio(t)
	s := f.studio
	raw, _ := json.Marshal(workInstruction("badge-access"))
	a, err := s.ImportTemplate(raw, "alice")
	if err != nil || a.Origin != OriginImport || a.TemplateID != "badge-access" {
		t.Fatalf("import: %+v %v", a, err)
	}
	var conflict ErrConflict
	if _, err := s.ImportTemplate(raw, "alice"); !errors.As(err, &conflict) {
		t.Fatalf("importing the same id twice: %v", err)
	}
	builtIn, _ := json.Marshal(workInstruction("procedure-skeleton"))
	if _, err := s.ImportTemplate(builtIn, "alice"); !errors.As(err, &conflict) {
		t.Fatalf("importing over a built-in id: %v", err)
	}
	for _, junk := range []string{`{"id":"x-y","title":"T","extra":1}`, `not json`, `{"id":"NO"}`} {
		if _, err := s.ImportTemplate([]byte(junk), "alice"); !policydocs.IsValidation(err) {
			t.Errorf("%s: %v", junk, err)
		}
	}

	c, err := s.CopyTemplate("procedure-skeleton", "bob")
	if err != nil || c.TemplateID != "procedure-skeleton-copy" || c.Origin != OriginCopy || !strings.HasPrefix(c.Draft.Title, "Copy of ") || len(c.Problems) != 0 {
		t.Fatalf("copy: %+v %v", c, err)
	}
	if err := s.DeleteAppTemplate(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppTemplate(c.ID); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	list, err := s.AppTemplates()
	if err != nil || len(list) != 1 || list[0].TemplateID != "badge-access" {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestTemplateSlugsAndVersions(t *testing.T) {
	for in, want := range map[string]string{"Access Control Policy": "access-control-policy", "  ISO/IEC 27001 — A.5  ": "iso-iec-27001-a-5", "2026 plan": "template-2026-plan", "": "template"} {
		if got := TemplateSlug(in); got != want {
			t.Errorf("TemplateSlug(%q) = %q, want %q", in, got, want)
		}
	}
	taken := map[string]bool{}
	if a, b := SectionSeed("Scope", taken), SectionSeed("Scope", taken); a != "scope" || b != "scope-2" {
		t.Errorf("seeds %q %q", a, b)
	}
	for in, want := range map[string]string{"": "1.0.0", "1.0.0": "1.1.0", "2.9.0": "2.10.0", "junk": "1.0.0"} {
		if got := nextTemplateVersion(in); got != want {
			t.Errorf("nextTemplateVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

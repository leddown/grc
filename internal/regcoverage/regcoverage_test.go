package regcoverage

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"grc/internal/aiprovider"
	"grc/internal/securitynfr"
)

// ---- fakes ----

// stubAsker answers from a per-section script, falling back to replies in
// order. Keying on the section reference rather than on call order keeps the
// tests from depending on how many sections the segmenter happens to produce.
type stubAsker struct {
	bySection map[string]string
	replies   []string
	err       error
	asked     []aiprovider.Request
}

func (s *stubAsker) Ask(_ context.Context, req aiprovider.Request) (aiprovider.Response, error) {
	s.asked = append(s.asked, req)
	if s.err != nil {
		return aiprovider.Response{}, s.err
	}
	for ref, reply := range s.bySection {
		if strings.Contains(req.Prompt, "Ref: "+ref+"\n") {
			return aiprovider.Response{Text: reply, Provider: "stub", Model: "stub-1"}, nil
		}
	}
	if len(s.replies) == 0 {
		return aiprovider.Response{Text: "{}", Model: "stub-1"}, nil
	}
	reply := s.replies[0]
	if len(s.replies) > 1 {
		s.replies = s.replies[1:]
	}
	return aiprovider.Response{Text: reply, Provider: "stub", Model: "stub-1"}, nil
}

func (s *stubAsker) Available() bool  { return true }
func (s *stubAsker) Describe() string { return "stub" }

type stubNFRs struct{ list []securitynfr.NFR }

func (s stubNFRs) List(_, _ string) ([]securitynfr.NFR, error) { return s.list, nil }

func testNFRs() stubNFRs {
	return stubNFRs{list: []securitynfr.NFR{
		{Key: "NFR-ENCRYPT-TRANSIT", Summary: "Encryption of data in transit",
			Domain: "Cryptography", Description: "All network traffic carrying personal or confidential data must use TLS 1.2 or higher.",
			NISTMapping: "SC-8"},
		{Key: "NFR-INCIDENT-REPORT", Summary: "Security incident reporting",
			Domain: "Incident Response", Description: "Major incidents must be reported to the regulator within the statutory deadline.",
			NISTMapping: "IR-6"},
		{Key: "NFR-BACKUP", Summary: "Backup and restoration",
			Domain: "Resilience", Description: "Backups are taken daily and restoration is tested twice a year.",
			NISTMapping: "CP-9"},
	}}
}

// ---- ingestion ----

// sampleRegulation is deliberately worded to match no shipped profile — the
// generic fallback is the path most uploads will take, so it is the one the
// tests exercise. (An earlier draft said "network and information systems" and
// was correctly detected as NIS2.)
const sampleRegulation = `REGULATION (EU) 2099/1234 OF THE EUROPEAN PARLIAMENT

Article 1
Subject matter

This Regulation lays down uniform requirements for the resilience of critical digital infrastructure.

Article 2
Definitions

For the purposes of this Regulation, the following definitions apply: 'incident' means any event compromising availability.

Article 17
Incident reporting

Entities shall report any major incident to the competent authority without undue delay and in any event within 24 hours of becoming aware of it. The report shall describe the impact and the mitigating measures applied.

Annex I
Technical measures

Entities shall encrypt personal data in transit using state of the art cryptography.
`

func TestIngestSegmentsAndIdentifies(t *testing.T) {
	got, err := Ingest(UploadInput{Filename: "eu-2099-1234.txt", Body: []byte(sampleRegulation)})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	// Nothing in the text names a known framework, so it must fall back to the
	// generic profile rather than refusing the upload.
	if got.Regulation.Framework != GenericProfileID {
		t.Errorf("framework = %q, want the generic fallback", got.Regulation.Framework)
	}
	if got.Regulation.Detected {
		t.Error("Detected = true for a document no profile recognises")
	}

	refs := make([]string, 0, len(got.Sections))
	for _, s := range got.Sections {
		refs = append(refs, s.Ref)
	}
	// Articles and the annex both segment; the preamble is kept rather than
	// dropped, so it appears too.
	for _, want := range []string{"EU-ART-1", "EU-ART-2", "EU-ART-17", "EU-ANNEX-I"} {
		if !containsString(refs, want) {
			t.Errorf("sections %v are missing %q", refs, want)
		}
	}

	var art17 Section
	for _, s := range got.Sections {
		if s.Ref == "EU-ART-17" {
			art17 = s
		}
	}
	if !strings.Contains(art17.Body, "within 24 hours") {
		t.Errorf("Article 17 body does not carry its obligation: %q", art17.Body)
	}
	if got.Source.MediaType != "text/plain; charset=utf-8" {
		t.Errorf("source media type = %q", got.Source.MediaType)
	}
	if string(got.Source.Content) != sampleRegulation {
		t.Error("the original upload was not preserved byte for byte")
	}
}

func TestIngestRejectsBadUploads(t *testing.T) {
	tests := []struct {
		name    string
		in      UploadInput
		wantErr string
	}{
		{"empty", UploadInput{Filename: "a.txt"}, "empty"},
		{"no filename", UploadInput{Body: []byte("x")}, "filename is required"},
		{"unsupported type", UploadInput{Filename: "reg.xlsx", Body: []byte("x")}, "unsupported document type"},
		{"oversized", UploadInput{Filename: "reg.txt", Body: make([]byte, MaxUploadBytes+1)}, "the limit is"},
		{"no articles", UploadInput{Filename: "reg.txt", Body: []byte("just some prose with no structure at all")},
			"no articles or sections"},
		{"unknown framework", UploadInput{Filename: "reg.txt", Body: []byte(sampleRegulation), Framework: "nope"},
			"unknown framework"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Ingest(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// A known framework must be recognised from the document, because that is what
// brings its curated crosswalk into the analysis.
func TestIngestDetectsAKnownFramework(t *testing.T) {
	text := "Regulation (EU) 2022/2554 on digital operational resilience\n\n" +
		"Article 5\nICT risk management framework\n\n" +
		"Financial entities shall have an internal governance and control framework that ensures " +
		"an effective and prudent management of ICT risk.\n"
	got, err := Ingest(UploadInput{Filename: "dora.txt", Body: []byte(text)})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if got.Regulation.Framework != "dora" {
		t.Fatalf("framework = %q, want dora", got.Regulation.Framework)
	}
	if !got.Regulation.Detected {
		t.Error("Detected = false for a document that names its own regulation")
	}
	if got.Regulation.SourceRef != "EU 2022/2554" {
		t.Errorf("SourceRef = %q, want the profile's", got.Regulation.SourceRef)
	}
}

// ---- retrieval ----

func TestShortlistRanksTheRightCandidates(t *testing.T) {
	cat, err := LoadCatalog(testNFRs())
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	controls, nfrs := cat.Size()
	if controls == 0 {
		t.Fatal("the 800-53 catalog loaded empty")
	}
	if nfrs != 3 {
		t.Fatalf("loaded %d NFRs, want 3", nfrs)
	}

	section := Section{
		Ref: "EU-ART-17", Label: "Article 17", Title: "Incident reporting",
		Body: "Entities shall report any major incident to the competent authority without undue " +
			"delay and in any event within 24 hours of becoming aware of it.",
	}
	shortlist := NewLexicalRetriever().Shortlist(section, cat.Candidates, 5)
	if len(shortlist) == 0 {
		t.Fatal("Shortlist returned nothing")
	}

	var gotNFR, gotControl bool
	refs := make([]string, 0, len(shortlist))
	for _, c := range shortlist {
		refs = append(refs, c.Ref)
		switch c.Kind {
		case KindNFR:
			gotNFR = true
		case KindControl:
			gotControl = true
		}
	}
	if !gotNFR || !gotControl {
		t.Errorf("shortlist %v does not cover both kinds", refs)
	}
	// The incident-reporting NFR is the one this section is about; a shortlist
	// that misses it would leave the model unable to map the section correctly.
	if !containsString(refs, "NFR-INCIDENT-REPORT") {
		t.Errorf("shortlist %v is missing the incident reporting NFR", refs)
	}
	// Both kinds are capped independently, so one kind cannot crowd out the other.
	perKind := map[string]int{}
	for _, c := range shortlist {
		perKind[c.Kind]++
	}
	for kind, n := range perKind {
		if n > 5 {
			t.Errorf("%s candidates = %d, want at most 5", kind, n)
		}
	}
}

// ---- prompt and parsing ----

func TestSectionPromptCarriesEverythingTheModelNeeds(t *testing.T) {
	prompt := SectionPrompt(
		Regulation{Title: "Test Act", FrameworkName: "DORA", SourceRef: "EU 2022/2554"},
		Section{Ref: "DORA-ART-17", Label: "Article 17", Title: "Incident reporting",
			Body: "Entities shall report within 24 hours."},
		[]Candidate{{Kind: KindControl, Ref: "IR-6", Title: "Incident Reporting", Text: "Report incidents."}},
		[]Mapping{{Kind: KindControl, Ref: "IR-4", Title: "Incident Handling", Rationale: "curated"}},
	)

	for _, want := range []string{
		"Test Act", "DORA-ART-17", "Article 17 Incident reporting",
		"Entities shall report within 24 hours.",
		"[control] IR-6 — Incident Reporting",
		"IR-4", "strong prior",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q\n%s", want, prompt)
		}
	}
}

func TestParseSectionReply(t *testing.T) {
	t.Run("plain JSON", func(t *testing.T) {
		reply, err := ParseSectionReply(`{"relevant":true,"requirement":"report fast","mappings":[{"kind":"control","ref":"IR-6"}]}`)
		if err != nil {
			t.Fatalf("ParseSectionReply: %v", err)
		}
		if reply.Requirement != "report fast" || len(reply.Mappings) != 1 {
			t.Errorf("parsed = %+v", reply)
		}
	})

	// Models wrap JSON in fences and preamble; refusing those would fail runs
	// over a formatting habit rather than a real problem.
	t.Run("fenced with preamble", func(t *testing.T) {
		raw := "Here is the analysis:\n```json\n{\"requirement\":\"x\",\"commentary\":\"y\"}\n```\n"
		reply, err := ParseSectionReply(raw)
		if err != nil {
			t.Fatalf("ParseSectionReply: %v", err)
		}
		if reply.Requirement != "x" {
			t.Errorf("requirement = %q", reply.Requirement)
		}
	})

	t.Run("rejects prose", func(t *testing.T) {
		if _, err := ParseSectionReply("I cannot analyse this section."); err == nil {
			t.Fatal("ParseSectionReply accepted a non-JSON reply")
		}
	})

	t.Run("rejects an empty analysis", func(t *testing.T) {
		if _, err := ParseSectionReply(`{"relevant":true}`); err == nil {
			t.Fatal("ParseSectionReply accepted an empty analysis")
		}
	})
}

// The grounding check is what stops a confident analysis of text that is not
// there, so it has to tolerate reformatting without tolerating invention.
func TestQuoteAppearsIn(t *testing.T) {
	body := "Entities shall report any major incident\nto the competent authority within 24 hours."
	tests := []struct {
		name  string
		quote string
		want  bool
	}{
		{"verbatim", "report any major incident", true},
		{"re-wrapped across the source's line break", "major incident to the competent authority", true},
		{"case and punctuation normalised", "Report Any Major Incident,", true},
		{"invented", "report within seven days", false},
		{"too short to be evidence", "shall", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := quoteAppearsIn(tc.quote, body); got != tc.want {
				t.Errorf("quoteAppearsIn(%q) = %v, want %v", tc.quote, got, tc.want)
			}
		})
	}
}

// A model that names a control which does not exist must be caught, not
// rendered into a client-facing report as though it were real.
func TestResolveMappingsChecksTheCatalog(t *testing.T) {
	cat, err := LoadCatalog(testNFRs())
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	reply, err := ParseSectionReply(`{"requirement":"x","mappings":[
		{"kind":"control","ref":"IR-6","rationale":"real"},
		{"kind":"control","ref":"ZZ-99","rationale":"invented"},
		{"kind":"nfr","ref":"NFR-INCIDENT-REPORT","rationale":"real nfr"},
		{"kind":"control","ref":"IR-6","rationale":"duplicate"}
	]}`)
	if err != nil {
		t.Fatalf("ParseSectionReply: %v", err)
	}

	got := resolveMappings(reply, cat, []Mapping{{Kind: KindControl, Ref: "IR-6"}})
	if len(got) != 3 {
		t.Fatalf("got %d mappings, want 3 (the duplicate dropped): %+v", len(got), got)
	}
	byRef := map[string]Mapping{}
	for _, m := range got {
		byRef[m.Ref] = m
	}
	if !byRef["IR-6"].Known || byRef["IR-6"].Title == "" {
		t.Errorf("IR-6 was not resolved against the catalog: %+v", byRef["IR-6"])
	}
	// The curated crosswalk proposed IR-6 and the model confirmed it; the
	// report says so, because that is stronger evidence than either alone.
	if byRef["IR-6"].Source != SourceSeed {
		t.Errorf("IR-6 source = %q, want %q", byRef["IR-6"].Source, SourceSeed)
	}
	if byRef["ZZ-99"].Known {
		t.Error("ZZ-99 is not a real control but was marked known")
	}
	if !byRef["NFR-INCIDENT-REPORT"].Known || byRef["NFR-INCIDENT-REPORT"].Kind != KindNFR {
		t.Errorf("the NFR did not resolve: %+v", byRef["NFR-INCIDENT-REPORT"])
	}
}

// ---- statistics ----

func TestComputeStats(t *testing.T) {
	sections := []Section{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	findings := []Finding{
		{SectionID: 1, Relevant: true, Grounded: true, Confidence: ConfidenceHigh,
			Mappings: []Mapping{{Kind: KindControl, Ref: "IR-6", Known: true}, {Kind: KindNFR, Ref: "NFR-A", Known: true}}},
		{SectionID: 2, Relevant: true, Grounded: true, Confidence: ConfidenceLow},
		{SectionID: 3, Relevant: false, Grounded: true, Confidence: ConfidenceHigh},
		{SectionID: 4, Relevant: true, Grounded: false, Confidence: ConfidenceMedium,
			Mappings: []Mapping{{Kind: KindControl, Ref: "IR-6", Known: true}, {Kind: KindControl, Ref: "ZZ-9"}}},
	}

	got := computeStats(sections, findings)
	want := Stats{
		Sections: 4, Analyzed: 4, Relevant: 3, Mapped: 2, Unmapped: 1, NotRelevant: 1,
		ControlMappings: 3, NFRMappings: 1, LowConfidence: 1, Ungrounded: 1, UnknownRefs: 1,
		DistinctControls: 2, DistinctNFRs: 1,
	}
	if got != want {
		t.Errorf("stats = %+v\nwant  %+v", got, want)
	}
	if got.CoveragePercent() != 66 {
		t.Errorf("CoveragePercent = %d, want 66", got.CoveragePercent())
	}

	// Nothing relevant must not read as complete coverage of nothing.
	empty := computeStats(sections, []Finding{{SectionID: 1, Relevant: false}})
	if empty.CoveragePercent() != 0 {
		t.Errorf("CoveragePercent with nothing relevant = %d, want 0", empty.CoveragePercent())
	}
}

// ---- end to end, against a stub provider ----

func analysisReply(relevant bool, quote string, refs ...string) string {
	var mappings []string
	for _, ref := range refs {
		kind := "control"
		if strings.HasPrefix(ref, "NFR-") {
			kind = "nfr"
		}
		mappings = append(mappings, fmt.Sprintf(
			`{"kind":%q,"ref":%q,"rationale":"because","confidence":"high"}`, kind, ref))
	}
	return fmt.Sprintf(`{"relevant":%t,"requirement":"the obligation","confidence":"high",
		"quote":%q,"commentary":"do it properly","gaps":"","mappings":[%s]}`,
		relevant, quote, strings.Join(mappings, ","))
}

// analysisScript is a full set of section replies for sampleRegulation under
// the generic profile: the preamble and the first two articles impose no
// security obligation, Article 17 and the annex do. Every quote is copied out
// of the document, so a grounding failure in a test means the check broke.
func analysisScript() map[string]string {
	return map[string]string{
		"EU-GENERIC-PREAMBLE": analysisReply(false, "OF THE EUROPEAN PARLIAMENT"),
		"EU-ART-1":            analysisReply(false, "uniform requirements for the resilience"),
		"EU-ART-2":            analysisReply(false, "For the purposes of this Regulation"),
		"EU-ART-17":           analysisReply(true, "within 24 hours of becoming aware of it", "IR-6", "NFR-INCIDENT-REPORT"),
		"EU-ANNEX-I":          analysisReply(true, "encrypt personal data in transit", "SC-8", "NFR-ENCRYPT-TRANSIT"),
	}
}

func newTestService(t *testing.T, asker Asker) (*Service, *memRepo) {
	t.Helper()
	repo := newMemRepo()
	svc := NewService(repo, testNFRs(), asker)
	stamp := 0
	svc.now = func() time.Time {
		stamp++
		return time.Date(2026, 8, 14, 10, 0, stamp, 0, time.UTC)
	}
	return svc, repo
}

func TestAnalyzeProducesAReportAndAVersion(t *testing.T) {
	asker := &stubAsker{
		bySection: analysisScript(),
		replies:   []string{"An executive summary of the coverage."},
	}
	svc, _ := newTestService(t, asker)

	reg, err := svc.Upload(UploadInput{Filename: "eu.txt", Body: []byte(sampleRegulation), UploadedBy: "alice"})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if reg.Status != StatusIngested {
		t.Errorf("status after upload = %q, want %q", reg.Status, StatusIngested)
	}

	result, err := svc.Analyze(context.Background(), reg.ID, "alice")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(result.Failures) != 0 {
		t.Errorf("failures = %v", result.Failures)
	}
	if result.Version.Number != 1 {
		t.Errorf("version = %d, want 1", result.Version.Number)
	}
	if result.Regulation.Status != StatusAnalyzed {
		t.Errorf("status = %q, want %q", result.Regulation.Status, StatusAnalyzed)
	}

	report, err := svc.Report(reg.ID, 0)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if report.Version.Summary != "An executive summary of the coverage." {
		t.Errorf("summary = %q", report.Version.Summary)
	}
	if report.Stats.Relevant == 0 || report.Stats.Mapped == 0 {
		t.Fatalf("stats show nothing mapped: %+v", report.Stats)
	}
	// The quotes were copied out of the document, so every finding must be
	// grounded — if this fails the grounding check has broken, not the model.
	if report.Stats.Ungrounded != 0 {
		t.Errorf("%d finding(s) ungrounded, want 0", report.Stats.Ungrounded)
	}
	if report.Stats.UnknownRefs != 0 {
		t.Errorf("%d unknown reference(s), want 0", report.Stats.UnknownRefs)
	}

	// The provenance the whole design rests on.
	for _, entry := range report.Sections {
		if entry.Finding == nil {
			continue
		}
		if entry.Finding.Model == "" || entry.Finding.PromptHash == "" {
			t.Errorf("%s has no provenance: %+v", entry.Section.Ref, entry.Finding)
		}
	}
}

func TestAnalyzeSurvivesAFailedSection(t *testing.T) {
	// One section's reply is unusable; the run must continue past it.
	script := analysisScript()
	script["EU-ART-2"] = "I refuse to answer."
	asker := &stubAsker{bySection: script, replies: []string{"Summary."}}
	svc, _ := newTestService(t, asker)
	reg, err := svc.Upload(UploadInput{Filename: "eu.txt", Body: []byte(sampleRegulation)})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	result, err := svc.Analyze(context.Background(), reg.ID, "")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(result.Failures) != 1 {
		t.Fatalf("failures = %v, want exactly one", result.Failures)
	}
	if !strings.Contains(result.Failures[0], "EU-ART-2") {
		t.Errorf("failure = %q, want it to name the section that failed", result.Failures[0])
	}
	report, err := svc.Report(reg.ID, 0)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	// The failed section still appears, marked as not analysed, rather than
	// vanishing from the report.
	var missing int
	for _, entry := range report.Sections {
		if entry.Finding == nil {
			missing++
		}
	}
	if missing != 1 {
		t.Errorf("%d sections without a finding, want 1", missing)
	}
}

func TestReviseWritesANewVersionAndKeepsTheOld(t *testing.T) {
	script := analysisScript()
	script["EU-ART-17"] = analysisReply(true, "within 24 hours of becoming aware of it", "IR-6", "AC-2")
	asker := &stubAsker{bySection: script, replies: []string{"Summary v1."}}
	svc, _ := newTestService(t, asker)
	reg, err := svc.Upload(UploadInput{Filename: "eu.txt", Body: []byte(sampleRegulation)})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if _, err := svc.Analyze(context.Background(), reg.ID, "alice"); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	before, err := svc.Report(reg.ID, 1)
	if err != nil {
		t.Fatalf("Report v1: %v", err)
	}
	if !reportMaps(before, "EU-ART-17", "AC-2") {
		t.Fatal("v1 should contain the mapping the reviewer is about to reject")
	}

	// The reviewer rejects AC-2; the revision drops it.
	asker.bySection = map[string]string{
		"EU-ART-17": analysisReply(true, "within 24 hours of becoming aware of it", "IR-6"),
	}
	after, err := svc.Revise(context.Background(), reg.ID,
		"EU-ART-17", "AC-2 is about account management, not incident reporting. Drop it.", "bob")
	if err != nil {
		t.Fatalf("Revise: %v", err)
	}

	if after.Version.Number != 2 {
		t.Errorf("version after revision = %d, want 2", after.Version.Number)
	}
	if reportMaps(after, "EU-ART-17", "AC-2") {
		t.Error("v2 still contains the rejected mapping")
	}
	if !reportMaps(after, "EU-ART-17", "IR-6") {
		t.Error("v2 dropped a mapping the reviewer did not object to")
	}
	if !strings.Contains(after.Version.Note, "EU-ART-17") {
		t.Errorf("version note does not say what changed: %q", after.Version.Note)
	}
	// The summary carries forward: a section-level correction must not blank it.
	if after.Version.Summary != "Summary v1." {
		t.Errorf("summary after revision = %q, want it carried forward", after.Version.Summary)
	}

	// The point of versioning: v1 still says what it said when it was sent out.
	v1, err := svc.Report(reg.ID, 1)
	if err != nil {
		t.Fatalf("Report v1 after revision: %v", err)
	}
	if !reportMaps(v1, "EU-ART-17", "AC-2") {
		t.Error("v1 changed when v2 was written — the snapshot is not immutable")
	}

	// The reviewer's instruction has to reach the model, or the revision is
	// just a re-run.
	last := asker.asked[len(asker.asked)-1]
	if !strings.Contains(last.Prompt, "AC-2 is about account management") {
		t.Error("the revision prompt does not carry the reviewer's instruction")
	}
	if !strings.Contains(last.Prompt, "Current analysis") {
		t.Error("the revision prompt does not carry the analysis being corrected")
	}
}

func TestChatIsGroundedInTheReport(t *testing.T) {
	asker := &stubAsker{bySection: analysisScript(), replies: []string{"Summary."}}
	svc, _ := newTestService(t, asker)
	reg, err := svc.Upload(UploadInput{Filename: "eu.txt", Body: []byte(sampleRegulation)})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Chat before analysis has no report to stand on.
	if _, err := svc.Ask(context.Background(), reg.ID, "what does this require?", ""); err == nil {
		t.Fatal("Ask succeeded before the regulation was analysed")
	}

	if _, err := svc.Analyze(context.Background(), reg.ID, ""); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	asker.bySection = nil
	asker.replies = []string{"Articles 17 and Annex I carry the security obligations."}
	turn, err := svc.Ask(context.Background(), reg.ID, "Which articles matter most?", "alice")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if turn.Role != RoleAssistant || turn.Content == "" {
		t.Errorf("turn = %+v", turn)
	}

	asked := asker.asked[len(asker.asked)-1]
	for _, want := range []string{"Report under discussion", "EU-ART-17", "IR-6"} {
		if !strings.Contains(asked.System, want) {
			t.Errorf("the chat context is missing %q", want)
		}
	}

	// Both turns are recorded, so the next question carries the conversation.
	turns, err := svc.ListChat(reg.ID)
	if err != nil {
		t.Fatalf("ListChat: %v", err)
	}
	if len(turns) != 2 || turns[0].Role != RoleUser || turns[1].Role != RoleAssistant {
		t.Fatalf("stored turns = %+v", turns)
	}

	asker.replies = []string{"Yes — Article 17 in particular."}
	if _, err := svc.Ask(context.Background(), reg.ID, "Only those two?", "alice"); err != nil {
		t.Fatalf("second Ask: %v", err)
	}
	second := asker.asked[len(asker.asked)-1]
	if len(second.History) != 2 {
		t.Fatalf("second question carried %d prior turns, want 2", len(second.History))
	}
	if second.History[0].Text != "Which articles matter most?" {
		t.Errorf("history[0] = %q", second.History[0].Text)
	}
}

// A Wintermute session is held by the server, so once one exists the transcript
// is not resent — the same rule the AI Chat dock follows.
func TestChatResumesAProviderSession(t *testing.T) {
	asker := &sessionAsker{stubAsker: stubAsker{
		bySection: analysisScript(),
		replies:   []string{"Summary."},
	}}
	svc, _ := newTestService(t, asker)
	reg, err := svc.Upload(UploadInput{Filename: "eu.txt", Body: []byte(sampleRegulation)})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if _, err := svc.Analyze(context.Background(), reg.ID, ""); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	asker.sessionID = "sess-1"
	asker.bySection = nil
	asker.replies = []string{"first answer"}
	if _, err := svc.Ask(context.Background(), reg.ID, "one?", ""); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	asker.replies = []string{"second answer"}
	if _, err := svc.Ask(context.Background(), reg.ID, "two?", ""); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	second := asker.asked[len(asker.asked)-1]
	if second.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want the session the first answer opened", second.SessionID)
	}
	if len(second.History) != 0 {
		t.Errorf("history was resent into a live session: %+v", second.History)
	}
}

type sessionAsker struct {
	stubAsker
	sessionID string
}

func (s *sessionAsker) Ask(ctx context.Context, req aiprovider.Request) (aiprovider.Response, error) {
	resp, err := s.stubAsker.Ask(ctx, req)
	resp.SessionID = s.sessionID
	return resp, err
}

func TestUploadRefusesADuplicate(t *testing.T) {
	svc, _ := newTestService(t, &stubAsker{})
	if _, err := svc.Upload(UploadInput{Filename: "eu.txt", Body: []byte(sampleRegulation)}); err != nil {
		t.Fatalf("first Upload: %v", err)
	}
	_, err := svc.Upload(UploadInput{Filename: "copy.txt", Body: []byte(sampleRegulation)})
	if err == nil || !strings.Contains(err.Error(), "already uploaded") {
		t.Fatalf("error = %v, want a duplicate complaint", err)
	}
}

func TestReportHTMLEscapesModelOutput(t *testing.T) {
	report := &Report{
		Regulation: Regulation{Title: `Act <script>alert(1)</script>`, FrameworkName: "generic"},
		Version:    Version{Number: 1},
		Sections: []Entry{{
			Section: Section{Ref: "A-1", Label: "Article 1", Title: "Scope"},
			Finding: &Finding{
				Relevant:    true,
				Requirement: `<img src=x onerror="alert(2)">`,
				Commentary:  "fine",
				Quote:       "quoted",
				Grounded:    true,
				Confidence:  ConfidenceHigh,
				Model:       "stub",
				Mappings:    []Mapping{{Kind: KindControl, Ref: `AC-2"><b>`, Known: false}},
			},
		}},
	}
	html := ReportHTML(report)
	for _, bad := range []string{"<script>alert(1)", `<img src=x onerror=`, `AC-2"><b>`} {
		if strings.Contains(html, bad) {
			t.Errorf("unescaped model or upload content in the report: %q", bad)
		}
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("the title was not escaped into the document at all")
	}
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

func reportMaps(report *Report, sectionRef, mappingRef string) bool {
	for _, entry := range report.Sections {
		if entry.Section.Ref != sectionRef || entry.Finding == nil {
			continue
		}
		for _, m := range entry.Finding.Mappings {
			if m.Ref == mappingRef {
				return true
			}
		}
	}
	return false
}

package crisisexercise

import (
	"context"
	"encoding/csv"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"grc/internal/reporting"
)

// This file produces the exercise's documents. There are three, because an
// exercise needs three and they are not the same document:
//
//   - The exercise brief and after-action report, as a paginated PDF. What gets
//     circulated, filed, and shown to an auditor.
//   - The controller's MSEL, as CSV. What the person delivering injects
//     actually works from — on a laptop, offline, sorted by the clock, with the
//     expected action in the next column.
//   - The player handout, as markdown, with the expected actions and the trap
//     door stripped out. Handing players the document that says what they are
//     supposed to do defeats the exercise, and it has happened.

// Renderer turns HTML into a PDF. The interface is local so this module depends
// on the capability rather than on the reporting package's construction.
type Renderer interface {
	Render(ctx context.Context, html []byte, opts reporting.RenderOptions) ([]byte, error)
}

// pdfTimeout bounds one render. A full after-action report with a long MSEL is
// a long document and the engine paginates all of it before returning anything.
const pdfTimeout = 120 * time.Second

// RenderPDF produces the report. Version 0 means the live record.
func (s *Service) RenderPDF(ctx context.Context, id int64, version int) ([]byte, string, error) {
	if s.renderer == nil {
		return nil, "", invalid("PDF rendering is not available on this deployment")
	}
	d, err := s.Dossier(id, version)
	if err != nil {
		return nil, "", err
	}
	pdf, err := s.renderer.Render(ctx, []byte(ReportHTML(d)), reporting.RenderOptions{
		PaperWidthInches:   8.27,
		PaperHeightInches:  11.69,
		MarginTopInches:    0.6,
		MarginBottomInches: 0.6,
		MarginLeftInches:   0.6,
		MarginRightInches:  0.6,
	})
	if err != nil {
		return nil, "", fmt.Errorf("render report: %w", err)
	}
	return pdf, reportFilename(d), nil
}

func reportFilename(d Dossier) string {
	name := d.Exercise.Reference
	if name == "" {
		name = "exercise"
	}
	suffix := "brief"
	if d.Version.Kind == VersionAfterAction || d.Stats.InjectsPlayed > 0 {
		suffix = "after-action"
	}
	if d.Version.Number > 0 {
		return fmt.Sprintf("%s-%s-v%d.pdf", name, suffix, d.Version.Number)
	}
	return fmt.Sprintf("%s-%s.pdf", name, suffix)
}

// ---- report HTML ----

// reportStyles is deliberately plain. This document is read on paper, forwarded
// as an attachment, and occasionally printed in black and white by someone in a
// board meeting; anything that depends on colour to carry meaning has to also
// carry it in words.
const reportStyles = `
  @page { size: A4; margin: 16mm 14mm; }
  * { box-sizing: border-box; }
  body { font-family: Georgia, "Times New Roman", serif; color: #1c2431; font-size: 10.5pt; line-height: 1.45; margin: 0; }
  h1 { font-size: 20pt; margin: 0 0 4px; letter-spacing: -0.01em; }
  h2 { font-size: 13pt; margin: 22px 0 6px; padding-bottom: 3px; border-bottom: 1.5px solid #1c2431; page-break-after: avoid; }
  h3 { font-size: 11pt; margin: 14px 0 4px; page-break-after: avoid; }
  h4 { font-size: 10pt; margin: 10px 0 2px; page-break-after: avoid; }
  p { margin: 0 0 6px; }
  .subtitle { font-size: 12pt; color: #4a5262; margin: 0 0 10px; }
  .meta { font-family: Arial, Helvetica, sans-serif; font-size: 8.5pt; color: #5e6672; }
  .marking { font-family: Arial, Helvetica, sans-serif; font-size: 9pt; font-weight: bold;
             letter-spacing: 0.08em; border: 1.5px solid #8b3d2e; color: #8b3d2e;
             padding: 3px 8px; display: inline-block; margin-bottom: 10px; }
  table { width: 100%; border-collapse: collapse; margin: 8px 0 14px; font-size: 9pt; }
  th, td { border: 1px solid #c9c2b4; padding: 5px 7px; text-align: left; vertical-align: top; }
  th { background: #efe9dd; font-family: Arial, Helvetica, sans-serif; font-size: 8pt;
       text-transform: uppercase; letter-spacing: 0.05em; }
  .facts td:first-child { width: 26%; font-family: Arial, Helvetica, sans-serif; font-size: 8.5pt;
                          text-transform: uppercase; letter-spacing: 0.04em; color: #5e6672; }
  .stats { display: flex; flex-wrap: wrap; gap: 10px; margin: 10px 0 16px; }
  .stat { border: 1px solid #c9c2b4; border-radius: 6px; padding: 8px 12px; min-width: 110px; }
  .stat .n { font-size: 17pt; font-weight: bold; display: block; line-height: 1.1; }
  .stat .l { font-family: Arial, Helvetica, sans-serif; font-size: 7.5pt; text-transform: uppercase;
             letter-spacing: 0.05em; color: #5e6672; }
  .inject { border-left: 3px solid #c9c2b4; padding-left: 10px; margin: 0 0 12px; page-break-inside: avoid; }
  .inject .head { font-family: Arial, Helvetica, sans-serif; font-size: 8.5pt; color: #5e6672;
                  text-transform: uppercase; letter-spacing: 0.05em; }
  .inject .body { white-space: pre-wrap; margin: 4px 0 6px; }
  .label { font-family: Arial, Helvetica, sans-serif; font-size: 8pt; text-transform: uppercase;
           letter-spacing: 0.05em; color: #5e6672; }
  .refs { font-family: Arial, Helvetica, sans-serif; font-size: 8pt; color: #4a5262; margin-top: 4px; }
  .refs .unknown { color: #8b3d2e; }
  .met { color: #0b5d3b; font-weight: bold; }
  .missed { color: #8b3d2e; font-weight: bold; }
  .warn { color: #8a5a00; font-weight: bold; }
  .quiet { color: #5e6672; }
  section { page-break-inside: auto; }
  .pagebreak { page-break-before: always; }
`

// ReportHTML renders the exercise brief and, once there is anything to report,
// the after-action report. They are one document rather than two because they
// describe the same exercise and separating them is how the design and the
// record drift apart.
func ReportHTML(d Dossier) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<title>` + esc(d.Exercise.Reference+" "+d.Exercise.Title) + `</title>`)
	b.WriteString(`<style>` + reportStyles + `</style></head><body>`)

	writeReportHeader(&b, d)
	writeReportOverview(&b, d)
	writeReportObjectives(&b, d)
	writeReportScenario(&b, d)
	writeReportClassification(&b, d)
	writeReportClocks(&b, d)
	writeReportPhases(&b, d)
	writeReportDecisions(&b, d)
	writeReportFindings(&b, d)
	writeReportCoverage(&b, d)
	writeReportRoster(&b, d)

	b.WriteString(`</body></html>`)
	return b.String()
}

func writeReportHeader(b *strings.Builder, d Dossier) {
	ex := d.Exercise
	b.WriteString(`<div class="marking">` + esc(ex.TLP) + `</div>`)
	b.WriteString(`<h1>` + esc(ex.Title) + `</h1>`)

	subtitle := ex.Reference
	if ex.EntityName != "" {
		subtitle += " · " + ex.EntityName
	}
	if j := JurisdictionByKey(ex.Jurisdiction); ex.Jurisdiction != "" {
		subtitle += " · " + j.Label
	}
	b.WriteString(`<p class="subtitle">` + esc(subtitle) + `</p>`)

	line := "Generated " + esc(d.GeneratedAt)
	if d.Version.Number > 0 {
		line = fmt.Sprintf("Version %d (%s), issued %s by %s. ", d.Version.Number,
			esc(versionKindLabel(d.Version.Kind)), esc(d.Version.CreatedAt), esc(orDash(d.Version.CreatedBy))) + line
	}
	b.WriteString(`<p class="meta">` + line + `</p>`)
	if d.Exercise.Summary != "" {
		b.WriteString(`<p>` + esc(d.Exercise.Summary) + `</p>`)
	}
}

func writeReportOverview(b *strings.Builder, d Dossier) {
	ex := d.Exercise
	b.WriteString(`<h2>Exercise at a glance</h2>`)
	b.WriteString(`<div class="stats">`)
	writeStat(b, itoa(int64(d.Stats.Phases)), "Phases")
	writeStat(b, itoa(int64(d.Stats.Injects)), "Injects")
	if d.Stats.InjectsPlayed > 0 {
		writeStat(b, fmt.Sprintf("%d%%", d.Stats.PlayedPercent()), "MSEL played")
		writeStat(b, fmt.Sprintf("%d%%", d.Stats.PerformancePercent()), "As expected")
	}
	if d.Stats.Clocks > 0 {
		writeStat(b, fmt.Sprintf("%d/%d", d.Stats.ClocksMet, d.Stats.Clocks), "Clocks met")
	}
	writeStat(b, itoa(int64(d.Stats.Findings)), "Findings")
	writeStat(b, itoa(int64(d.Stats.DistinctRefs)), "References cited")
	b.WriteString(`</div>`)

	format, _ := FormatByKey(ex.Format)
	b.WriteString(`<table class="facts">`)
	writeFact(b, "Reference", ex.Reference)
	writeFact(b, "Format", format.Label)
	writeFact(b, "Audience", audienceLabel(ex.Audience))
	writeFact(b, "Entity", ex.EntityName)
	writeFact(b, "Entity type", entityTypeLabel(ex.EntityType))
	writeFact(b, "Jurisdiction", JurisdictionByKey(ex.Jurisdiction).Label)
	writeFact(b, "Supervision", ex.Supervision)
	writeFact(b, "Critical functions in scope", ex.CriticalFunctions)
	writeFact(b, "Scheduled", ex.ScheduledFor)
	writeFact(b, "Duration", fmt.Sprintf("%d minutes", ex.DurationMinutes))
	writeFact(b, "Delivered", timeRange(ex.StartedAt, ex.EndedAt))
	writeFact(b, "Facilitator", ex.Facilitator)
	writeFact(b, "Control team", ex.ControlTeam)
	writeFact(b, "Evaluators", ex.Evaluators)
	writeFact(b, "Status", ex.Status)
	b.WriteString(`</table>`)

	if refs := referencesFor(d, OwnerExercise, ex.ID); len(refs) > 0 {
		b.WriteString(`<h3>Instruments this exercise is answerable to</h3>`)
		writeReferenceTable(b, refs)
	}
}

func writeReportObjectives(b *strings.Builder, d Dossier) {
	if len(d.Objectives) == 0 {
		return
	}
	b.WriteString(`<h2>Objectives</h2>`)
	b.WriteString(`<table><tr><th>#</th><th>Objective</th><th>Capability tested</th><th>Success criteria</th><th>Rating</th></tr>`)
	for _, o := range d.Objectives {
		b.WriteString(`<tr>`)
		b.WriteString(`<td>` + esc(o.Code) + `</td>`)
		b.WriteString(`<td>` + esc(o.Text) + refsInline(o.References) + `</td>`)
		b.WriteString(`<td>` + esc(o.Capability) + `</td>`)
		b.WriteString(`<td>` + esc(o.SuccessCriteria) + `</td>`)
		b.WriteString(`<td class="` + ratingClass(o.Rating) + `">` + esc(ratingLabel(o.Rating)) + `</td>`)
		b.WriteString(`</tr>`)
		if trim(o.Notes) != "" {
			b.WriteString(`<tr><td></td><td colspan="4" class="quiet">` + esc(o.Notes) + `</td></tr>`)
		}
	}
	b.WriteString(`</table>`)
}

func writeReportScenario(b *strings.Builder, d Dossier) {
	ex := d.Exercise
	if ex.ThreatActor == "" && ex.ThreatNarrative == "" && ex.InitialVector == "" {
		return
	}
	b.WriteString(`<h2>Scenario</h2>`)
	writeBlock(b, "Threat actor", ex.ThreatActor)
	writeBlock(b, "Initial vector", ex.InitialVector)
	writeBlock(b, "Narrative", ex.ThreatNarrative)
}

func writeReportClassification(b *strings.Builder, d Dossier) {
	c := d.Classification
	if c.ClassifiedOffset == 0 && c.AwareOffset == 0 && !c.CriticalServicesAffected && c.TeamVerdict == "" {
		return
	}
	b.WriteString(`<h2>Incident classification</h2>`)
	b.WriteString(`<table class="facts">`)
	writeFact(b, "Became aware", FormatOffset(c.AwareOffset))
	writeFact(b, "Classified", FormatOffset(c.ClassifiedOffset))
	writeFact(b, "Critical services affected", yesNo(c.CriticalServicesAffected))
	writeFact(b, "Clients and counterparts", criterion(c.ClientsAffected, c.ClientsMaterial))
	writeFact(b, "Transactions", criterion(c.TransactionsAffected, c.TransactionsMaterial))
	writeFact(b, "Reputational impact", criterion(c.ReputationalImpact, c.ReputationalMaterial))
	writeFact(b, "Duration and downtime", criterion(fmt.Sprintf("%d minutes", c.DowntimeMinutes), c.DurationMaterial))
	writeFact(b, "Geographical spread", criterion(c.GeographicalSpread, c.GeographicalMaterial))
	writeFact(b, "Data losses", criterion(c.DataLosses, c.DataLossesMaterial))
	writeFact(b, "Economic impact", criterion(c.EconomicImpact, c.EconomicMaterial))
	writeFact(b, "Personal data breach", yesNo(c.PersonalDataBreach))
	writeFact(b, "Significant under NIS2", yesNo(c.NIS2Significant))
	b.WriteString(`</table>`)

	verdict := "Not a major incident"
	class := "quiet"
	if c.Major {
		verdict = "Major incident"
		class = "missed"
	}
	b.WriteString(`<p><span class="label">Computed classification</span><br><span class="` + class + `">` +
		esc(verdict) + `</span> — ` + esc(c.Rationale) + `</p>`)

	if trim(c.TeamVerdict) != "" {
		b.WriteString(`<p><span class="label">What the team concluded</span><br>` + esc(c.TeamVerdict) + `</p>`)
	}
	if trim(c.Notes) != "" {
		b.WriteString(`<p class="quiet">` + esc(c.Notes) + `</p>`)
	}
}

func writeReportClocks(b *strings.Builder, d Dossier) {
	if len(d.Clocks) == 0 {
		return
	}
	b.WriteString(`<h2>Notification obligations and whether they were met</h2>`)
	b.WriteString(`<table><tr><th>Obligation</th><th>Owed to</th><th>Due</th><th>Sent</th><th>Outcome</th></tr>`)
	for _, c := range d.Clocks {
		b.WriteString(`<tr>`)
		b.WriteString(`<td>` + esc(c.Label) + `<br><span class="meta">` + esc(c.Basis) + `</span>`)
		if trim(c.Notes) != "" {
			b.WriteString(`<br><span class="meta quiet">` + esc(c.Notes) + `</span>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`<td>` + esc(c.Authority) + `</td>`)
		b.WriteString(`<td>` + esc(clockDue(c)) + `</td>`)
		b.WriteString(`<td>` + esc(FormatOffset(c.ActualOffset)) + `</td>`)
		b.WriteString(`<td class="` + clockClass(c.Status) + `">` + esc(clockStatusLabel(c)) + `</td>`)
		b.WriteString(`</tr>`)
		if trim(c.Evidence) != "" {
			b.WriteString(`<tr><td colspan="5" class="quiet">Evidence: ` + esc(c.Evidence) + `</td></tr>`)
		}
	}
	b.WriteString(`</table>`)
}

func writeReportPhases(b *strings.Builder, d Dossier) {
	if len(d.Phases) == 0 {
		return
	}
	b.WriteString(`<h2 class="pagebreak">The exercise, phase by phase</h2>`)
	for _, p := range d.Phases {
		b.WriteString(`<h3>` + esc(FormatOffset(p.OffsetMinutes)) + ` — ` + esc(p.Name) + `</h3>`)
		b.WriteString(`<p class="meta">Lead: ` + esc(RoleLabel(p.LeadRole)) +
			` · ` + esc(strconv.Itoa(p.DurationMinutes)) + ` minutes · ` + esc(phaseStatusLabel(p.Status)) + `</p>`)
		if p.Purpose != "" {
			b.WriteString(`<p>` + esc(p.Purpose) + `</p>`)
		}
		writeBlock(b, "Entry criteria", p.EntryCriteria)
		writeBlock(b, "Exit criteria", p.ExitCriteria)
		if len(p.References) > 0 {
			writeReferenceTable(b, p.References)
		}
		for _, in := range p.Injects {
			writeInject(b, in)
		}
	}
}

func writeInject(b *strings.Builder, in Inject) {
	b.WriteString(`<div class="inject">`)
	b.WriteString(`<div class="head">` + esc(in.Code) + ` · ` + esc(FormatOffset(in.OffsetMinutes)) +
		` · ` + esc(injectTypeLabel(in.Type)) + ` · ` + esc(channelLabel(in.Channel)))
	if in.From != "" || in.To != "" {
		b.WriteString(` · ` + esc(orDash(in.From)) + ` → ` + esc(orDash(in.To)))
	}
	b.WriteString(`</div>`)
	if in.Title != "" {
		b.WriteString(`<h4>` + esc(in.Title) + `</h4>`)
	}
	if in.Body != "" {
		b.WriteString(`<div class="body">` + esc(in.Body) + `</div>`)
	}
	if in.ExpectedActions != "" {
		b.WriteString(`<p><span class="label">Expected</span><br>` + esc(in.ExpectedActions) + `</p>`)
	}
	if in.ExpectedDecision != "" {
		b.WriteString(`<p><span class="label">Decision sought</span><br>` + esc(in.ExpectedDecision))
		if in.DecisionOwner != "" {
			b.WriteString(` <span class="meta">(` + esc(RoleLabel(in.DecisionOwner)) + `)</span>`)
		}
		b.WriteString(`</p>`)
	}
	if in.Response != nil && in.Response.Outcome != OutcomeNotPlayed {
		r := in.Response
		b.WriteString(`<p><span class="label">Observed — ` + esc(outcomeLabel(r.Outcome)))
		if r.RespondedOffset >= 0 {
			b.WriteString(` at ` + esc(FormatOffset(r.RespondedOffset)))
		}
		b.WriteString(`</span><br>` + esc(r.ActualActions))
		if trim(r.Observations) != "" {
			b.WriteString(`<br><span class="quiet">` + esc(r.Observations) + `</span>`)
		}
		b.WriteString(`</p>`)
	}
	if len(in.References) > 0 {
		b.WriteString(`<div class="refs">Exercises: ` + refsList(in.References) + `</div>`)
	}
	b.WriteString(`</div>`)
}

func writeReportDecisions(b *strings.Builder, d Dossier) {
	if len(d.Decisions) == 0 {
		return
	}
	b.WriteString(`<h2>Decision log</h2>`)
	b.WriteString(`<table><tr><th>T+</th><th>Decision</th><th>Options considered</th><th>Rationale</th><th>Taken by</th></tr>`)
	for _, dec := range d.Decisions {
		b.WriteString(`<tr>`)
		b.WriteString(`<td>` + esc(FormatOffset(dec.OffsetMinutes)) + `</td>`)
		b.WriteString(`<td>` + esc(dec.Title) + `<br>` + esc(dec.Decision))
		if !dec.Reversible {
			b.WriteString(`<br><span class="missed">Irreversible</span>`)
		}
		b.WriteString(refsInline(dec.References) + `</td>`)
		b.WriteString(`<td>` + esc(dec.Options) + `</td>`)
		b.WriteString(`<td>` + esc(dec.Rationale))
		if trim(dec.RegulatoryImplication) != "" {
			b.WriteString(`<br><span class="meta">Regulatory: ` + esc(dec.RegulatoryImplication) + `</span>`)
		}
		if trim(dec.CustomerImpact) != "" {
			b.WriteString(`<br><span class="meta">Customers: ` + esc(dec.CustomerImpact) + `</span>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`<td>` + esc(orDash(dec.MadeBy)) + `<br><span class="meta">` + esc(RoleLabel(dec.Role)) + `</span>`)
		if trim(dec.Authority) != "" {
			b.WriteString(`<br><span class="meta">Authority: ` + esc(dec.Authority) + `</span>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table>`)
}

func writeReportFindings(b *strings.Builder, d Dossier) {
	if len(d.Findings) == 0 {
		return
	}
	b.WriteString(`<h2 class="pagebreak">Findings</h2>`)
	for _, f := range d.Findings {
		b.WriteString(`<h3>` + esc(f.Code) + ` — ` + esc(f.Title) + `</h3>`)
		b.WriteString(`<p class="meta"><span class="` + severityClass(f.Severity) + `">` + esc(strings.ToUpper(f.Severity)) +
			`</span> · ` + esc(categoryLabel(f.Category)) + ` · ` + esc(f.Status))
		if f.PhaseKey != "" {
			b.WriteString(` · ` + esc(phaseKeyLabel(f.PhaseKey)))
		}
		b.WriteString(`</p>`)
		writeBlock(b, "What was observed", f.Description)
		writeBlock(b, "Evidence", f.Evidence)
		writeBlock(b, "Root cause", f.RootCause)
		writeBlock(b, "Recommendation", f.Recommendation)
		b.WriteString(`<p class="meta">Owner: ` + esc(orDash(f.Owner)) + ` · Due: ` + esc(orDash(f.DueDate)))
		if trim(f.RiskRef) != "" {
			b.WriteString(` · Risk register: ` + esc(f.RiskRef))
		}
		b.WriteString(`</p>`)
		if len(f.References) > 0 {
			b.WriteString(`<div class="refs">Relates to: ` + refsList(f.References) + `</div>`)
		}
	}
}

// writeReportCoverage is the table an internal auditor turns to first: what this
// exercise actually tested, expressed in the same vocabulary as the control
// catalog and the regulatory framework rather than in exercise prose.
func writeReportCoverage(b *strings.Builder, d Dossier) {
	rows := Coverage(d.References)
	if len(rows) == 0 {
		return
	}
	b.WriteString(`<h2>Coverage — what this exercise tested</h2>`)
	b.WriteString(`<p class="quiet">Every citation made anywhere in this exercise, inverted: each control, requirement, clause, risk or framework and the parts of the exercise that exercised it.</p>`)
	b.WriteString(`<table><tr><th>Kind</th><th>Reference</th><th>Title</th><th>Cited by</th><th>Citations</th></tr>`)
	for _, row := range rows {
		b.WriteString(`<tr>`)
		b.WriteString(`<td>` + esc(ReferenceKindLabel(row.Kind)) + `</td>`)
		b.WriteString(`<td>` + esc(row.Ref))
		if !row.Known {
			b.WriteString(` <span class="missed">(unresolved)</span>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`<td>` + esc(row.Title) + `</td>`)
		b.WriteString(`<td>` + esc(strings.Join(ownerLabels(row.Owners), ", ")) + `</td>`)
		b.WriteString(`<td>` + itoa(int64(row.Citations)) + `</td>`)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table>`)
}

func writeReportRoster(b *strings.Builder, d Dossier) {
	if len(d.Participants) == 0 {
		return
	}
	b.WriteString(`<h2>Participants</h2>`)
	b.WriteString(`<table><tr><th>Name</th><th>Role</th><th>Organisation</th><th>Capacity</th><th>Attended</th></tr>`)
	for _, p := range d.Participants {
		capacity := "Player"
		if !p.Player {
			capacity = "Exercise staff"
		}
		b.WriteString(`<tr><td>` + esc(p.Name) + `</td><td>` + esc(RoleLabel(p.RoleKey)) +
			`</td><td>` + esc(p.Org) + `</td><td>` + capacity + `</td><td>` + yesNo(p.Attended) + `</td></tr>`)
	}
	b.WriteString(`</table>`)
}

// ---- MSEL export ----

// MSELCSV renders the Master Scenario Events List as CSV for the controller.
//
// The controller's copy carries the expected actions and the evaluation notes,
// which is exactly what must not reach a player. Whoever exports this is
// responsible for where it goes, and the TLP marking is the first column so
// that a printed copy left on a table says what it is.
func MSELCSV(d Dossier) ([]byte, error) {
	var buf strings.Builder
	w := csv.NewWriter(&buf)

	if err := w.Write([]string{
		"marking", "code", "t_plus", "offset_minutes", "phase", "type", "difficulty",
		"channel", "from", "to", "title", "body", "expected_actions", "expected_decision",
		"decision_owner", "evaluation_notes", "references", "outcome", "responded_t_plus",
		"actual_actions", "observations",
	}); err != nil {
		return nil, err
	}

	injects := allInjects(d)
	sortInjectsByClock(injects)
	for _, in := range injects {
		outcome, responded, actual, observations := "", "", "", ""
		if in.Response != nil {
			outcome = in.Response.Outcome
			responded = FormatOffset(in.Response.RespondedOffset)
			actual = in.Response.ActualActions
			observations = in.Response.Observations
		}
		if err := w.Write([]string{
			d.Exercise.TLP, in.Code, FormatOffset(in.OffsetMinutes), strconv.Itoa(in.OffsetMinutes),
			phaseKeyLabel(in.PhaseKey), in.Type, in.Difficulty, in.Channel, in.From, in.To,
			in.Title, in.Body, in.ExpectedActions, in.ExpectedDecision, in.DecisionOwner,
			in.EvaluationNotes, refsPlain(in.References), outcome, responded, actual, observations,
		}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// PlayerHandoutMarkdown is the brief a participant may see: the situation, the
// scope, the roles and the ground rules, with the MSEL, the expected actions
// and the evaluation criteria removed.
//
// It exists as a separate document because the alternative — trusting people to
// scroll past the answers — does not work, and an exercise whose participants
// have read the expected actions is a rehearsal, not an assessment.
func PlayerHandoutMarkdown(d Dossier) string {
	ex := d.Exercise
	var b strings.Builder

	b.WriteString("# " + ex.Title + "\n\n")
	b.WriteString("**" + ex.TLP + "** · " + ex.Reference + "\n\n")
	if ex.Summary != "" {
		b.WriteString(ex.Summary + "\n\n")
	}

	b.WriteString("## What this is\n\n")
	format, _ := FormatByKey(ex.Format)
	b.WriteString("A " + strings.ToLower(format.Label) + " for " + strings.ToLower(audienceLabel(ex.Audience)) + ".\n\n")
	if ex.EntityName != "" {
		b.WriteString("Entity in scope: " + ex.EntityName + "\n\n")
	}
	if ex.CriticalFunctions != "" {
		b.WriteString("### Critical or important functions in scope\n\n")
		for _, line := range strings.Split(ex.CriticalFunctions, "\n") {
			if trim(line) != "" {
				b.WriteString("- " + trim(line) + "\n")
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## Ground rules\n\n")
	b.WriteString("- This is a no-fault exercise. Findings are about the system, not about people.\n")
	b.WriteString("- Play the organisation you have, not the one in the policy document. If the plan says something nobody does, say so.\n")
	b.WriteString("- If you would pick up the phone, say who you are calling and what you would say. Do not summarise it — say it.\n")
	b.WriteString("- Decisions are real. If you decide something, it holds for the rest of the exercise.\n")
	b.WriteString("- Nothing here leaves the room, and nothing here is a real notification to anyone.\n\n")

	if len(d.Objectives) > 0 {
		b.WriteString("## What we are testing\n\n")
		for _, o := range d.Objectives {
			b.WriteString("- " + o.Text + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## The situation\n\n")
	if ex.ThreatNarrative != "" {
		b.WriteString(ex.ThreatNarrative + "\n\n")
	}
	if ex.ThreatActor != "" {
		b.WriteString("**Adversary.** " + ex.ThreatActor + "\n\n")
	}

	if len(d.Phases) > 0 {
		b.WriteString("## How the session runs\n\n")
		for _, p := range d.Phases {
			b.WriteString("- **" + FormatOffset(p.OffsetMinutes) + " · " + p.Name + "** — " + p.Purpose + "\n")
		}
		b.WriteString("\n")
	}

	if len(d.Participants) > 0 {
		b.WriteString("## Who is in the room\n\n")
		for _, p := range d.Participants {
			if !p.Player {
				continue
			}
			b.WriteString("- " + p.Name + " — " + RoleLabel(p.RoleKey) + "\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// ---- rendering helpers ----

func allInjects(d Dossier) []Inject {
	var out []Inject
	for _, p := range d.Phases {
		out = append(out, p.Injects...)
	}
	return out
}

func referencesFor(d Dossier, ownerKind string, ownerID int64) []Reference {
	return IndexReferences(d.References).For(ownerKind, ownerID)
}

func writeStat(b *strings.Builder, n, label string) {
	b.WriteString(`<div class="stat"><span class="n">` + esc(n) + `</span><span class="l">` + esc(label) + `</span></div>`)
}

func writeFact(b *strings.Builder, label, value string) {
	if trim(value) == "" {
		return
	}
	b.WriteString(`<tr><td>` + esc(label) + `</td><td>` + escMultiline(value) + `</td></tr>`)
}

func writeBlock(b *strings.Builder, label, value string) {
	if trim(value) == "" {
		return
	}
	b.WriteString(`<p><span class="label">` + esc(label) + `</span><br>` + escMultiline(value) + `</p>`)
}

func writeReferenceTable(b *strings.Builder, refs []Reference) {
	b.WriteString(`<table><tr><th>Kind</th><th>Reference</th><th>Title</th><th>Note</th></tr>`)
	for _, ref := range refs {
		b.WriteString(`<tr><td>` + esc(ReferenceKindLabel(ref.RefKind)) + `</td><td>` + esc(ref.Ref))
		if !ref.Known {
			b.WriteString(` <span class="missed">(unresolved)</span>`)
		}
		b.WriteString(`</td><td>` + esc(ref.Title) + `</td><td class="quiet">` + esc(ref.Note) + `</td></tr>`)
	}
	b.WriteString(`</table>`)
}

func refsInline(refs []Reference) string {
	if len(refs) == 0 {
		return ""
	}
	return `<div class="refs">` + refsList(refs) + `</div>`
}

func refsList(refs []Reference) string {
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		label := esc(ref.Ref)
		if ref.Title != "" {
			label += " " + esc(ref.Title)
		}
		if !ref.Known {
			label = `<span class="unknown">` + label + ` (unresolved)</span>`
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " · ")
}

func refsPlain(refs []Reference) string {
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, ref.RefKind+":"+ref.Ref)
	}
	return strings.Join(parts, "; ")
}

func ownerLabels(owners []string) []string {
	out := make([]string, 0, len(owners))
	for _, o := range owners {
		switch o {
		case OwnerExercise:
			out = append(out, "exercise")
		case OwnerObjective:
			out = append(out, "objectives")
		case OwnerPhase:
			out = append(out, "phases")
		case OwnerInject:
			out = append(out, "injects")
		case OwnerDecision:
			out = append(out, "decisions")
		case OwnerFinding:
			out = append(out, "findings")
		case OwnerClock:
			out = append(out, "notification clocks")
		default:
			out = append(out, o)
		}
	}
	return out
}

func clockDue(c Clock) string {
	if c.Status == ClockNotApplicable {
		return "—"
	}
	return FormatOffset(c.DueOffset)
}

func clockStatusLabel(c Clock) string {
	switch c.Status {
	case ClockMet:
		return "Met"
	case ClockMissed:
		if c.ActualOffset < 0 {
			return "Not sent"
		}
		return fmt.Sprintf("Late by %s", strings.TrimPrefix(FormatOffset(c.LatenessMinutes()), "T+"))
	case ClockNotApplicable:
		return "Not applicable"
	default:
		return "Pending"
	}
}

func clockClass(status string) string {
	switch status {
	case ClockMet:
		return "met"
	case ClockMissed:
		return "missed"
	default:
		return "quiet"
	}
}

func severityClass(severity string) string {
	switch severity {
	case SeverityCritical, SeverityHigh:
		return "missed"
	case SeverityMedium:
		return "warn"
	default:
		return "quiet"
	}
}

func ratingClass(rating string) string {
	switch rating {
	case RatingMet:
		return "met"
	case RatingNotMet:
		return "missed"
	case RatingPartiallyMet:
		return "warn"
	default:
		return "quiet"
	}
}

func ratingLabel(rating string) string {
	switch rating {
	case RatingMet:
		return "Met"
	case RatingPartiallyMet:
		return "Partially met"
	case RatingNotMet:
		return "Not met"
	case RatingNotApplicable:
		return "Not applicable"
	default:
		return "Untested"
	}
}

func outcomeLabel(outcome string) string {
	switch outcome {
	case OutcomeAsExpected:
		return "as expected"
	case OutcomePartial:
		return "partially as expected"
	case OutcomeDeviation:
		return "deviation from expected"
	case OutcomeMissed:
		return "no action taken"
	default:
		return "not played"
	}
}

func injectTypeLabel(t string) string {
	switch t {
	case InjectStress:
		return "stress"
	case InjectAmbiguous:
		return "ambiguous information"
	case InjectDecision:
		return "decision point"
	case InjectContingency:
		return "contingency"
	case InjectInformation:
		return "information"
	default:
		return "event"
	}
}

func channelLabel(c string) string {
	return strings.ReplaceAll(c, "_", " ")
}

func categoryLabel(c string) string {
	return strings.ReplaceAll(c, "_", " ")
}

func phaseStatusLabel(s string) string {
	return strings.ReplaceAll(s, "_", " ")
}

func phaseKeyLabel(key string) string {
	if t, ok := PhaseTemplateFor(key); ok {
		return t.Name
	}
	return strings.ReplaceAll(key, "_", " ")
}

func audienceLabel(key string) string {
	for _, a := range Audiences() {
		if a.Key == key {
			return a.Label
		}
	}
	return key
}

func entityTypeLabel(key string) string {
	for _, e := range EntityTypes() {
		if e.Key == key {
			return e.Label
		}
	}
	return key
}

func versionKindLabel(kind string) string {
	if kind == VersionAfterAction {
		return "after-action report"
	}
	return "exercise brief"
}

func criterion(value string, material bool) string {
	value = trim(value)
	if value == "" {
		value = "not assessed"
	}
	if material {
		return value + " — threshold met"
	}
	return value + " — below threshold"
}

func timeRange(start, end string) string {
	switch {
	case start == "" && end == "":
		return ""
	case end == "":
		return start + " (running)"
	default:
		return start + " to " + end
	}
}

func yesNo(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}

func orDash(v string) string {
	if trim(v) == "" {
		return "—"
	}
	return v
}

func esc(s string) string { return html.EscapeString(s) }

// escMultiline preserves the line breaks in a field a person typed as a list,
// which is most of the criteria fields in this module.
func escMultiline(s string) string {
	return strings.ReplaceAll(html.EscapeString(s), "\n", "<br>")
}

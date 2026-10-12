package auditfinding

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/db"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	svc := NewService(NewSQLiteRepository(conn))
	svc.now = func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) }
	return svc
}

func baseFinding() Finding {
	return Finding{
		Title:       "Privileged access not recertified",
		Severity:    "High",
		FindingType: "Control Deficiency",
		Owner:       "Head of IAM",
		Criteria:    "AC-2(j): review accounts quarterly",
		Condition:   "No review of 14 admin accounts in 2 quarters",
	}
}

func wantInvalid(t *testing.T, err error, fragment string) {
	t.Helper()
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err=%v, want a ValidationError containing %q", err, fragment)
	}
	if !strings.Contains(verr.Error(), fragment) {
		t.Fatalf("err=%q, want it to mention %q", verr.Error(), fragment)
	}
}

func TestCreateDefaultsReferenceDatesAndSLA(t *testing.T) {
	svc := newTestService(t)
	f, err := svc.Create(baseFinding(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if f.Reference != "AF-2026-001" {
		t.Errorf("reference=%q want AF-2026-001", f.Reference)
	}
	if f.IdentifiedDate != "2026-09-27" || f.DueDate != "2026-12-26" || f.OriginalDueDate != f.DueDate {
		t.Errorf("dates identified=%s due=%s original=%s; want High SLA of 90 days", f.IdentifiedDate, f.DueDate, f.OriginalDueDate)
	}
	if f.Status != StatusOpen || len(f.History) != 1 || f.History[0].Actor != "alice" {
		t.Errorf("status=%s history=%+v", f.Status, f.History)
	}

	second, err := svc.Create(baseFinding(), "alice")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if second.Reference != "AF-2026-002" {
		t.Errorf("second reference=%q", second.Reference)
	}

	dup := baseFinding()
	dup.Reference = "AF-2026-001"
	_, err = svc.Create(dup, "alice")
	wantInvalid(t, err, "already used")
}

func TestCreateRejectsMissingOwnerAndBadVocabulary(t *testing.T) {
	svc := newTestService(t)
	f := baseFinding()
	f.Owner = ""
	_, err := svc.Create(f, "")
	wantInvalid(t, err, "owner is required")

	f = baseFinding()
	f.Severity = "Severe"
	_, err = svc.Create(f, "")
	wantInvalid(t, err, "severity must be one of")

	f = baseFinding()
	f.DueDate = "27/09/2026"
	_, err = svc.Create(f, "")
	wantInvalid(t, err, "YYYY-MM-DD")
}

func TestExtensionNeedsReasonAndIsCounted(t *testing.T) {
	svc := newTestService(t)
	f, err := svc.Create(baseFinding(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	original := f.DueDate

	f.DueDate = "2027-03-31"
	_, err = svc.Update(f.ID, f, "bob")
	wantInvalid(t, err, "extension")

	f.ExtensionReason = "Vendor patch slipped; approved by CISO 2026-09-27"
	f.ExtensionCount = 0
	f.OriginalDueDate = "2027-03-31"
	updated, err := svc.Update(f.ID, f, "bob")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ExtensionCount != 1 || updated.OriginalDueDate != original {
		t.Errorf("extension_count=%d original=%s; want 1 and %s kept from the first commitment",
			updated.ExtensionCount, updated.OriginalDueDate, original)
	}
	if updated.History[0].Event != "due date extended" {
		t.Errorf("latest history=%+v", updated.History[0])
	}
}

func TestLifecycleGates(t *testing.T) {
	svc := newTestService(t)
	f, err := svc.Create(baseFinding(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	f.Status = StatusInRemediation
	_, err = svc.Update(f.ID, f, "alice")
	wantInvalid(t, err, "at least one action")

	f.ManagementResponse = ResponseDisagree
	f.Actions = []Action{{Description: "Quarterly recertification", Owner: "IAM lead"}}
	_, err = svc.Update(f.ID, f, "alice")
	wantInvalid(t, err, "disagrees")

	f.ManagementResponse = ResponseAgree
	f, err = svc.Update(f.ID, f, "alice")
	if err != nil {
		t.Fatalf("to In Remediation: %v", err)
	}
	if f.Actions[0].DueDate != f.DueDate || f.Actions[0].Status != ActionNotStarted || f.Actions[0].Reference != "AF-2026-001-A1" {
		t.Errorf("action defaults=%+v", f.Actions[0])
	}

	f.Status = StatusPendingValidation
	_, err = svc.Update(f.ID, f, "alice")
	wantInvalid(t, err, "must be Implemented")

	f.Actions[0].Status = ActionImplemented
	f, err = svc.Update(f.ID, f, "alice")
	if err != nil {
		t.Fatalf("to Pending Validation: %v", err)
	}
	if f.Actions[0].CompletedDate != "2026-09-27" {
		t.Errorf("completed_date=%q want today", f.Actions[0].CompletedDate)
	}

	f.Status = StatusClosed
	_, err = svc.Update(f.ID, f, "alice")
	wantInvalid(t, err, "validated_by")

	f.ValidatedBy, f.ValidationDate, f.ClosureEvidence = "Head of IAM", "2026-09-27", "Q3 recert pack"
	_, err = svc.Update(f.ID, f, "alice")
	wantInvalid(t, err, "independent")

	f.ValidatedBy = "Internal Audit — J. Doe"
	f, err = svc.Update(f.ID, f, "auditor")
	if err != nil {
		t.Fatalf("to Closed: %v", err)
	}
	if f.ClosedDate != "2026-09-27" || f.Actions[0].Status != ActionValidated {
		t.Errorf("closed_date=%q action=%s; closing validates implemented actions", f.ClosedDate, f.Actions[0].Status)
	}

	f.Status = StatusInRemediation
	f, err = svc.Update(f.ID, f, "alice")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if f.Status != StatusReopened || f.ClosedDate != "" {
		t.Errorf("status=%s closed=%q; a closed finding that returns is Reopened", f.Status, f.ClosedDate)
	}
}

func TestRiskAcceptanceNeedsApproverRationaleAndFutureExpiry(t *testing.T) {
	svc := newTestService(t)
	f, err := svc.Create(baseFinding(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.Status = StatusRiskAccepted
	_, err = svc.Update(f.ID, f, "alice")
	wantInvalid(t, err, "risk_accepted_by")

	f.RiskAcceptedBy, f.RiskAcceptanceRationale, f.RiskAcceptanceExpiry = "CRO", "Legacy system retired Q2", "2026-01-01"
	_, err = svc.Update(f.ID, f, "alice")
	wantInvalid(t, err, "future")

	f.RiskAcceptanceExpiry = "2027-06-30"
	if _, err := svc.Update(f.ID, f, "alice"); err != nil {
		t.Fatalf("accept: %v", err)
	}
}

func TestAdvisoryFindingsCloseWithoutActionPlan(t *testing.T) {
	svc := newTestService(t)
	f := baseFinding()
	f.FindingType = "Opportunity for Improvement"
	created, err := svc.Create(f, "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.DueDate != "" {
		t.Errorf("advisory due_date=%q; an OFI carries no remediation deadline", created.DueDate)
	}
	created.Status = StatusClosed
	created.ValidatedBy, created.ValidationDate, created.ClosureEvidence = "IA", "2026-09-27", "Considered; not adopted"
	if _, err := svc.Update(created.ID, created, "alice"); err != nil {
		t.Fatalf("close OFI: %v", err)
	}
}

func TestSummaryAndOverdue(t *testing.T) {
	svc := newTestService(t)
	late := baseFinding()
	late.Severity = "Critical"
	late.IdentifiedDate = "2026-01-10"
	late.RepeatFinding = true
	late.PriorReference = "AF-2025-004"
	if _, err := svc.Create(late, "a"); err != nil {
		t.Fatalf("Create late: %v", err)
	}
	if _, err := svc.Create(baseFinding(), "a"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	sum, err := svc.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if sum.Open != 2 || sum.Overdue != 1 || sum.RepeatOpen != 1 || sum.OpenBySeverity["Critical"] != 1 {
		t.Errorf("summary=%+v", sum)
	}
	if sum.OpenByAging["181-365"] != 1 || sum.OpenByAging["0-30"] != 1 {
		t.Errorf("aging=%v", sum.OpenByAging)
	}

	overdue, err := svc.List(Filter{OverdueOnly: true})
	if err != nil || len(overdue) != 1 || overdue[0].DaysOverdue != 230 {
		t.Fatalf("overdue list=%+v err=%v", overdue, err)
	}
}

func TestPageAndWriteStatusCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newTestService(t)
	r := gin.New()
	h := NewHandler(svc, func(*gin.Context) string { return "tester" })
	h.RegisterRoutes(r)
	h.RegisterAdminRoutes(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/audit-findings", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `href="/audit-findings"`) {
		t.Fatalf("page status=%d", rec.Code)
	}
	for _, fragment := range []string{"color-scheme: light;", "--ink: ", "color: var(--ink);", "max-width: 1320px;", "margin: 24px auto;", "padding: 24px;"} {
		if !strings.Contains(rec.Body.String(), fragment) {
			t.Errorf("page missing shared palette/spacing fragment %q", fragment)
		}
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/audit-findings", strings.NewReader(`{"title":"x"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid create status=%d want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/audit-findings/99", strings.NewReader(`{"title":"x","owner":"o","severity":"Low"}`)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("update missing status=%d want 404", rec.Code)
	}
}

package auditfinding

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("audit finding not found")

// ValidationError is a rejection the caller can fix, as opposed to a storage
// failure; the handler maps it to 400.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

const maxFieldLength = 20000

const dateLayout = "2006-01-02"

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

func (s *Service) today() string { return s.now().UTC().Format(dateLayout) }

func (s *Service) List(filter Filter) ([]Finding, error) {
	filter.Search = strings.TrimSpace(filter.Search)
	items, err := s.repo.List(filter)
	if err != nil {
		return nil, err
	}
	today := s.today()
	out := items[:0]
	for _, f := range items {
		derive(&f, today)
		if filter.OverdueOnly && !f.Overdue {
			continue
		}
		if filter.OpenOnly && !isOpenStatus(f.Status) {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

func (s *Service) Get(id int64) (Finding, error) {
	f, err := s.repo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return Finding{}, ErrNotFound
	}
	if err != nil {
		return Finding{}, err
	}
	derive(&f, s.today())
	return f, nil
}

func (s *Service) Create(f Finding, actor string) (Finding, error) {
	today := s.today()
	if err := normalize(&f); err != nil {
		return Finding{}, err
	}
	if f.Status == "" {
		f.Status = StatusOpen
	}
	if f.Status == StatusReopened {
		return Finding{}, invalid("a new finding cannot start as Reopened")
	}
	if f.IdentifiedDate == "" {
		f.IdentifiedDate = today
	}
	if f.DueDate == "" && !advisoryTypes[f.FindingType] {
		f.DueDate = addDays(f.IdentifiedDate, severitySLADays[f.Severity])
	}
	f.OriginalDueDate = f.DueDate
	f.ExtensionCount = 0
	if f.Reference == "" {
		ref, err := s.nextReference()
		if err != nil {
			return Finding{}, err
		}
		f.Reference = ref
	} else if err := s.ensureReferenceFree(f.Reference); err != nil {
		return Finding{}, err
	}
	defaultActions(&f, today)
	if err := checkGates(&f, today, ""); err != nil {
		return Finding{}, err
	}
	if f.Status == StatusClosed && f.ClosedDate == "" {
		f.ClosedDate = firstNonEmpty(f.ValidationDate, today)
	}

	stamp := s.now().UTC().Format(time.RFC3339)
	f.CreatedAt, f.CreatedBy, f.UpdatedAt, f.UpdatedBy = stamp, actor, stamp, actor
	id, err := s.repo.Create(f, []HistoryEntry{{At: stamp, Actor: actor, Event: "created", ToValue: f.Status}})
	if err != nil {
		return Finding{}, err
	}
	return s.Get(id)
}

func (s *Service) Update(id int64, f Finding, actor string) (Finding, error) {
	today := s.today()
	prev, err := s.repo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return Finding{}, ErrNotFound
	}
	if err != nil {
		return Finding{}, err
	}
	if err := normalize(&f); err != nil {
		return Finding{}, err
	}
	if f.Reference == "" {
		f.Reference = prev.Reference
	} else if f.Reference != prev.Reference {
		if err := s.ensureReferenceFree(f.Reference); err != nil {
			return Finding{}, err
		}
	}
	if f.Status == "" {
		f.Status = prev.Status
	}
	// A closed finding that comes back is recorded as Reopened whatever open
	// state was picked, so the register never loses the fact that it recurred.
	if prev.Status == StatusClosed && isOpenStatus(f.Status) {
		f.Status = StatusReopened
	}
	if f.Status != StatusClosed {
		f.ClosedDate = ""
	}

	stamp := s.now().UTC().Format(time.RFC3339)
	var history []HistoryEntry
	record := func(event, from, to, note string) {
		history = append(history, HistoryEntry{At: stamp, Actor: actor, Event: event, FromValue: from, ToValue: to, Note: note})
	}

	// The client never sets the commitment trail; it is carried from the stored
	// row so extensions cannot be hidden by editing the payload.
	f.OriginalDueDate = prev.OriginalDueDate
	f.ExtensionCount = prev.ExtensionCount
	if f.DueDate == "" {
		f.DueDate = prev.DueDate
	}
	if f.OriginalDueDate == "" {
		f.OriginalDueDate = f.DueDate
	}
	if prev.DueDate != "" && f.DueDate != prev.DueDate {
		if f.DueDate > prev.DueDate {
			if f.ExtensionReason == "" || f.ExtensionReason == prev.ExtensionReason {
				return Finding{}, invalid("moving the due date from %s to %s is an extension: give the reason and who approved it in extension_reason", prev.DueDate, f.DueDate)
			}
			f.ExtensionCount++
			record("due date extended", prev.DueDate, f.DueDate, f.ExtensionReason)
		} else {
			record("due date brought forward", prev.DueDate, f.DueDate, "")
		}
	}

	defaultActions(&f, today)
	if err := checkGates(&f, today, prev.Status); err != nil {
		return Finding{}, err
	}
	if f.Status == StatusClosed && f.ClosedDate == "" {
		f.ClosedDate = firstNonEmpty(prev.ClosedDate, f.ValidationDate, today)
	}

	if f.Status != prev.Status {
		record("status", prev.Status, f.Status, statusNote(f))
	}
	if f.ManagementResponse != prev.ManagementResponse {
		record("management response", prev.ManagementResponse, f.ManagementResponse, "")
	}
	if f.Severity != prev.Severity {
		record("severity", prev.Severity, f.Severity, "")
	}
	if f.Owner != prev.Owner {
		record("owner", prev.Owner, f.Owner, "")
	}
	if summary := actionPlanSummary(f.Actions); summary != actionPlanSummary(prev.Actions) {
		record("action plan", actionPlanSummary(prev.Actions), summary, "")
	}

	f.CreatedAt, f.CreatedBy = prev.CreatedAt, prev.CreatedBy
	f.UpdatedAt, f.UpdatedBy = stamp, actor
	if err := s.repo.Update(id, f, history); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Finding{}, ErrNotFound
		}
		return Finding{}, err
	}
	return s.Get(id)
}

func (s *Service) Delete(id int64) error {
	err := s.repo.Delete(id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *Service) Summary() (Summary, error) {
	items, err := s.List(Filter{})
	if err != nil {
		return Summary{}, err
	}
	today := s.today()
	cutoff := addDays(today, -90)
	sum := Summary{
		ByStatus:       map[string]int{},
		OpenBySeverity: map[string]int{},
		OpenByAging:    map[string]int{},
	}
	closedDays, closedCount := 0, 0
	for _, f := range items {
		sum.Total++
		sum.ByStatus[f.Status]++
		if f.ExtensionCount > 0 {
			sum.Extended++
		}
		switch f.Status {
		case StatusPendingValidation:
			sum.PendingValidation++
		case StatusRiskAccepted:
			sum.RiskAccepted++
			if f.RiskAcceptanceExpiry != "" && f.RiskAcceptanceExpiry < today {
				sum.AcceptanceExpired++
			}
		case StatusClosed:
			if f.ClosedDate >= cutoff {
				sum.ClosedLast90Days++
			}
			if f.IdentifiedDate != "" && f.ClosedDate != "" {
				closedDays += daysBetween(f.IdentifiedDate, f.ClosedDate)
				closedCount++
			}
		}
		if !isOpenStatus(f.Status) {
			continue
		}
		sum.Open++
		sum.OpenBySeverity[f.Severity]++
		sum.OpenByAging[f.AgingBucket]++
		if f.Overdue {
			sum.Overdue++
		}
		if f.RepeatFinding {
			sum.RepeatOpen++
		}
	}
	if closedCount > 0 {
		sum.AvgDaysToClose = closedDays / closedCount
	}
	return sum, nil
}

func (s *Service) nextReference() (string, error) {
	prefix := fmt.Sprintf("AF-%d-", s.now().UTC().Year())
	refs, err := s.repo.ReferencesWithPrefix(prefix)
	if err != nil {
		return "", fmt.Errorf("allocate audit finding reference: %w", err)
	}
	next := 1
	for _, ref := range refs {
		if n, err := strconv.Atoi(strings.TrimPrefix(ref, prefix)); err == nil && n >= next {
			next = n + 1
		}
	}
	return fmt.Sprintf("%s%03d", prefix, next), nil
}

func (s *Service) ensureReferenceFree(ref string) error {
	refs, err := s.repo.ReferencesWithPrefix(ref)
	if err != nil {
		return err
	}
	if slices.Contains(refs, ref) {
		return invalid("reference %s is already used by another finding", ref)
	}
	return nil
}

// checkGates enforces the lifecycle rules that make a closure mean something.
// prevStatus is empty for a new finding.
func checkGates(f *Finding, today, prevStatus string) error {
	live := liveActions(f.Actions)
	advisory := advisoryTypes[f.FindingType]

	switch f.Status {
	case StatusInRemediation:
		if f.ManagementResponse == ResponseDisagree {
			return invalid("management disagrees with this finding: escalate it or record a risk acceptance rather than tracking remediation")
		}
		if len(live) == 0 {
			return invalid("In Remediation needs at least one action plan item with an owner and a due date")
		}
	case StatusPendingValidation:
		if len(live) == 0 && !advisory {
			return invalid("Pending Validation needs an action plan to validate")
		}
		for _, a := range live {
			if a.Status != ActionImplemented && a.Status != ActionValidated {
				return invalid("action %q is %s: every action must be Implemented before the finding goes to validation", a.Description, a.Status)
			}
		}
	case StatusClosed:
		if f.ValidatedBy == "" || f.ValidationDate == "" || f.ClosureEvidence == "" {
			return invalid("closing needs validated_by, validation_date and closure_evidence: a finding closes on validated evidence, not on management's say-so")
		}
		if strings.EqualFold(f.ValidatedBy, f.Owner) {
			return invalid("the validator must be independent of the finding owner")
		}
		if len(live) == 0 && !advisory {
			return invalid("a %s cannot close without a completed action plan; use Risk Accepted if it will not be fixed", firstNonEmpty(f.FindingType, "finding"))
		}
		for i := range f.Actions {
			a := &f.Actions[i]
			switch a.Status {
			case ActionCancelled, ActionValidated:
			case ActionImplemented:
				a.Status = ActionValidated
			default:
				return invalid("action %q is %s: every action must be Implemented before the finding can close", a.Description, a.Status)
			}
		}
	case StatusRiskAccepted:
		if f.RiskAcceptedBy == "" || f.RiskAcceptanceRationale == "" || f.RiskAcceptanceExpiry == "" {
			return invalid("risk acceptance needs risk_accepted_by, risk_acceptance_rationale and risk_acceptance_expiry")
		}
		if prevStatus != StatusRiskAccepted && f.RiskAcceptanceExpiry <= today {
			return invalid("risk acceptance expiry must be in the future")
		}
	}
	return nil
}

func statusNote(f Finding) string {
	switch f.Status {
	case StatusClosed:
		return "validated by " + f.ValidatedBy + " on " + f.ValidationDate
	case StatusRiskAccepted:
		return "accepted by " + f.RiskAcceptedBy + " until " + f.RiskAcceptanceExpiry
	}
	return ""
}

func liveActions(actions []Action) []Action {
	out := make([]Action, 0, len(actions))
	for _, a := range actions {
		if a.Status != ActionCancelled {
			out = append(out, a)
		}
	}
	return out
}

func actionPlanSummary(actions []Action) string {
	if len(actions) == 0 {
		return "none"
	}
	counts := map[string]int{}
	for _, a := range actions {
		counts[a.Status]++
	}
	parts := []string{}
	for _, st := range ActionStatuses {
		if counts[st] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[st], strings.ToLower(st)))
		}
	}
	return fmt.Sprintf("%d actions (%s)", len(actions), strings.Join(parts, ", "))
}

func defaultActions(f *Finding, today string) {
	for i := range f.Actions {
		a := &f.Actions[i]
		if a.DueDate == "" {
			a.DueDate = f.DueDate
		}
		if (a.Status == ActionImplemented || a.Status == ActionValidated) && a.CompletedDate == "" {
			a.CompletedDate = today
		}
		if a.Reference == "" {
			a.Reference = fmt.Sprintf("%s-A%d", f.Reference, i+1)
		}
	}
}

// derive fills the fields that depend on today's date.
func derive(f *Finding, today string) {
	end := today
	if f.Status == StatusClosed && f.ClosedDate != "" {
		end = f.ClosedDate
	}
	if f.IdentifiedDate != "" {
		f.DaysOpen = max(daysBetween(f.IdentifiedDate, end), 0)
	}
	open := isOpenStatus(f.Status)
	if open && f.DueDate != "" && f.DueDate < today {
		f.Overdue = true
		f.DaysOverdue = daysBetween(f.DueDate, today)
	}
	if open {
		f.AgingBucket = agingBucket(f.DaysOpen)
	}
	for i := range f.Actions {
		a := &f.Actions[i]
		switch a.Status {
		case ActionImplemented, ActionValidated, ActionCancelled:
		default:
			a.Overdue = a.DueDate != "" && a.DueDate < today
		}
	}
}

func agingBucket(days int) string {
	switch {
	case days <= 30:
		return "0-30"
	case days <= 90:
		return "31-90"
	case days <= 180:
		return "91-180"
	case days <= 365:
		return "181-365"
	}
	return "365+"
}

func normalize(f *Finding) error {
	for _, p := range []*string{
		&f.Reference, &f.Title, &f.Source, &f.Engagement, &f.Auditor, &f.FindingType, &f.Severity,
		&f.DeficiencyType, &f.Criteria, &f.Condition, &f.Cause, &f.Effect, &f.Recommendation,
		&f.RootCauseCategory, &f.BusinessUnit, &f.ProcessArea, &f.Owner, &f.ManagementResponse,
		&f.ManagementResponseText, &f.IdentifiedDate, &f.ReportDate, &f.DueDate, &f.ExtensionReason,
		&f.Status, &f.PriorReference, &f.LinkedRisks, &f.LinkedControls, &f.FrameworkRefs,
		&f.ValidatedBy, &f.ValidationDate, &f.ValidationNotes, &f.ClosureEvidence, &f.ClosedDate,
		&f.RiskAcceptedBy, &f.RiskAcceptanceRationale, &f.RiskAcceptanceExpiry, &f.Notes,
	} {
		*p = strings.TrimSpace(*p)
		if len(*p) > maxFieldLength {
			return invalid("fields must be at most %d characters", maxFieldLength)
		}
	}
	if f.Title == "" {
		return invalid("title is required")
	}
	if f.Owner == "" {
		return invalid("owner is required: every finding needs one accountable owner")
	}
	if f.FindingType == "" {
		f.FindingType = FindingTypes[0]
	}
	for _, check := range []struct {
		name, value string
		allowed     []string
		required    bool
	}{
		{"severity", f.Severity, Severities, true},
		{"finding_type", f.FindingType, FindingTypes, true},
		{"status", f.Status, Statuses, false},
		{"source", f.Source, Sources, false},
		{"deficiency_type", f.DeficiencyType, DeficiencyTypes, false},
		{"root_cause_category", f.RootCauseCategory, RootCauseCategories, false},
		{"management_response", f.ManagementResponse, ManagementResponses, false},
	} {
		if check.value == "" && !check.required {
			continue
		}
		if !slices.Contains(check.allowed, check.value) {
			return invalid("%s must be one of: %s", check.name, strings.Join(check.allowed, ", "))
		}
	}
	for name, value := range map[string]string{
		"identified_date": f.IdentifiedDate, "report_date": f.ReportDate, "due_date": f.DueDate,
		"validation_date": f.ValidationDate, "closed_date": f.ClosedDate,
		"risk_acceptance_expiry": f.RiskAcceptanceExpiry,
	} {
		if err := checkDate(name, value); err != nil {
			return err
		}
	}
	if !f.RepeatFinding {
		f.PriorReference = ""
	}

	actions := make([]Action, 0, len(f.Actions))
	for i, a := range f.Actions {
		for _, p := range []*string{&a.Reference, &a.Description, &a.ActionType, &a.Owner, &a.DueDate, &a.Status, &a.CompletedDate, &a.Evidence} {
			*p = strings.TrimSpace(*p)
			if len(*p) > maxFieldLength {
				return invalid("fields must be at most %d characters", maxFieldLength)
			}
		}
		if a.Description == "" && a.Owner == "" {
			continue
		}
		if a.Description == "" {
			return invalid("action %d needs a description", i+1)
		}
		if a.Owner == "" {
			return invalid("action %q needs a named owner", a.Description)
		}
		if a.Status == "" {
			a.Status = ActionNotStarted
		}
		if a.ActionType == "" {
			a.ActionType = ActionTypes[0]
		}
		if !slices.Contains(ActionStatuses, a.Status) {
			return invalid("action status must be one of: %s", strings.Join(ActionStatuses, ", "))
		}
		if !slices.Contains(ActionTypes, a.ActionType) {
			return invalid("action type must be one of: %s", strings.Join(ActionTypes, ", "))
		}
		if err := checkDate("action due_date", a.DueDate); err != nil {
			return err
		}
		if err := checkDate("action completed_date", a.CompletedDate); err != nil {
			return err
		}
		actions = append(actions, a)
	}
	f.Actions = actions
	return nil
}

func checkDate(name, value string) error {
	if value == "" {
		return nil
	}
	if _, err := time.Parse(dateLayout, value); err != nil {
		return invalid("%s must be a date in YYYY-MM-DD form", name)
	}
	return nil
}

func addDays(date string, days int) string {
	t, err := time.Parse(dateLayout, date)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, days).Format(dateLayout)
}

func daysBetween(from, to string) int {
	a, errA := time.Parse(dateLayout, from)
	b, errB := time.Parse(dateLayout, to)
	if errA != nil || errB != nil {
		return 0
	}
	return int(b.Sub(a).Hours() / 24)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

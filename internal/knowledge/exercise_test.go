package knowledge

import (
	"path/filepath"
	"strings"
	"testing"

	"grc/internal/db"
)

// An agent helping someone build an exercise is asked about its injects,
// decisions and clocks rather than how many there are, and about the exercise
// on screen rather than the one from a minute ago.
func TestExerciseRecordIsCompleteAndCurrent(t *testing.T) {
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	for _, stmt := range []string{
		`INSERT INTO crisis_ex_exercises (id, reference, title, summary, status)
			VALUES (1, 'CX-2026-001', 'Core banking ransomware', 'Board-level tabletop.', 'draft')`,
		`INSERT INTO crisis_ex_phases (id, exercise_id, ordinal, phase_key, name, purpose, lead_role, offset_minutes, duration_minutes)
			VALUES (10, 1, 1, 'detection', 'Detection', 'Confirm the incident.', 'incident_lead', 0, 60)`,
		`INSERT INTO crisis_ex_injects (id, exercise_id, phase_id, ordinal, code, offset_minutes, title, body,
			channel, from_actor, to_actor, inject_type, expected_actions)
			VALUES (100, 1, 10, 1, 'INJ-001', 5, 'EDR alert on the ledger host', 'Ransom note found on LEDGER-01.',
			'siem_alert', 'SOC', 'CISO', 'event', 'Isolate the host.')`,
		`INSERT INTO crisis_ex_responses (exercise_id, inject_id, outcome, responded_offset, actual_actions)
			VALUES (1, 100, 'partial', 20, 'Isolated after 15 minutes.')`,
		`INSERT INTO crisis_ex_decisions (exercise_id, offset_minutes, title, decision, made_by, role, reversible)
			VALUES (1, 90, 'Take payments offline', 'Suspend outgoing payments', 'J. Smith', 'ceo', 0)`,
		`INSERT INTO crisis_ex_classification (exercise_id, aware_offset, classified_offset, major, rationale)
			VALUES (1, 10, 240, 1, 'Critical services down beyond the threshold.')`,
		`INSERT INTO crisis_ex_clocks (exercise_id, ordinal, regime, authority, label, due_offset, actual_offset, status)
			VALUES (1, 1, 'DORA', 'Finantsinspektsioon', 'Initial notification', 480, 420, 'met')`,
		`INSERT INTO crisis_ex_findings (exercise_id, ordinal, code, title, severity, status)
			VALUES (1, 1, 'F-01', 'Escalation took too long', 'high', 'open')`,
		`INSERT INTO crisis_ex_participants (exercise_id, name, role_key, org, player, contact)
			VALUES (1, 'Maria', 'ciso', 'Example Bank', 1, 'maria@example.com')`,
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	svc := NewService(NewStore(conn))

	item, err := svc.Get(KindExercise, "CX-2026-001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	for _, want := range []string{
		"Phase T+0m · Detection (pending, 60 min, led by incident lead): Confirm the incident.",
		"Inject INJ-001 at T+5m, event by siem alert, SOC → CISO: EDR alert on the ledger host.",
		"Message: Ransom note found on LEDGER-01.",
		"Expected: Isolate the host.",
		"Played: partial at T+20m. Observed: Isolated after 15 minutes.",
		"Decision at T+1h 30m: Take payments offline — Suspend outgoing payments.",
		"Made by J. Smith (ceo). Irreversible.",
		"classified at T+4h, a major incident.",
		"Clock: Initial notification (DORA) to Finantsinspektsioon, due T+8h, met at T+7h.",
		"Finding F-01 [high]: Escalation took too long (open).",
		"Participants: Maria, ciso, Example Bank.",
	} {
		if !strings.Contains(item.Body, want) {
			t.Errorf("exercise record is missing %q\n--- body ---\n%s", want, item.Body)
		}
	}
	if strings.Contains(item.Body, "maria@example.com") {
		t.Error("the exercise record carries a participant's contact details")
	}

	if _, err := conn.Exec(`UPDATE crisis_ex_injects SET title = 'Ransom note on the ledger host' WHERE id = 100`); err != nil {
		t.Fatalf("edit inject: %v", err)
	}
	item, err = svc.Get(KindExercise, "CX-2026-001")
	if err != nil {
		t.Fatalf("Get after edit: %v", err)
	}
	if !strings.Contains(item.Body, "Ransom note on the ledger host") {
		t.Error("an inject edited a moment ago is missing from the record an agent reads")
	}
}

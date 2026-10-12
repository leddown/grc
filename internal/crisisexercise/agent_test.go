package crisisexercise

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func groundedAgent(send bool) AgentConfig {
	return AgentConfig{
		Agent:        func() string { return "crisis-exercises" },
		SendExercise: func() bool { return send },
		Grounded:     func() bool { return true },
	}
}

func exerciseWithAnInject(t *testing.T, svc *Service) (Exercise, Inject) {
	t.Helper()
	ex, err := svc.Create(CreateInput{
		Title:        "Core banking ransomware",
		ScenarioKey:  "ransomware-core-banking",
		Jurisdiction: "EE",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	d, err := svc.Dossier(ex.ID, 0)
	if err != nil {
		t.Fatalf("Dossier: %v", err)
	}
	if len(d.Phases) == 0 {
		t.Fatal("the scenario seeded no phases")
	}
	in, err := svc.SaveInject(ex.ID, Inject{
		PhaseID:       d.Phases[0].ID,
		OffsetMinutes: d.Phases[0].OffsetMinutes,
		Title:         "EDR alert on the ledger host",
		Body:          "Ransom note found on LEDGER-01.",
	})
	if err != nil {
		t.Fatalf("SaveInject: %v", err)
	}
	return ex, in
}

func askAgent(t *testing.T, svc *Service, exerciseID int64, question string) {
	t.Helper()
	if _, err := svc.AskAgent(context.Background(), exerciseID, question, "facilitator"); err != nil {
		t.Fatalf("AskAgent(%q): %v", question, err)
	}
}

// Every question the module asks goes to the Crisis Exercise agent, not only the
// conversation with it.
func TestEveryQuestionGoesToTheCrisisAgent(t *testing.T) {
	asker := &stubAsker{reply: "noted"}
	svc, _ := newTestService(t, nil, asker)
	svc.WithAgent(groundedAgent(false))
	ex, _ := svc.Create(CreateInput{Title: "Board simulation", Jurisdiction: "EE"})

	if _, err := svc.Advise(context.Background(), ex.ID, PersonaBoardTrainer, "", "What will the chair ask?", "facilitator"); err != nil {
		t.Fatalf("Advise: %v", err)
	}
	askAgent(t, svc, ex.ID, "What is missing?")

	if len(asker.agents) != 2 {
		t.Fatalf("asked %d questions, want 2", len(asker.agents))
	}
	for i, agent := range asker.agents {
		if agent != "crisis-exercises" {
			t.Errorf("question %d went to agent %q, want the Crisis Exercise agent", i, agent)
		}
	}
}

// By default the agent is told once which exercise is open and where its record
// is, and fetches the record itself; the record is not pasted in.
func TestAskAgentPointsAGroundedAgentAtTheRecord(t *testing.T) {
	asker := &stubAsker{reply: "looking it up", session: "sess-1"}
	svc, _ := newTestService(t, nil, asker)
	svc.WithAgent(groundedAgent(false))
	ex, _ := exerciseWithAnInject(t, svc)

	askAgent(t, svc, ex.ID, "Which phase has no decision inject?")
	askAgent(t, svc, ex.ID, "And the board phase?")

	first := asker.prompts[0]
	if !strings.Contains(first, `ref "`+ex.Reference+`"`) {
		t.Errorf("the first question does not say where the record is:\n%s", first)
	}
	if strings.Contains(first, "Ransom note found on LEDGER-01") {
		t.Error("the record was pasted in although the agent fetches it")
	}
	if asker.prompts[1] != "And the board phase?" || asker.sessions[1] != "sess-1" {
		t.Errorf("follow-up = %q on session %q, want the bare question in the same conversation",
			asker.prompts[1], asker.sessions[1])
	}
	if !strings.Contains(asker.systems[0], "Say when you do not know") {
		t.Error("the standing constraints were not appended to the agent's prompt")
	}

	turns, _ := svc.ListChat(ex.ID)
	if len(turns) != 4 {
		t.Fatalf("turns = %d, want a question and an answer for each", len(turns))
	}
	for _, turn := range turns {
		if turn.Persona != PersonaAgent {
			t.Errorf("turn recorded under %q, want %q", turn.Persona, PersonaAgent)
		}
	}
}

// With the exercise sent — for an agent that cannot reach this server — a
// conversation gets the whole record, and gets it again only once it changes.
func TestAskAgentSendsTheExerciseAgainOnlyWhenItChanges(t *testing.T) {
	asker := &stubAsker{reply: "ok", session: "sess-1"}
	svc, _ := newTestService(t, nil, asker)
	svc.WithAgent(groundedAgent(true))
	ex, inject := exerciseWithAnInject(t, svc)

	askAgent(t, svc, ex.ID, "first")
	askAgent(t, svc, ex.ID, "second")
	inject.Title = "Ransom note on the ledger host"
	if _, err := svc.SaveInject(ex.ID, inject); err != nil {
		t.Fatalf("SaveInject: %v", err)
	}
	askAgent(t, svc, ex.ID, "third")

	if !strings.Contains(asker.prompts[0], "Ransom note found on LEDGER-01") {
		t.Errorf("the first question did not carry the record:\n%s", asker.prompts[0])
	}
	if asker.prompts[1] != "second" {
		t.Errorf("an unchanged exercise was sent again:\n%s", asker.prompts[1])
	}
	third := asker.prompts[2]
	if !strings.Contains(third, "Ransom note on the ledger host") || !strings.Contains(third, "replaces any earlier copy") {
		t.Errorf("the edited exercise was not sent again as a replacement:\n%s", third)
	}
}

// Claude has nothing to fetch a record with, so it is sent the exercise whatever
// Settings says — and, holding no conversation of its own, with every question.
func TestAskAgentSendsTheExerciseToAProviderThatCannotFetchIt(t *testing.T) {
	asker := &stubAsker{reply: "ok"}
	svc, _ := newTestService(t, nil, asker)
	svc.WithAgent(AgentConfig{Grounded: func() bool { return false }})
	ex, _ := exerciseWithAnInject(t, svc)

	askAgent(t, svc, ex.ID, "first")
	askAgent(t, svc, ex.ID, "second")

	for i, prompt := range asker.prompts {
		if !strings.Contains(prompt, "Ransom note found on LEDGER-01") {
			t.Errorf("question %d did not carry the record:\n%s", i, prompt)
		}
	}
	if info := svc.Agent(); !info.SendsExercise || info.Grounded {
		t.Errorf("Agent() = %+v, want an ungrounded provider that is sent the exercise", info)
	}
}

// The advisers and the agent are cleared from different tabs, and clearing one
// leaves the other.
func TestClearingOneConversationKeepsTheOther(t *testing.T) {
	svc, _ := newTestService(t, nil, &stubAsker{reply: "ok"})
	ex, _ := svc.Create(CreateInput{Title: "Tabletop"})

	if _, err := svc.Advise(context.Background(), ex.ID, PersonaCISO, "", "What does containment cost?", ""); err != nil {
		t.Fatalf("Advise: %v", err)
	}
	askAgent(t, svc, ex.ID, "What is missing?")

	if err := svc.ClearChat(ex.ID, false); err != nil {
		t.Fatalf("ClearChat advisers: %v", err)
	}
	turns, _ := svc.ListChat(ex.ID)
	if len(turns) != 2 || turns[0].Persona != PersonaAgent || turns[1].Persona != PersonaAgent {
		t.Fatalf("after clearing the advisers: %+v, want only the agent's conversation", turns)
	}

	if err := svc.ClearChat(ex.ID, true); err != nil {
		t.Fatalf("ClearChat agent: %v", err)
	}
	if turns, _ := svc.ListChat(ex.ID); len(turns) != 0 {
		t.Errorf("after clearing the agent: %d turns left, want none", len(turns))
	}
}

// The dock asks about the exercise on screen on an exercise page, is told where
// the exercises are elsewhere in the module, and leaves every other page alone.
func TestDockQuestionFollowsThePage(t *testing.T) {
	svc, _ := newTestService(t, nil, &stubAsker{reply: "ok"})
	svc.WithAgent(groundedAgent(false))
	ex, _ := exerciseWithAnInject(t, svc)
	page := fmt.Sprintf("/crisis-exercises/%d", ex.ID)

	for _, path := range []string{"/controls", "/crisis-exercises-archive", ""} {
		if _, ok, err := svc.DockQuestion(path, "", "q"); ok || err != nil {
			t.Errorf("DockQuestion(%q) = ok %v, err %v; want it left alone", path, ok, err)
		}
	}

	turn, ok, err := svc.DockQuestion(page, "", "what next?")
	if err != nil || !ok {
		t.Fatalf("DockQuestion(exercise page) = ok %v, err %v", ok, err)
	}
	if turn.Agent != "crisis-exercises" {
		t.Errorf("dock agent = %q, want the Crisis Exercise agent", turn.Agent)
	}
	if !strings.Contains(turn.Prompt, ex.Reference) || !strings.HasSuffix(turn.Prompt, "what next?") {
		t.Errorf("dock prompt does not name the exercise before the question:\n%s", turn.Prompt)
	}
	turn.Answered("sess-9")

	list, ok, err := svc.DockQuestion("/crisis-exercises/", "", "which exercises are drafts?")
	if err != nil || !ok {
		t.Fatalf("DockQuestion(list) = ok %v, err %v", ok, err)
	}
	if !strings.Contains(list.Prompt, `kind "exercise"`) {
		t.Errorf("the list page does not say where the exercises are:\n%s", list.Prompt)
	}

	followUp, _, err := svc.DockQuestion(page+"#run", "sess-9", "and then?")
	if err != nil {
		t.Fatalf("DockQuestion(follow-up): %v", err)
	}
	if followUp.Prompt != "and then?" {
		t.Errorf("follow-up prompt = %q, want the bare question", followUp.Prompt)
	}
}

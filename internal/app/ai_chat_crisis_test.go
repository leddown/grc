package app

import (
	"strings"
	"testing"

	"grc/internal/crisisexercise"
)

type fakeCrisisDock struct {
	asked   bool
	session string
}

func (f *fakeCrisisDock) DockQuestion(path, sessionID, question string) (crisisexercise.DockTurn, bool, error) {
	f.asked, f.session = true, sessionID
	if !strings.HasPrefix(path, "/crisis-exercises") {
		return crisisexercise.DockTurn{}, false, nil
	}
	return crisisexercise.DockTurn{
		Agent:    "crisis-exercises",
		Prompt:   "the exercise on screen\n\n" + question,
		Answered: func(string) {},
	}, true, nil
}

// The dock on a Crisis Exercises page asks that module's agent about the
// exercise on screen; everywhere else, and from the AI Chat page, which names
// its own provider and agent, a question goes as it was asked.
func TestDockQuestionsOnCrisisPagesGoToTheCrisisAgent(t *testing.T) {
	original := activeCrisisDock
	t.Cleanup(func() { activeCrisisDock = original })
	dock := &fakeCrisisDock{}
	configureCrisisDock(dock)

	turn, err := crisisDockTurn(aiChatRequest{Question: "what next?", Page: "/crisis-exercises/3", SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("crisisDockTurn: %v", err)
	}
	if turn.Agent != "crisis-exercises" || !strings.HasPrefix(turn.Prompt, "the exercise on screen") {
		t.Errorf("crisis page turn = %+v, want the crisis agent and the exercise", turn)
	}
	if dock.session != "sess-1" {
		t.Errorf("session passed on = %q, want the dock's session", dock.session)
	}

	other, err := crisisDockTurn(aiChatRequest{Question: "what next?", Page: "/controls"})
	if err != nil {
		t.Fatalf("crisisDockTurn: %v", err)
	}
	if other.Agent != "" || other.Prompt != "what next?" {
		t.Errorf("another page's turn = %+v, want the question unchanged", other)
	}

	dock.asked = false
	chat, err := crisisDockTurn(aiChatRequest{Provider: "wintermute", Question: "what next?", Page: "/crisis-exercises/3"})
	if err != nil {
		t.Fatalf("crisisDockTurn: %v", err)
	}
	if dock.asked || chat.Agent != "" || chat.Prompt != "what next?" {
		t.Errorf("AI Chat page turn = %+v (module consulted: %v), want it untouched", chat, dock.asked)
	}
	chat.Answered("sess-2")
}

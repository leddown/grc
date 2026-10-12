package aiprovider

import (
	"context"
	"testing"
)

// A provider that cannot stream — the Wintermute server answers a turn whole —
// hands the whole answer over once.
func TestAskStreamFallsBackToTheWholeAnswer(t *testing.T) {
	var whole []string
	resp, err := AskStream(context.Background(), wholeProvider{}, Request{}, func(s string) { whole = append(whole, s) })
	if err != nil || len(whole) != 1 || whole[0] != resp.Text {
		t.Fatalf("fallback: %q %v", whole, err)
	}
}

type wholeProvider struct{}

func (wholeProvider) Name() string     { return "whole" }
func (wholeProvider) Available() bool  { return true }
func (wholeProvider) Describe() string { return "whole" }
func (wholeProvider) Ask(context.Context, Request) (Response, error) {
	return Response{Text: "the whole answer"}, nil
}

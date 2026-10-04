//go:build live

// The one live call the owner approved (Q5): a real request through a
// Wintermute server and the whole engine -- the turn, parsing, repair and
// validation -- on synthetic policy text only. It is behind the "live" build
// tag and needs a server and a client token, so go test ./... never makes it:
//
//	WINTERMUTE_URL=... WINTERMUTE_TOKEN=... go test -tags live -run Live -v ./internal/policyai/
//
// WINTERMUTE_AGENT names the agent, GRC_LIVE_BACKEND and GRC_LIVE_MODEL the
// backend and model (default: the server's).
package policyai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grc/internal/aiprovider"
	"grc/internal/db"
	"grc/internal/knowledge"
	"grc/internal/policydocs"
	"grc/internal/policystudio"
)

func TestLiveProposal(t *testing.T) {
	url, token := strings.TrimSpace(os.Getenv("WINTERMUTE_URL")), strings.TrimSpace(os.Getenv("WINTERMUTE_TOKEN"))
	if url == "" || token == "" {
		t.Skip("WINTERMUTE_URL and WINTERMUTE_TOKEN are not set")
	}
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	policies := policydocs.NewService(policydocs.NewSQLiteRepository(conn))
	studio := policystudio.NewService(conn, policies, policystudio.Options{})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = studio.Shutdown(ctx)
	}()
	doc, err := policies.CreateDocument(policydocs.Document{Title: "Synthetic access policy", DocType: policydocs.TypePolicy, OwnerRole: "CISO"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policies.CreateSection(policydocs.Section{DocumentID: doc.ID, SectionKind: policydocs.KindStatements, Heading: "Policy statements",
		Body: "Staff will review access where possible. Managers are encouraged to remove leavers' accounts."}); err != nil {
		t.Fatal(err)
	}
	if _, err := studio.Migrate(doc.ID, "live"); err != nil {
		t.Fatal(err)
	}

	wm := aiprovider.NewWintermute(func() aiprovider.WintermuteConfig {
		return aiprovider.WintermuteConfig{URL: url, Token: token, Agent: os.Getenv("WINTERMUTE_AGENT"),
			Backend: os.Getenv("GRC_LIVE_BACKEND"), Model: os.Getenv("GRC_LIVE_MODEL")}
	})
	calls := 0
	router := aiprovider.NewRouter(wm, func(p, m string, in, out int) {
		calls++
		t.Logf("usage: %s %s in=%d out=%d", p, m, in, out)
	})
	engine := NewEngine(Config{Conn: conn, Studio: studio, Policies: policies, Knowledge: knowledge.NewService(knowledge.NewStore(conn)), Router: router})

	res, err := engine.Propose(context.Background(), doc.ID, Request{Action: "testable", Scope: ScopeDocument}, "live")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("destination: %s; answer: %s", res.Destination, res.Answer)
	for _, e := range res.Edits {
		t.Logf("%s %s: %q -> %q (%s) %v", e.Status, e.Op, e.Quote, e.Replacement, e.Reason, e.Warnings)
	}
	if !res.HasProposal || len(res.Edits) == 0 {
		t.Fatal("the live answer proposed no edits")
	}
	if calls < 1 || calls > 2 {
		t.Fatalf("%d model calls", calls)
	}
}

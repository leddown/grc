package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// settingsPageHTML renders the page once, so these assertions read the same
// markup a browser gets rather than a constant.
func settingsPageHTML(t *testing.T) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/settings", settingsPage)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	return rec.Body.String()
}

// A stored setting the page cannot currently list must still be selected, or
// pressing Save writes an empty value over it.
//
// This is how the Wintermute agent was lost. The agent list is fetched from
// that server; when it was unreachable — which it is on every restart until it
// comes up, and always if it lives on a laptop — the select fell back to "No
// agent", and the next Save stored exactly that. Nothing said so: every
// question still got an answer, from the model's training data rather than from
// this installation's catalogs. The backend and model selects carry the same
// guard for the same reason.
func TestSettingsPageKeepsStoredSelectionsItCannotList(t *testing.T) {
	html := settingsPageHTML(t)

	for _, guard := range []string{
		"ensureOption(wmAgent, agent, agent)",
		"ensureOption(wmBackend, backend, backend)",
		"ensureOption(wmModel, model, model)",
	} {
		if !strings.Contains(html, guard) {
			t.Errorf("the page does not preserve a stored value it cannot list: missing %q", guard)
		}
	}

	// And when a list does arrive without it, the value is labelled rather than
	// dropped — it is still what the next question would run against.
	for _, label := range []string{
		"not on this server",
		"not listed on this server",
	} {
		if !strings.Contains(html, label) {
			t.Errorf("a stored value missing from the server's list must be labelled: missing %q", label)
		}
	}
}

// The page says which database it is writing to, because this application is
// run against two of them and "the settings did not save" is the wrong
// conclusion to draw from looking at the other one.
func TestSettingsPageNamesItsStorage(t *testing.T) {
	html := settingsPageHTML(t)
	if !strings.Contains(html, "Settings are stored in ") {
		t.Error("the page does not name where settings are stored")
	}
}

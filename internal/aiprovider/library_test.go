package aiprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// stubLibrary speaks the shape wintermuted's document endpoint actually emits:
// a page of chunks, the same page reassembled as text, and next_from present
// only while there is more. The field names are copied from that server's
// knowledge.Document and knowledge.Chunk, because a mismatch here decodes to
// zero values rather than to an error.
type stubLibrary struct {
	token   string
	agent   string
	chunks  []map[string]any
	pages   int
	lastReq []string
	// nextFrom overrides the paging cursor, for the server-misbehaving case.
	nextFrom func(from, returned int) *int
}

func (s *stubLibrary) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+s.token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			s.lastReq = append(s.lastReq, r.URL.String())
			h(w, r)
		}
	}

	mux.HandleFunc("/api/v1/agents/"+s.agent+"/documents", authed(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"documents": []any{
			map[string]any{
				"id": 7, "title": "DORA", "filename": "dora.pdf", "media_type": "application/pdf",
				"byte_size": 4096, "text_chars": 900, "extract_via": "ocrmypdf",
				"chunk_count": len(s.chunks),
			},
		}})
	}))

	mux.HandleFunc("/api/v1/agents/"+s.agent+"/documents/7/text", authed(func(w http.ResponseWriter, r *http.Request) {
		s.pages++
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		count, _ := strconv.Atoi(r.URL.Query().Get("count"))
		if count <= 0 || count > len(s.chunks) {
			count = len(s.chunks)
		}

		end := from + count
		if end > len(s.chunks) {
			end = len(s.chunks)
		}
		page := s.chunks[from:end]

		var text strings.Builder
		for _, c := range page {
			if h, _ := c["heading"].(string); h != "" {
				text.WriteString(h + "\n\n")
			}
			text.WriteString(c["body"].(string) + "\n\n")
		}

		body := map[string]any{
			"document": map[string]any{
				"id": 7, "title": "DORA", "filename": "dora.pdf", "media_type": "application/pdf",
				"byte_size": 4096, "text_chars": 900, "extract_via": "ocrmypdf",
				"chunk_count": len(s.chunks),
			},
			"from":   from,
			"chunks": page,
			"text":   strings.TrimSpace(text.String()),
		}
		if s.nextFrom != nil {
			if next := s.nextFrom(from, len(page)); next != nil {
				body["next_from"] = *next
			}
		} else if end < len(s.chunks) {
			body["next_from"] = end
		}
		writeJSON(w, body)
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func chunkPage(n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, map[string]any{
			"ordinal": i,
			"heading": fmt.Sprintf("Article %d", i+1),
			"body":    fmt.Sprintf("An entity shall satisfy requirement %d.", i+1),
		})
	}
	return out
}

func libraryFor(t *testing.T, stub *stubLibrary) *Wintermute {
	t.Helper()
	srv := stub.server(t)
	return NewWintermute(func() WintermuteConfig {
		return WintermuteConfig{URL: srv.URL, Token: stub.token, Agent: stub.agent}
	})
}

// The document has to arrive whole, in order, with its headings — a segmenter
// looking for "Article 17" as a boundary finds nothing without them, and a
// document that arrived in the wrong order gives every article the wrong one.
func TestReadLibraryDocumentFollowsPagingToTheEnd(t *testing.T) {
	stub := &stubLibrary{token: "tok", agent: "acme", chunks: chunkPage(250)}
	w := libraryFor(t, stub)

	content, err := w.ReadLibraryDocument(context.Background(), 7)
	if err != nil {
		t.Fatalf("ReadLibraryDocument: %v", err)
	}

	if len(content.Chunks) != 250 {
		t.Fatalf("got %d chunks of 250 — the read stopped early", len(content.Chunks))
	}
	if stub.pages < 2 {
		t.Fatalf("250 chunks came back in %d page(s); the paging was never exercised", stub.pages)
	}
	if content.Document.Title != "DORA" || content.Document.ExtractVia != "ocrmypdf" {
		t.Errorf("document metadata did not decode: %+v", content.Document)
	}
	if content.Document.ChunkCount != 250 {
		t.Errorf("ChunkCount = %d, want 250", content.Document.ChunkCount)
	}

	for i, want := range []string{"Article 1", "Article 150", "Article 250", "requirement 250."} {
		if !strings.Contains(content.Text, want) {
			t.Errorf("case %d: reassembled text is missing %q", i, want)
		}
	}
	if strings.Index(content.Text, "Article 150") < strings.Index(content.Text, "Article 2\n") {
		t.Error("pages were concatenated out of order")
	}
	if content.Chunks[0].Ordinal != 0 || content.Chunks[249].Ordinal != 249 {
		t.Errorf("chunk ordinals are wrong at the edges: %d … %d",
			content.Chunks[0].Ordinal, content.Chunks[249].Ordinal)
	}
}

// A server that answers every page with the same cursor would otherwise be read
// five hundred times, with the text duplicated in the result and no error.
func TestReadLibraryDocumentRefusesACursorThatDoesNotAdvance(t *testing.T) {
	stuck := 0
	stub := &stubLibrary{
		token: "tok", agent: "acme", chunks: chunkPage(250),
		nextFrom: func(from, returned int) *int { return &stuck },
	}
	w := libraryFor(t, stub)

	_, err := w.ReadLibraryDocument(context.Background(), 7)
	if err == nil {
		t.Fatal("a cursor that never advances was followed without complaint")
	}
	if !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("unhelpful error: %v", err)
	}
	if stub.pages > 3 {
		t.Errorf("the stuck cursor was followed %d times before being refused", stub.pages)
	}
}

func TestLibraryDocumentsDecodesTheListing(t *testing.T) {
	stub := &stubLibrary{token: "tok", agent: "acme", chunks: chunkPage(3)}
	w := libraryFor(t, stub)

	docs, err := w.LibraryDocuments(context.Background())
	if err != nil {
		t.Fatalf("LibraryDocuments: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != 7 || docs[0].Filename != "dora.pdf" {
		t.Fatalf("listing did not decode: %+v", docs)
	}
	if !docs[0].Ready() {
		t.Error("a document with passages and no processing state is not Ready()")
	}
}

// Ready() gates the import, so the states it separates are worth pinning: a
// document mid-processing has only some of its passages.
func TestLibraryDocumentReady(t *testing.T) {
	tests := []struct {
		name string
		doc  LibraryDocument
		want bool
	}{
		{"extracted", LibraryDocument{ChunkCount: 4}, true},
		{"no passages", LibraryDocument{ChunkCount: 0}, false},
		{"still being read", LibraryDocument{
			ChunkCount: 4, Processing: &LibraryProcessing{Attempts: 1}}, false},
		{"processing gave up", LibraryDocument{
			ChunkCount: 4, Processing: &LibraryProcessing{Failed: true, LastError: "no OCR"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.doc.Ready(); got != tt.want {
				t.Errorf("Ready() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Without an agent there is no library — every document lives in one agent's —
// and the message has to say that rather than reporting a missing server.
func TestLibraryNeedsAnAgent(t *testing.T) {
	w := NewWintermute(func() WintermuteConfig {
		return WintermuteConfig{URL: "https://wintermute.example", Token: "tok"}
	})
	if _, err := w.LibraryDocuments(context.Background()); !errors.Is(err, ErrNoAgent) {
		t.Fatalf("LibraryDocuments with no agent = %v, want ErrNoAgent", err)
	}
	if w.LibraryURL() != "" {
		t.Errorf("LibraryURL() = %q with no agent", w.LibraryURL())
	}
}

// A server that predates the endpoint answers 404, and "not found" on its own
// sends someone hunting for a document that is sitting right there.
func TestLibraryNamesAnOlderServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	w := NewWintermute(func() WintermuteConfig {
		return WintermuteConfig{URL: srv.URL, Token: "tok", Agent: "acme"}
	})
	_, err := w.ReadLibraryDocument(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "predates its document API") {
		t.Fatalf("error = %v, want it to name an older server", err)
	}
}

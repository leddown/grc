package aiprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"strconv"
	"time"
)

// The document library an agent on a wintermuted server holds.
//
// This application used to read PDFs and Word documents itself, which meant a
// second extractor next to the one that server already had — without its OCR,
// without its LibreOffice conversion, and with a second copy of every file. It
// now uploads there and reads the text back, so a document is extracted once,
// by whatever tools that server has, and there is one library rather than two.
//
// Everything here is read-only. Uploading, re-processing and deleting are done
// on that server's own pages, which the Settings and module pages link to: a
// library is what an agent treats as established fact, and deciding what goes
// into one stays with a person, in the place that owns it.

// libraryTimeout bounds one read. Generous next to the catalog lookups because
// a page of a scanned document is a great deal more text than a list of model
// names, and stingy next to a turn because nothing here waits on a model.
const libraryTimeout = 60 * time.Second

// libraryPageChunks is how many passages one request asks for. The server caps
// this at 500; asking for the cap would make one response several megabytes,
// and the bounded reader on this side would reject it.
const libraryPageChunks = 100

// maxLibraryPages bounds a read of one document.
//
// At the page size above this is fifty thousand passages, far past any
// regulation. It exists so that a server answering with a next_from that never
// advances cannot spin here forever — the loop below trusts a field it did not
// compute.
const maxLibraryPages = 500

// maxLibraryChars bounds how much text one document may contribute. A
// consolidated EU act runs to a few hundred thousand characters; this is an
// order of magnitude past that, and refusing beyond it is deliberate. The
// alternative — reading the first N characters and analysing those — produces a
// coverage report that says nothing about the rest of the instrument while
// looking exactly like one that does.
const maxLibraryChars = 12 << 20

// ErrNoAgent means no agent is configured, so there is no library to read.
// Every document lives in one agent's library; there is no server-wide corpus
// to fall back to.
var ErrNoAgent = errors.New("no Wintermute agent is selected, so there is no document library to read")

// LibraryDocument is one document in the agent's library, as that server
// describes it.
type LibraryDocument struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	SourceURL string `json:"source_url,omitempty"`
	SHA256    string `json:"sha256"`
	ByteSize  int64  `json:"byte_size"`
	TextChars int    `json:"text_chars"`
	// ExtractVia names what read the text — a PDF text layer, OCR, LibreOffice.
	// It travels with everything derived from the document, because a mapping
	// built on an OCR'd scan deserves to be read differently from one built on
	// a text layer.
	ExtractVia string    `json:"extract_via"`
	ChunkCount int       `json:"chunk_count"`
	UploadedAt time.Time `json:"uploaded_at"`
	// Processing is set while the server is still reading the document, or
	// after it gave up. A document imported mid-processing would be imported
	// half-read, so this is checked rather than displayed only.
	Processing *LibraryProcessing `json:"processing,omitempty"`
}

// Ready reports whether the document is finished being read.
func (d LibraryDocument) Ready() bool {
	return d.ChunkCount > 0 && (d.Processing == nil || d.Processing.Failed)
}

// LibraryProcessing is a document's position in that server's processing queue.
type LibraryProcessing struct {
	Attempts  int    `json:"attempts"`
	Failed    bool   `json:"failed"`
	LastError string `json:"last_error,omitempty"`
}

// LibraryChunk is one passage of a document, as that server chunked it.
type LibraryChunk struct {
	Ordinal int    `json:"ordinal"`
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

// LibraryContent is one document read in full: its metadata, its passages, and
// the text they reassemble into.
//
// Both shapes are carried because both callers exist — NFR enrichment cites
// passages, regulation coverage segments the text into articles — and the
// server derives one from the other, so they cannot disagree.
type LibraryContent struct {
	Document LibraryDocument
	Chunks   []LibraryChunk
	Text     string
}

// LibraryDocuments lists the documents in the configured agent's library.
func (w *Wintermute) LibraryDocuments(ctx context.Context) ([]LibraryDocument, error) {
	base, cfg, err := w.libraryTarget()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, libraryTimeout)
	defer cancel()

	var payload struct {
		Documents []LibraryDocument `json:"documents"`
	}
	if err := w.requestInto(ctx, http.MethodGet,
		base+"/api/v1/agents/"+neturl.PathEscape(cfg.Agent)+"/documents",
		cfg.Token, nil, &payload); err != nil {
		return nil, libraryError(err)
	}
	if payload.Documents == nil {
		payload.Documents = []LibraryDocument{}
	}
	return payload.Documents, nil
}

// ReadLibraryDocument reads one document's extracted text in full, following
// the server's paging until there is none left.
func (w *Wintermute) ReadLibraryDocument(ctx context.Context, documentID int64) (LibraryContent, error) {
	base, cfg, err := w.libraryTarget()
	if err != nil {
		return LibraryContent{}, err
	}
	if documentID <= 0 {
		return LibraryContent{}, fmt.Errorf("a document id is required")
	}

	ctx, cancel := context.WithTimeout(ctx, libraryTimeout*time.Duration(4))
	defer cancel()

	endpoint := base + "/api/v1/agents/" + neturl.PathEscape(cfg.Agent) +
		"/documents/" + strconv.FormatInt(documentID, 10) + "/text"

	var (
		content LibraryContent
		from    = 0
		chars   int
	)
	for page := 0; page < maxLibraryPages; page++ {
		var payload struct {
			Document LibraryDocument `json:"document"`
			Chunks   []LibraryChunk  `json:"chunks"`
			Text     string          `json:"text"`
			NextFrom *int            `json:"next_from"`
		}
		url := fmt.Sprintf("%s?from=%d&count=%d", endpoint, from, libraryPageChunks)
		if err := w.requestInto(ctx, http.MethodGet, url, cfg.Token, nil, &payload); err != nil {
			return LibraryContent{}, libraryError(err)
		}

		content.Document = payload.Document
		content.Chunks = append(content.Chunks, payload.Chunks...)
		if content.Text != "" && payload.Text != "" {
			content.Text += "\n\n"
		}
		content.Text += payload.Text

		chars += len(payload.Text)
		if chars > maxLibraryChars {
			return LibraryContent{}, fmt.Errorf(
				"%q is larger than this application will read in one piece (%d MiB); "+
					"split it and import the parts separately",
				payload.Document.Title, maxLibraryChars>>20)
		}

		// The server omits next_from on the last page, so the loop ends on the
		// field being absent rather than on arithmetic over a chunk count.
		if payload.NextFrom == nil {
			return content, nil
		}
		// A next_from that does not advance would otherwise re-read the same
		// page until the page cap, quietly, with the text repeated in the
		// result. Refusing says which document and which page.
		if *payload.NextFrom <= from {
			return LibraryContent{}, fmt.Errorf(
				"wintermute did not advance past passage %d of %q", from, payload.Document.Title)
		}
		from = *payload.NextFrom
	}
	return LibraryContent{}, fmt.Errorf(
		"%q has more passages than this application will read (%d pages of %d)",
		content.Document.Title, maxLibraryPages, libraryPageChunks)
}

// LibraryURL is the page on the Wintermute server where this agent's library is
// managed, for a link out. Empty when there is nothing to link to.
func (w *Wintermute) LibraryURL() string {
	base, cfg, err := w.libraryTarget()
	if err != nil {
		return ""
	}
	return base + "/agents/" + neturl.PathEscape(cfg.Agent)
}

// libraryTarget resolves the server and agent every read needs, or says which
// one is missing.
func (w *Wintermute) libraryTarget() (string, WintermuteConfig, error) {
	cfg := w.resolve()
	if cfg.URL == "" || cfg.Token == "" {
		return "", cfg, ErrNotConfigured
	}
	if cfg.Agent == "" {
		return "", cfg, ErrNoAgent
	}
	base, err := ValidateEndpoint(cfg.URL)
	if err != nil {
		return "", cfg, err
	}
	return base, cfg, nil
}

// libraryError names the two failures worth telling apart from a transport
// error: a server too old to have the endpoint, and an agent or document that
// is not there.
func libraryError(err error) error {
	var status *statusError
	if errors.As(err, &status) {
		switch status.code {
		case http.StatusNotFound:
			return fmt.Errorf("wintermute has no such agent or document, "+
				"or this server predates its document API: %w", err)
		}
	}
	return err
}

// Library is the read side of a library, as the modules that import documents
// need it. An interface so those modules can be tested without a server, and so
// they depend on the two reads they make rather than on the whole provider.
type Library interface {
	LibraryDocuments(ctx context.Context) ([]LibraryDocument, error)
	ReadLibraryDocument(ctx context.Context, documentID int64) (LibraryContent, error)
	LibraryURL() string
}

// Library returns the configured agent's document library.
//
// It is deliberately not routed the way a question is: there is no Claude
// equivalent to fall back to, because the library is a thing that exists on one
// server rather than a capability two providers both have. A module that
// imports documents is unavailable when Wintermute is, and says so.
func (r *Router) Library() (Library, error) {
	if r.wintermute == nil || !r.wintermute.Available() {
		return nil, fmt.Errorf("%w: set a Wintermute server and token in Settings — "+
			"documents are uploaded to an agent there, not here", ErrNotConfigured)
	}
	if _, _, err := r.wintermute.libraryTarget(); err != nil {
		return nil, err
	}
	return r.wintermute, nil
}

// LibraryAvailable reports whether there is a library to read, for a page that
// has to render either way.
func (r *Router) LibraryAvailable() bool {
	_, err := r.Library()
	return err == nil
}

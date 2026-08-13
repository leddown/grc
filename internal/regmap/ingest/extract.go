// Package ingest extracts text from an uploaded document and segments it into
// requirements using a framework profile's rules.
package ingest

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// Extraction is the result of pulling text out of a source document.
type Extraction struct {
	Text   string
	Format string
	Method string
	// Notes records anything the extractor wants the human to know at GATE 1.
	Notes []string
}

// Extract reads a PDF, DOCX or plain-text file and returns its text. It never
// drops content silently: anything it is unsure about is reported in Notes.
func Extract(path string) (*Extraction, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return extractPDF(path)
	case ".docx":
		return extractDOCX(path)
	case ".txt", ".text", ".md", "":
		// #nosec G304 -- reading an operator-named source document is this
		// command's entire purpose; the path comes from the --in flag.
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		return &Extraction{Text: Normalize(string(raw)), Format: "txt", Method: "direct read"}, nil
	default:
		return nil, fmt.Errorf("unsupported input format %q (supported: .pdf, .docx, .txt)", filepath.Ext(path))
	}
}

// extractPDF prefers the pdftotext binary because its layout mode preserves
// heading structure far better than a pure-Go extraction; it falls back to the
// ledongthuc/pdf library when pdftotext is not installed.
func extractPDF(path string) (*Extraction, error) {
	if bin, err := exec.LookPath("pdftotext"); err == nil {
		var out bytes.Buffer
		var stderr bytes.Buffer
		// An absolute path can never be mistaken for a pdftotext flag, which
		// closes the argument-injection hole a path like "-v" would open.
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", path, err)
		}
		// #nosec G204 -- fixed binary resolved via LookPath, fixed flags, and
		// an absolute input path; no shell is involved.
		cmd := exec.Command(bin, "-layout", "-enc", "UTF-8", abs, "-")
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			text := Normalize(out.String())
			if strings.TrimSpace(text) != "" {
				return &Extraction{Text: text, Format: "pdf", Method: "pdftotext -layout"}, nil
			}
		}
		// Fall through to the library extractor on any pdftotext failure.
	}

	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open pdf %s: %w", path, err)
	}
	defer f.Close()

	var buf bytes.Buffer
	notes := []string{}
	pages := r.NumPage()
	failed := 0
	for i := 1; i <= pages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			failed++
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			failed++
			continue
		}
		buf.WriteString(text)
		buf.WriteString("\n")
	}
	if failed > 0 {
		notes = append(notes, fmt.Sprintf("%d of %d PDF pages could not be extracted; review the segmentation carefully at GATE 1", failed, pages))
	}
	text := Normalize(buf.String())
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("extracted no text from %s: the PDF is probably scanned images and needs OCR first", path)
	}
	notes = append(notes, "extracted without pdftotext; install poppler-utils for better heading detection")
	return &Extraction{Text: text, Format: "pdf", Method: "ledongthuc/pdf", Notes: notes}, nil
}

// extractDOCX reads word/document.xml out of the OOXML package and converts
// paragraph and break elements into newlines.
func extractDOCX(path string) (*Extraction, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open docx %s: %w", path, err)
	}
	defer zr.Close()

	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return nil, fmt.Errorf("%s does not contain word/document.xml (is it really a .docx?)", path)
	}
	rc, err := doc.Open()
	if err != nil {
		return nil, fmt.Errorf("open word/document.xml: %w", err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read word/document.xml: %w", err)
	}

	var buf bytes.Buffer
	dec := xml.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse word/document.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			buf.Write(t)
		case xml.StartElement:
			switch t.Name.Local {
			case "br", "cr":
				buf.WriteString("\n")
			case "tab":
				buf.WriteString("\t")
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				buf.WriteString("\n")
			}
		}
	}
	text := Normalize(buf.String())
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("extracted no text from %s", path)
	}
	return &Extraction{Text: text, Format: "docx", Method: "archive/zip + word/document.xml"}, nil
}

var (
	crlf         = regexp.MustCompile(`\r\n?`)
	trailingWS   = regexp.MustCompile(`[ \t]+\n`)
	tooManyLines = regexp.MustCompile(`\n{4,}`)
)

// Normalize applies only conservative whitespace cleanup. It deliberately does
// not strip headers, footers or page numbers: dropping content silently is
// exactly what GATE 1 exists to prevent. Suspected noise is flagged instead,
// during segmentation.
func Normalize(s string) string {
	s = strings.ReplaceAll(s, "\ufeff", "")
	s = strings.ReplaceAll(s, "\f", "\n")
	s = crlf.ReplaceAllString(s, "\n")
	s = trailingWS.ReplaceAllString(s, "\n")
	s = tooManyLines.ReplaceAllString(s, "\n\n\n")
	return strings.TrimSpace(s) + "\n"
}

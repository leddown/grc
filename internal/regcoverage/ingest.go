package regcoverage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"grc/internal/regmap/ingest"
	"grc/internal/regmap/profile"
	"grc/internal/regmap/requirement"
	"grc/regmap/profiles"
)

// MaxUploadBytes bounds one regulation. Consolidated EU acts run to a few
// megabytes as PDFs; this leaves generous headroom while keeping a mistaken
// upload from being read into memory and stored before anyone notices.
const MaxUploadBytes = 25 << 20 // 25 MiB

// GenericProfileID is the fallback profile: article and annex segmentation with
// no curated knowledge of the instrument.
const GenericProfileID = "eu-generic"

// Profiles is the embedded framework profile registry, shared by every
// analysis. Loading it is cheap but not free, and it never changes at runtime.
var loadedProfiles *profile.Registry

// LoadProfiles returns the embedded registry, loading it once.
func LoadProfiles() (*profile.Registry, error) {
	if loadedProfiles != nil {
		return loadedProfiles, nil
	}
	reg, err := profile.LoadFS(profiles.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("load framework profiles: %w", err)
	}
	loadedProfiles = reg
	return reg, nil
}

// UploadInput is a regulation as it arrives from the browser.
type UploadInput struct {
	// Title is optional; the filename is used when it is empty.
	Title    string
	Filename string
	// Framework pins the profile. Empty means detect, falling back to generic.
	Framework  string
	Body       []byte
	UploadedBy string
}

// Ingested is the result of reading an upload, before it is stored.
type Ingested struct {
	Regulation Regulation
	Sections   []Section
	Text       string
	Source     Source
}

// Ingest extracts, identifies and segments an upload. It performs no I/O
// beyond the extraction itself, so it is testable without a database.
func Ingest(in UploadInput) (*Ingested, error) {
	if len(in.Body) == 0 {
		return nil, invalid("the uploaded file is empty")
	}
	if len(in.Body) > MaxUploadBytes {
		return nil, invalidf("the uploaded file is %s; the limit is %s",
			humanBytes(int64(len(in.Body))), humanBytes(MaxUploadBytes))
	}

	filename := strings.TrimSpace(in.Filename)
	if filename == "" {
		return nil, invalid("a filename is required, so the document type can be read")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf", ".docx", ".txt", ".text", ".md":
	default:
		return nil, invalidf("unsupported document type %q (supported: .pdf, .docx, .txt, .md)", ext)
	}

	extraction, err := ingest.ExtractBytes(filename, in.Body)
	if err != nil {
		return nil, invalid(err.Error())
	}

	registry, err := LoadProfiles()
	if err != nil {
		return nil, err
	}

	prof, detected, err := resolveProfile(registry, in.Framework, filename, extraction.Text)
	if err != nil {
		return nil, err
	}

	built, err := ingest.BuildRequirements(extraction.Text, prof)
	if err != nil {
		return nil, invalidf("segmenting the document failed: %v", err)
	}
	// Text before the first boundary is kept as a "preamble" segment so that
	// nothing is dropped silently. A document that produced *only* that has not
	// been segmented at all — it is one undifferentiated blob, which is not
	// something this module can analyse article by article.
	if len(built.Requirements) == 0 || allPreamble(built.Requirements) {
		return nil, invalid("no articles or sections could be found in this document — " +
			"check that the right framework profile is selected, and that the PDF is not a " +
			"scan needing OCR")
	}

	sections := make([]Section, 0, len(built.Requirements))
	for i, req := range built.Requirements {
		sections = append(sections, Section{
			Ref:        req.ID,
			Label:      sectionLabel(prof, req),
			Title:      req.Title,
			Category:   categoryOf(req),
			Body:       req.Text,
			Position:   i,
			Confidence: string(req.Confidence),
		})
	}

	sum := sha256.Sum256(in.Body)
	reg := Regulation{
		Title:         firstNonEmpty(in.Title, strings.TrimSuffix(filepath.Base(filename), ext)),
		Framework:     prof.ID,
		FrameworkName: prof.DisplayName,
		SourceRef:     prof.SourceRef,
		Detected:      detected,
		Filename:      filepath.Base(filename),
		MediaType:     mediaTypeFor(ext),
		SHA256:        hex.EncodeToString(sum[:]),
		ByteSize:      int64(len(in.Body)),
		ExtractMethod: extraction.Method,
		ExtractNotes:  extraction.Notes,
		TextChars:     len(extraction.Text),
		Status:        StatusIngested,
		SectionCount:  len(sections),
		UploadedBy:    in.UploadedBy,
	}

	return &Ingested{
		Regulation: reg,
		Sections:   sections,
		Text:       extraction.Text,
		Source: Source{
			MediaType: reg.MediaType,
			Filename:  reg.Filename,
			Content:   in.Body,
		},
	}, nil
}

// resolveProfile picks the framework profile. An explicit choice always wins;
// otherwise detection runs, and anything it cannot recognise falls back to the
// generic EU profile rather than refusing the upload — an instrument with no
// profile is exactly the case this module exists to handle.
func resolveProfile(registry *profile.Registry, pinned, filename, text string) (*profile.Profile, bool, error) {
	if pinned = strings.TrimSpace(pinned); pinned != "" {
		prof, err := registry.Get(pinned)
		if err != nil {
			return nil, false, invalidf("unknown framework %q", pinned)
		}
		return prof, prof.ID != GenericProfileID, nil
	}

	// Detection reads the filename and the opening of the text; a long document
	// contributes nothing extra past that and costs a full scan per profile.
	head := text
	if len(head) > 20000 {
		head = head[:20000]
	}
	for _, guess := range registry.Detect(filename, head) {
		if guess.Profile != nil && guess.Profile.ID != GenericProfileID {
			return guess.Profile, true, nil
		}
	}

	generic, err := registry.Get(GenericProfileID)
	if err != nil {
		return nil, false, fmt.Errorf("the %s fallback profile is missing: %w", GenericProfileID, err)
	}
	return generic, false, nil
}

// sectionLabel renders the human name of a section ("Article 17"). The
// requirement carries the profile's own label already; this only fills the gap
// when segmentation produced none.
func sectionLabel(prof *profile.Profile, req requirement.Requirement) string {
	if s := strings.TrimSpace(req.Section); s != "" {
		return s
	}
	if key := strings.TrimSpace(req.Key); key != "" {
		return prof.DisplayName + " " + key
	}
	return req.ID
}

// allPreamble reports whether segmentation found no boundaries at all.
func allPreamble(reqs requirement.Set) bool {
	for _, req := range reqs {
		if req.Strategy != "preamble" {
			return false
		}
	}
	return true
}

// categoryOf drops the segmenter's "unclassified" placeholder: an empty
// category reads as "not categorised", which is what it means, while the
// literal word in a report column reads as a category.
func categoryOf(req requirement.Requirement) string {
	if req.IsUnclassified() {
		return ""
	}
	return req.Category
}

func mediaTypeFor(ext string) string {
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".md":
		return "text/markdown; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

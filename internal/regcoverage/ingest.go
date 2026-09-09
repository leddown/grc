package regcoverage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"grc/internal/aiprovider"
	"grc/internal/regmap/ingest"
	"grc/internal/regmap/profile"
	"grc/internal/regmap/requirement"
	"grc/regmap/profiles"
)

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

// ImportInput names a regulation in the AI agent's library.
type ImportInput struct {
	// Title is optional; the library's own title is used when it is empty.
	Title string
	// Framework pins the profile. Empty means detect, falling back to generic.
	Framework string
	// Content is the document as the Wintermute server extracted it.
	Content    aiprovider.LibraryContent
	ImportedBy string
}

// Imported is the result of segmenting a regulation, before it is stored.
type Imported struct {
	Regulation Regulation
	Sections   []Section
	Text       string
}

// Import identifies and segments a regulation the agent's library has already
// extracted.
//
// The extraction itself is not done here and no longer can be: PDFs, scans and
// office documents are read on the Wintermute server, which has the OCR and the
// converters, and this application reads the text back. What remains is the
// part that is this module's own — deciding which instrument it is, and cutting
// it into the articles a coverage report is written against.
//
// It performs no I/O, so it is testable without a server or a database.
func Import(in ImportInput) (*Imported, error) {
	doc := in.Content.Document
	text := strings.TrimSpace(in.Content.Text)
	if text == "" {
		return nil, invalid("the agent's library holds no readable text for this document — " +
			"if it is a scan, check that it has finished being read on the Wintermute server")
	}

	filename := strings.TrimSpace(firstNonEmpty(doc.Filename, doc.Title))
	if filename == "" {
		return nil, invalid("the library document has neither a filename nor a title")
	}

	registry, err := LoadProfiles()
	if err != nil {
		return nil, err
	}

	prof, detected, err := resolveProfile(registry, in.Framework, filename, text)
	if err != nil {
		return nil, err
	}

	built, err := ingest.BuildRequirements(text, prof)
	if err != nil {
		return nil, invalidf("segmenting the document failed: %v", err)
	}
	// Text before the first boundary is kept as a "preamble" segment so that
	// nothing is dropped silently. A document that produced *only* that has not
	// been segmented at all — it is one undifferentiated blob, which is not
	// something this module can analyse article by article.
	if len(built.Requirements) == 0 || allPreamble(built.Requirements) {
		return nil, invalid("no articles or sections could be found in this document — " +
			"check that the right framework profile is selected, and that the document " +
			"was read properly on the Wintermute server rather than scanned without OCR")
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

	// Hashed over the extracted text rather than the file, which this
	// application no longer holds. It is also the better key: the same
	// instrument exported twice is two files and one document.
	sum := sha256.Sum256([]byte(text))
	reg := Regulation{
		Title:         firstNonEmpty(strings.TrimSpace(in.Title), doc.Title, strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))),
		Framework:     prof.ID,
		FrameworkName: prof.DisplayName,
		SourceRef:     prof.SourceRef,
		Detected:      detected,
		LibraryDocID:  doc.ID,
		Filename:      filepath.Base(filename),
		MediaType:     doc.MediaType,
		SHA256:        hex.EncodeToString(sum[:]),
		ByteSize:      doc.ByteSize,
		ExtractMethod: firstNonEmpty(doc.ExtractVia, "the Wintermute library"),
		TextChars:     len(text),
		Status:        StatusIngested,
		SectionCount:  len(sections),
		ImportedBy:    strings.TrimSpace(in.ImportedBy),
	}

	return &Imported{Regulation: reg, Sections: sections, Text: text}, nil
}

// resolveProfile picks the framework profile. An explicit choice always wins;
// otherwise detection runs, and anything it cannot recognise falls back to the
// generic EU profile rather than refusing the document — an instrument with no
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

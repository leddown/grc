package doctemplate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PayloadSource resolves a document id to a render payload. It is injected by
// internal/app, which is what connects this package to internal/policydocs
// without either importing the other — see the package comment.
//
// BaseName is the download filename stem, normally the document reference.
type PayloadSource func(id int64) (data []byte, baseName string, kind Kind, err error)

// Service holds the brand settings and the render pipeline.
type Service struct {
	repo    Repository
	payload PayloadSource
	now     func() time.Time
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// SetPayloadSource wires the document lookup. When it is nil — which is the
// state in a test that only exercises rendering — the render-a-document routes
// report that no document source is configured rather than panicking.
func (s *Service) SetPayloadSource(source PayloadSource) { s.payload = source }

// Brand returns the saved brand, or the shipped default when none is saved.
func (s *Service) Brand() (Brand, error) {
	if s.repo == nil {
		return DefaultBrand(), nil
	}
	brand, found, err := s.repo.LoadBrand()
	if err != nil {
		return Brand{}, err
	}
	if !found {
		return DefaultBrand(), nil
	}
	return brand, nil
}

// SaveBrand validates and stores brand settings.
func (s *Service) SaveBrand(brand Brand, actor string) (Brand, error) {
	if err := brand.Validate(); err != nil {
		return Brand{}, err
	}
	brand.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	brand.UpdatedBy = actor
	if s.repo == nil {
		return Brand{}, fmt.Errorf("brand settings are not persistable in this configuration")
	}
	if err := s.repo.SaveBrand(brand); err != nil {
		return Brand{}, err
	}
	return brand, nil
}

// ResetBrand restores the shipped default.
func (s *Service) ResetBrand() (Brand, error) {
	if s.repo == nil {
		return DefaultBrand(), nil
	}
	if err := s.repo.DeleteBrand(); err != nil {
		return Brand{}, err
	}
	return DefaultBrand(), nil
}

// RenderDocument renders a stored document through a template.
func (s *Service) RenderDocument(ctx context.Context, documentID int64, templateID string, pdfStandard string, bundle bool) (Result, error) {
	if s.payload == nil {
		return Result{}, fmt.Errorf("no document source is configured for rendering")
	}
	data, baseName, kind, err := s.payload(documentID)
	if err != nil {
		return Result{}, err
	}
	tpl, err := s.resolveTemplate(templateID, kind)
	if err != nil {
		return Result{}, err
	}
	return s.Render(ctx, Request{
		TemplateID:  tpl.ID,
		Data:        data,
		BaseName:    baseName,
		PDFStandard: pdfStandard,
		Bundle:      bundle,
	})
}

// RenderSample renders a template's bundled sample payload. This is how the
// brand editor previews a change: a real document would work too, but the
// sample is the one input guaranteed to exercise every part of the layout —
// every field populated, a multi-row control table, a revision history.
func (s *Service) RenderSample(ctx context.Context, templateID string, pdfStandard string, bundle bool) (Result, error) {
	tpl, ok := Lookup(templateID)
	if !ok {
		return Result{}, fmt.Errorf("unknown template %q", templateID)
	}
	data, err := SampleData(tpl)
	if err != nil {
		return Result{}, err
	}
	return s.Render(ctx, Request{
		TemplateID:  tpl.ID,
		Data:        data,
		BaseName:    tpl.ID + "-sample",
		PDFStandard: pdfStandard,
		Bundle:      bundle,
	})
}

// resolveTemplate picks the template to use. An explicit id must match the
// payload's kind: rendering a policy through the business-report template
// produces a PDF with every field blank, which looks like a template bug rather
// than the mismatch it is.
func (s *Service) resolveTemplate(templateID string, kind Kind) (Template, error) {
	if templateID == "" {
		tpl, ok := DefaultTemplateFor(kind)
		if !ok {
			return Template{}, fmt.Errorf("no template registered for %s documents", kind)
		}
		return tpl, nil
	}
	tpl, ok := Lookup(templateID)
	if !ok {
		return Template{}, fmt.Errorf("unknown template %q", templateID)
	}
	if kind != "" && tpl.Kind != kind {
		return Template{}, fmt.Errorf("template %q renders %s documents, not %s", tpl.ID, tpl.Kind, kind)
	}
	return tpl, nil
}

// SampleData reads a template's bundled sample payload.
func SampleData(tpl Template) ([]byte, error) {
	if tpl.SampleData == "" {
		return nil, fmt.Errorf("template %q has no bundled sample", tpl.ID)
	}
	path := filepath.Join(Dir(), filepath.FromSlash(tpl.SampleData))
	// #nosec G304 -- the path comes from the compile-time template registry
	// joined to the resolved templates directory, not from request input.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading sample payload %s: %w", tpl.SampleData, err)
	}
	return data, nil
}

// Catalog is everything the gallery page needs in one response.
type Catalog struct {
	Templates    []CatalogEntry `json:"templates"`
	Engines      []EngineStatus `json:"engines"`
	Brand        Brand          `json:"brand"`
	BrandSaved   bool           `json:"brand_saved"`
	Standards    any            `json:"pdf_standards"`
	TemplatesDir string         `json:"templates_dir"`
	DirAvailable bool           `json:"templates_dir_available"`
}

// CatalogEntry is a registry entry joined to the state of the engine it needs,
// so the UI never has to correlate the two itself.
type CatalogEntry struct {
	Template
	EngineAvailable bool   `json:"engine_available"`
	EngineCommand   string `json:"engine_command"`
	EngineVersion   string `json:"engine_version"`
	EngineDetail    string `json:"engine_detail"`
	HasSample       bool   `json:"has_sample"`
}

func (s *Service) Catalog() (Catalog, error) {
	brand, err := s.Brand()
	if err != nil {
		return Catalog{}, err
	}
	saved := brand.UpdatedAt != ""

	statuses := EngineStatuses()
	entries := make([]CatalogEntry, 0, len(Templates))
	for _, tpl := range Templates {
		status := statuses[tpl.Engine]
		entries = append(entries, CatalogEntry{
			Template:        tpl,
			EngineAvailable: status.Available,
			EngineCommand:   status.Command,
			EngineVersion:   status.Version,
			EngineDetail:    status.Detail,
			HasSample:       tpl.SampleData != "",
		})
	}

	// Ordered rather than ranged over the map, so the gallery does not shuffle
	// its engine cards between page loads.
	engines := make([]EngineStatus, 0, len(statuses))
	for _, engine := range []Engine{EngineTypst, EngineLaTeX} {
		engines = append(engines, statuses[engine])
	}

	return Catalog{
		Templates:    entries,
		Engines:      engines,
		Brand:        brand,
		BrandSaved:   saved,
		Standards:    PDFStandards,
		TemplatesDir: Dir(),
		DirAvailable: DirAvailable(),
	}, nil
}

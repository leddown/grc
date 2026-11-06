package securitynfr

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"grc/internal/nfrfile"
)

var ErrNotFound = errors.New("security NFR not found")

var ErrInvalidWeight = fmt.Errorf("weight must be a whole number from %d to %d", MinWeight, MaxWeight)

type Service struct {
	repo     Repository
	flatPath string
}

func NewService(repo Repository, flatPath string) *Service {
	return &Service{
		repo:     repo,
		flatPath: flatPath,
	}
}

// ImportResult says what an import did to the catalog.
type ImportResult struct {
	Imported int `json:"imported"`
	Removed  int `json:"removed"`
}

// ErrInvalidImport marks a file that was refused before anything was written.
var ErrInvalidImport = errors.New("invalid security NFR file")

// Import loads Security NFRs in the nfrfile JSON shape, keyed by record key.
// Every entry in the file is inserted or updated; with replace, NFRs the file
// does not carry are deleted, so the file becomes the whole catalog.
func (s *Service) Import(raw []byte, replace bool) (ImportResult, error) {
	fileData, err := nfrfile.ReadBytes(raw)
	if err != nil {
		return ImportResult{}, fmt.Errorf("%w: %w", ErrInvalidImport, err)
	}
	if len(fileData) == 0 {
		return ImportResult{}, fmt.Errorf("%w: the file holds no NFRs", ErrInvalidImport)
	}

	existing, err := s.repo.List("", "")
	if err != nil {
		return ImportResult{}, err
	}
	// A file that carries no weight for an NFR must not reset one set in the
	// application.
	weights := make(map[string]int, len(existing))
	for _, item := range existing {
		weights[item.Key] = item.Weight
	}

	incoming := make(map[string]struct{}, len(fileData))
	toImport := make([]NFR, 0, len(fileData))
	for key, entry := range fileData {
		normalizedKey := normalize(key)
		if normalizedKey == "" {
			return ImportResult{}, fmt.Errorf("%w: an entry has an empty key", ErrInvalidImport)
		}
		if _, dup := incoming[normalizedKey]; dup {
			return ImportResult{}, fmt.Errorf("%w: key %q appears more than once", ErrInvalidImport, normalizedKey)
		}
		incoming[normalizedKey] = struct{}{}
		if normalize(entry.Summary) == "" {
			return ImportResult{}, fmt.Errorf("%w: NFR %q has no summary", ErrInvalidImport, normalizedKey)
		}

		weight := entry.Weight
		if weight != 0 && !ValidWeight(weight) {
			return ImportResult{}, fmt.Errorf("%w: NFR %q: %w", ErrInvalidImport, normalizedKey, ErrInvalidWeight)
		}
		if weight == 0 {
			weight = weights[normalizedKey]
		}
		if !ValidWeight(weight) {
			weight = DefaultWeight
		}

		toImport = append(toImport, NFR{
			Key:               normalizedKey,
			ID:                normalize(nfrfile.IDToString(entry.ID)),
			Summary:           normalize(entry.Summary),
			IssueType:         normalize(entry.IssueType),
			Description:       normalize(entry.Description),
			NISTMapping:       normalize(entry.NISTMapping),
			AdditionalDetails: normalize(entry.AdditionalDetails),
			Implementation:    normalize(entry.Implementation),
			Domain:            normalize(entry.Domain),
			Weight:            weight,
		})
	}

	if err := s.repo.UpsertMany(toImport); err != nil {
		return ImportResult{}, fmt.Errorf("store imported NFRs: %w", err)
	}
	result := ImportResult{Imported: len(toImport)}
	if !replace {
		return result, nil
	}

	for _, item := range existing {
		if _, ok := incoming[item.Key]; ok {
			continue
		}
		if err := s.repo.DeleteByKey(item.Key); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, fmt.Errorf("remove NFR %q: %w", item.Key, err)
		}
		result.Removed++
	}
	return result, nil
}

func (s *Service) List(search, domain string) ([]NFR, error) {
	return s.repo.List(strings.TrimSpace(search), strings.TrimSpace(domain))
}

func (s *Service) Get(key string) (NFR, error) {
	item, err := s.repo.GetByKey(normalize(key))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NFR{}, ErrNotFound
		}
		return NFR{}, err
	}
	return item, nil
}

func (s *Service) Update(key string, nfr NFR) (NFR, error) {
	key = normalize(key)
	nfr.Key = key
	nfr.ID = normalize(nfr.ID)
	nfr.Summary = normalize(nfr.Summary)
	nfr.IssueType = normalize(nfr.IssueType)
	nfr.Description = normalize(nfr.Description)
	nfr.NISTMapping = normalize(nfr.NISTMapping)
	nfr.AdditionalDetails = normalize(nfr.AdditionalDetails)
	nfr.Implementation = normalize(nfr.Implementation)
	nfr.Domain = normalize(nfr.Domain)

	if nfr.Summary == "" {
		return NFR{}, fmt.Errorf("summary is required")
	}

	// A zero weight means the caller did not say: the NFR keeps the weight it
	// has, and a new one starts in the middle.
	if nfr.Weight == 0 {
		nfr.Weight = DefaultWeight
		current, err := s.repo.GetByKey(key)
		if err == nil {
			nfr.Weight = current.Weight
		} else if !errors.Is(err, sql.ErrNoRows) {
			return NFR{}, err
		}
	}
	if !ValidWeight(nfr.Weight) {
		return NFR{}, ErrInvalidWeight
	}

	if err := s.repo.Upsert(nfr); err != nil {
		return NFR{}, err
	}
	return s.repo.GetByKey(key)
}

func (s *Service) SetWeight(key string, weight int) (NFR, error) {
	if !ValidWeight(weight) {
		return NFR{}, ErrInvalidWeight
	}
	key = normalize(key)
	if err := s.repo.SetWeight(key, weight); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NFR{}, ErrNotFound
		}
		return NFR{}, err
	}
	return s.repo.GetByKey(key)
}

func (s *Service) Delete(key string) error {
	err := s.repo.DeleteByKey(normalize(key))
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *Service) SaveToJSON() (int, string, error) {
	if _, err := os.Stat(s.flatPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, s.flatPath, fmt.Errorf("json save target %q does not exist in this runtime environment", s.flatPath)
		}
		return 0, s.flatPath, err
	}

	items, err := s.repo.List("", "")
	if err != nil {
		return 0, s.flatPath, err
	}

	fileData := make(nfrfile.File, len(items))
	for _, item := range items {
		fileData[item.Key] = nfrfile.Entry{
			Summary:           item.Summary,
			ID:                parseID(item.ID),
			IssueType:         item.IssueType,
			Description:       item.Description,
			NISTMapping:       item.NISTMapping,
			AdditionalDetails: item.AdditionalDetails,
			Implementation:    item.Implementation,
			Domain:            item.Domain,
			Weight:            item.Weight,
		}
	}

	if err := nfrfile.Write(s.flatPath, fileData); err != nil {
		return 0, s.flatPath, err
	}

	return len(items), s.flatPath, nil
}

func normalize(value string) string {
	return strings.TrimSpace(value)
}

func parseID(value string) any {
	if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
		return parsed
	}
	return strings.TrimSpace(value)
}

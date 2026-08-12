package securitynfr

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	seeddata "grc/internal/data"
	"grc/internal/nfrfile"
)

var ErrNotFound = errors.New("security NFR not found")

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

func (s *Service) SourceHash() (string, error) {
	raw, err := s.sourceBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) Seed() (int, error) {
	raw, err := s.sourceBytes()
	if err != nil {
		return 0, err
	}
	fileData, err := nfrfile.ReadBytes(raw)
	if err != nil {
		return 0, err
	}

	incoming := make(map[string]struct{}, len(fileData))
	toSeed := make([]NFR, 0, len(fileData))
	for key, entry := range fileData {
		normalizedKey := normalize(key)
		incoming[normalizedKey] = struct{}{}

		toSeed = append(toSeed, NFR{
			Key:               normalizedKey,
			ID:                normalize(nfrfile.IDToString(entry.ID)),
			Summary:           normalize(entry.Summary),
			IssueType:         normalize(entry.IssueType),
			Description:       normalize(entry.Description),
			NISTMapping:       normalize(entry.NISTMapping),
			AdditionalDetails: normalize(entry.AdditionalDetails),
			Implementation:    normalize(entry.Implementation),
			Domain:            normalize(entry.Domain),
		})
	}

	if err := s.repo.UpsertMany(toSeed); err != nil {
		return 0, err
	}
	seeded := len(toSeed)

	existing, err := s.repo.List("", "")
	if err != nil {
		return seeded, err
	}
	for _, item := range existing {
		if _, ok := incoming[item.Key]; ok {
			continue
		}
		if err := s.repo.DeleteByKey(item.Key); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return seeded, err
		}
	}

	return seeded, nil
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

	if err := s.repo.Upsert(nfr); err != nil {
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

func (s *Service) sourceBytes() ([]byte, error) {
	if raw, err := os.ReadFile(s.flatPath); err == nil {
		return raw, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return seeddata.SecurityNFRJSON(), nil
}

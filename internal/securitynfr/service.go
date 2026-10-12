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

	existing, err := s.repo.List("", "")
	if err != nil {
		return 0, err
	}
	// A source file that carries no weight for an NFR must not reset one set in
	// the application: the embedded catalog carries none at all.
	weights := make(map[string]int, len(existing))
	for _, item := range existing {
		weights[item.Key] = item.Weight
	}

	incoming := make(map[string]struct{}, len(fileData))
	toSeed := make([]NFR, 0, len(fileData))
	for key, entry := range fileData {
		normalizedKey := normalize(key)
		incoming[normalizedKey] = struct{}{}

		weight := entry.Weight
		if !ValidWeight(weight) {
			weight = weights[normalizedKey]
		}
		if !ValidWeight(weight) {
			weight = DefaultWeight
		}

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
			Weight:            weight,
		})
	}

	if err := s.repo.UpsertMany(toSeed); err != nil {
		return 0, err
	}
	seeded := len(toSeed)

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

func (s *Service) sourceBytes() ([]byte, error) {
	if raw, err := os.ReadFile(s.flatPath); err == nil {
		return raw, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return seeddata.SecurityNFRJSON(), nil
}

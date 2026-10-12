package riskregister

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrNotFound = errors.New("risk register item not found")

// maxRiskFieldLength bounds free-text fields so a single risk row can't be
// used to store an arbitrarily large payload.
const maxRiskFieldLength = 10000

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(search, status string) ([]Risk, error) {
	return s.repo.List(strings.TrimSpace(search), strings.TrimSpace(status))
}

func (s *Service) Create(risk Risk) (Risk, error) {
	normalized, err := normalizeAndValidate(risk)
	if err != nil {
		return Risk{}, err
	}
	return s.repo.Create(normalized)
}

func (s *Service) Update(id int64, risk Risk) (Risk, error) {
	normalized, err := normalizeAndValidate(risk)
	if err != nil {
		return Risk{}, err
	}
	updated, err := s.repo.Update(id, normalized)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Risk{}, ErrNotFound
		}
		return Risk{}, err
	}
	return updated, nil
}

func (s *Service) Delete(id int64) error {
	err := s.repo.Delete(id)
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func normalizeAndValidate(risk Risk) (Risk, error) {
	risk.RiskID = strings.TrimSpace(risk.RiskID)
	risk.Title = strings.TrimSpace(risk.Title)
	risk.BusinessUnit = strings.TrimSpace(risk.BusinessUnit)
	risk.Asset = strings.TrimSpace(risk.Asset)
	risk.ThreatSource = strings.TrimSpace(risk.ThreatSource)
	risk.Vulnerability = strings.TrimSpace(risk.Vulnerability)
	risk.CurrentControls = strings.TrimSpace(risk.CurrentControls)
	risk.ResponseStrategy = strings.TrimSpace(risk.ResponseStrategy)
	risk.ResponseAction = strings.TrimSpace(risk.ResponseAction)
	risk.Owner = strings.TrimSpace(risk.Owner)
	risk.Status = strings.TrimSpace(risk.Status)
	risk.TargetDate = strings.TrimSpace(risk.TargetDate)
	risk.LastReviewDate = strings.TrimSpace(risk.LastReviewDate)
	risk.NextReviewDate = strings.TrimSpace(risk.NextReviewDate)
	risk.Notes = strings.TrimSpace(risk.Notes)

	if risk.RiskID == "" {
		return Risk{}, fmt.Errorf("risk_id is required")
	}
	if risk.Title == "" {
		return Risk{}, fmt.Errorf("title is required")
	}
	if risk.Asset == "" {
		return Risk{}, fmt.Errorf("asset is required")
	}
	if risk.Owner == "" {
		return Risk{}, fmt.Errorf("owner is required")
	}
	if risk.Status == "" {
		risk.Status = "Open"
	}

	for _, field := range []string{
		risk.RiskID, risk.Title, risk.BusinessUnit, risk.Asset, risk.ThreatSource, risk.Vulnerability,
		risk.CurrentControls, risk.ResponseStrategy, risk.ResponseAction, risk.Owner, risk.Notes,
	} {
		if len(field) > maxRiskFieldLength {
			return Risk{}, fmt.Errorf("fields must be at most %d characters", maxRiskFieldLength)
		}
	}

	risk.Likelihood = clampScore(risk.Likelihood)
	risk.Impact = clampScore(risk.Impact)
	risk.ResidualLikelihood = clampScore(risk.ResidualLikelihood)
	risk.ResidualImpact = clampScore(risk.ResidualImpact)
	risk.InherentScore = risk.Likelihood * risk.Impact
	risk.ResidualScore = risk.ResidualLikelihood * risk.ResidualImpact

	if risk.ResponseStrategy == "" {
		risk.ResponseStrategy = "Mitigate"
	}
	return risk, nil
}

func clampScore(value int) int {
	if value < 1 {
		return 1
	}
	if value > 5 {
		return 5
	}
	return value
}

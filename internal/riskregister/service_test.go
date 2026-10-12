package riskregister

import "testing"

type stubRepo struct {
	last Risk
}

func (s *stubRepo) List(_, _ string) ([]Risk, error)     { return nil, nil }
func (s *stubRepo) GetByID(_ int64) (Risk, error)        { return Risk{}, nil }
func (s *stubRepo) Delete(_ int64) error                 { return nil }
func (s *stubRepo) Create(r Risk) (Risk, error)          { s.last = r; r.ID = 1; return r, nil }
func (s *stubRepo) Update(_ int64, r Risk) (Risk, error) { s.last = r; r.ID = 1; return r, nil }

func TestCreateNormalizesScoresAndDefaults(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	_, err := svc.Create(Risk{
		RiskID:             " RISK-1 ",
		Title:              "  Exposure in API gateway ",
		Asset:              "API",
		Owner:              "SecOps",
		Likelihood:         8,
		Impact:             0,
		ResidualLikelihood: -1,
		ResidualImpact:     3,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if repo.last.Likelihood != 5 || repo.last.Impact != 1 {
		t.Fatalf("expected clamped inherent components, got L=%d I=%d", repo.last.Likelihood, repo.last.Impact)
	}
	if repo.last.InherentScore != 5 {
		t.Fatalf("expected inherent score 5, got %d", repo.last.InherentScore)
	}
	if repo.last.ResidualLikelihood != 1 || repo.last.ResidualImpact != 3 || repo.last.ResidualScore != 3 {
		t.Fatalf("unexpected residual scores: %+v", repo.last)
	}
	if repo.last.ResponseStrategy != "Mitigate" {
		t.Fatalf("expected default response strategy")
	}
	if repo.last.Status != "Open" {
		t.Fatalf("expected default status Open")
	}
}

func TestCreateRequiresCoreFields(t *testing.T) {
	svc := NewService(&stubRepo{})
	_, err := svc.Create(Risk{Title: "x", Asset: "y", Owner: "z"})
	if err == nil || err.Error() != "risk_id is required" {
		t.Fatalf("expected risk_id validation, got %v", err)
	}
}

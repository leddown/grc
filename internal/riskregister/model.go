package riskregister

type Risk struct {
	ID                  int64  `json:"id"`
	RiskID              string `json:"risk_id"`
	Title               string `json:"title"`
	BusinessUnit        string `json:"business_unit"`
	Asset               string `json:"asset"`
	ThreatSource        string `json:"threat_source"`
	Vulnerability       string `json:"vulnerability"`
	Likelihood          int    `json:"likelihood"`
	Impact              int    `json:"impact"`
	InherentScore       int    `json:"inherent_score"`
	CurrentControls     string `json:"current_controls"`
	ResidualLikelihood  int    `json:"residual_likelihood"`
	ResidualImpact      int    `json:"residual_impact"`
	ResidualScore       int    `json:"residual_score"`
	ResponseStrategy    string `json:"response_strategy"`
	ResponseAction      string `json:"response_action"`
	Owner               string `json:"owner"`
	Status              string `json:"status"`
	TargetDate          string `json:"target_date"`
	LastReviewDate      string `json:"last_review_date"`
	NextReviewDate      string `json:"next_review_date"`
	RiskAppetiteAligned bool   `json:"risk_appetite_aligned"`
	Notes               string `json:"notes"`
}

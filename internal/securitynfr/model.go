package securitynfr

type NFR struct {
	Key               string `json:"key"`
	ID                string `json:"id"`
	Summary           string `json:"summary"`
	IssueType         string `json:"issue_type"`
	Description       string `json:"description"`
	NISTMapping       string `json:"nist_mapping"`
	AdditionalDetails string `json:"additional_details"`
	Implementation    string `json:"implementation"`
	Domain            string `json:"domain"`
}

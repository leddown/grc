package securitynfr

// Weight is how much an NFR counts relative to the others: 1 is the lowest,
// 5 the highest, and an NFR nobody has weighted sits in the middle.
const (
	MinWeight     = 1
	MaxWeight     = 5
	DefaultWeight = 3
)

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
	Weight            int    `json:"weight"`
}

func ValidWeight(weight int) bool {
	return weight >= MinWeight && weight <= MaxWeight
}

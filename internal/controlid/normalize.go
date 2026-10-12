package controlid

import (
	"strings"
)

// Normalize converts IDs like "AC-2.1" to "AC-2(1)" and uppercases/trim spaces.
func Normalize(raw string) string {
	raw = strings.TrimSpace(strings.ToUpper(raw))
	if raw == "" {
		return ""
	}
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) == 1 {
		return raw
	}
	return parts[0] + "(" + parts[1] + ")"
}

func Family(controlID string) string {
	controlID = strings.TrimSpace(controlID)
	if controlID == "" {
		return ""
	}
	parts := strings.SplitN(controlID, "-", 2)
	return parts[0]
}

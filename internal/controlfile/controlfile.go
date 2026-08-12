package controlfile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type File map[string]Entry

type BaselineFlags struct {
	High     bool `json:"high"`
	Low      bool `json:"low"`
	Moderate bool `json:"moderate"`
	Privacy  bool `json:"privacy"`
}

type NISTEntry struct {
	ControlID string        `json:"control_id"`
	Type      string        `json:"type"`
	Name      string        `json:"name"`
	Threats   []string      `json:"threats"`
	Baselines BaselineFlags `json:"baselines"`
	CIA       struct {
		Confidentiality bool   `json:"confidentiality"`
		Integrity       bool   `json:"integrity"`
		Availability    bool   `json:"availability"`
		Justification   string `json:"justification"`
	} `json:"cia"`
	Requirements    string   `json:"nist_requirements"`
	Discussion      string   `json:"nist_discussion"`
	RelatedControls []string `json:"related_controls"`
}

func (e *NISTEntry) UnmarshalJSON(data []byte) error {
	type rawNISTEntry struct {
		ControlID string        `json:"control_id"`
		Type      string        `json:"type"`
		Name      string        `json:"name"`
		Threats   []string      `json:"threats"`
		Baselines BaselineFlags `json:"baselines"`
		CIA       struct {
			Confidentiality bool   `json:"confidentiality"`
			Integrity       bool   `json:"integrity"`
			Availability    bool   `json:"availability"`
			Justification   string `json:"justification"`
		} `json:"cia"`
		RequirementsLegacy string   `json:"requirements"`
		RequirementsNIST   string   `json:"nist_requirements"`
		DiscussionLegacy   string   `json:"discussion"`
		DiscussionNIST     string   `json:"nist_discussion"`
		RelatedControls    []string `json:"related_controls"`
	}

	var raw rawNISTEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	e.ControlID = raw.ControlID
	e.Type = raw.Type
	e.Name = raw.Name
	e.Threats = raw.Threats
	e.Baselines = raw.Baselines
	e.CIA = raw.CIA
	e.RelatedControls = raw.RelatedControls

	e.Requirements = raw.RequirementsNIST
	if strings.TrimSpace(e.Requirements) == "" {
		e.Requirements = raw.RequirementsLegacy
	}
	e.Discussion = raw.DiscussionNIST
	if strings.TrimSpace(e.Discussion) == "" {
		e.Discussion = raw.DiscussionLegacy
	}

	return nil
}

type Entry struct {
	NIST     NISTEntry      `json:"nist"`
	Appendix map[string]any `json:"appendix"`
}

func (e *Entry) UnmarshalJSON(data []byte) error {
	type nestedEntry struct {
		NIST     *NISTEntry      `json:"nist"`
		Appendix json.RawMessage `json:"appendix"`
	}

	var nested nestedEntry
	if err := json.Unmarshal(data, &nested); err == nil && nested.NIST != nil {
		e.NIST = *nested.NIST
		e.Appendix = decodeAppendix(nested.Appendix)
		return nil
	}

	var legacy NISTEntry
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	e.NIST = legacy
	e.Appendix = nil
	return nil
}

func decodeAppendix(raw json.RawMessage) map[string]any {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}

	var appendix map[string]any
	if err := json.Unmarshal([]byte(trimmed), &appendix); err != nil {
		return nil
	}
	if len(appendix) == 0 {
		return nil
	}
	return appendix
}

func Read(path string) (File, error) {
	cleanPath := filepath.Clean(path)
	// #nosec G304 -- path is provided by trusted application configuration, not direct user input.
	raw, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}

	return ReadBytes(raw)
}

func ReadBytes(raw []byte) (File, error) {
	var data File
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}

	return data, nil
}

func Write(path string, data File) error {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, leftErr := strconv.Atoi(keys[i])
		right, rightErr := strconv.Atoi(keys[j])
		if leftErr == nil && rightErr == nil {
			return left < right
		}
		return keys[i] < keys[j]
	})

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, key := range keys {
		keyRaw, err := json.Marshal(key)
		if err != nil {
			return err
		}
		entryRaw, err := json.MarshalIndent(data[key], "  ", "  ")
		if err != nil {
			return err
		}
		buf.WriteString("  ")
		buf.Write(keyRaw)
		buf.WriteString(": ")
		buf.Write(bytes.ReplaceAll(entryRaw, []byte("\n"), []byte("\n  ")))
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")

	cleanPath := filepath.Clean(path)
	return os.WriteFile(cleanPath, buf.Bytes(), 0o600)
}

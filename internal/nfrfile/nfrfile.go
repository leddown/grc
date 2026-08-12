package nfrfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type File map[string]Entry

type Entry struct {
	Summary           string `json:"Summary"`
	ID                any    `json:"ID"`
	IssueType         string `json:"Issue Type"`
	Description       string `json:"Description"`
	NISTMapping       string `json:"NIST Mapping"`
	AdditionalDetails string `json:"Additional Details"`
	Implementation    string `json:"Implementation"`
	Domain            string `json:"Domain"`
}

func (e *Entry) UnmarshalJSON(data []byte) error {
	type rawEntry struct {
		Summary              string `json:"Summary"`
		ID                   any    `json:"ID"`
		IssueType            string `json:"Issue Type"`
		Description          string `json:"Description"`
		NISTMapping          string `json:"NIST Mapping"`
		AdditionalDetails    string `json:"Additional Details"`
		AdditionalDetailsOld string `json:"Additional Details "`
		Implementation       string `json:"Implementation"`
		Domain               string `json:"Domain"`
	}

	var raw rawEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	e.Summary = raw.Summary
	e.ID = raw.ID
	e.IssueType = raw.IssueType
	e.Description = raw.Description
	e.NISTMapping = raw.NISTMapping
	e.AdditionalDetails = strings.TrimSpace(raw.AdditionalDetails)
	if e.AdditionalDetails == "" {
		e.AdditionalDetails = raw.AdditionalDetailsOld
	}
	e.Implementation = raw.Implementation
	e.Domain = raw.Domain
	return nil
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
		li, lErr := strconv.Atoi(keys[i])
		ri, rErr := strconv.Atoi(keys[j])
		if lErr == nil && rErr == nil {
			return li < ri
		}
		return keys[i] < keys[j]
	})

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, key := range keys {
		entryRaw, err := json.MarshalIndent(data[key], "  ", "  ")
		if err != nil {
			return err
		}
		buf.WriteString("  ")
		keyRaw, _ := json.Marshal(key)
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

func IDToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", v), "0"), ".")
	case json.Number:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

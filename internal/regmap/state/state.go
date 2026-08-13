// Package state persists the pipeline: which framework is in flight, how far
// each gate has progressed, and the requirement/mapping data files. Every
// command is resumable and idempotent because all of it round-trips here.
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"grc/internal/regmap/mapping"
	"grc/internal/regmap/requirement"
)

// Gate numbers.
const (
	GateSegmentation = 1
	GateMapping      = 2
	GateSuggestion   = 3
)

// Gate progress states.
const (
	GatePending    = "pending"
	GateInProgress = "in_progress"
	GateComplete   = "complete"
)

// GateState records progress through one review gate.
type GateState struct {
	Gate         int    `yaml:"gate"`
	Status       string `yaml:"status"`
	Approved     int    `yaml:"approved"`
	Rejected     int    `yaml:"rejected"`
	Skipped      int    `yaml:"skipped"`
	AutoApproved bool   `yaml:"autoApproved,omitempty"`
	ReviewedBy   string `yaml:"reviewedBy,omitempty"`
	CompletedAt  string `yaml:"completedAt,omitempty"`
}

// State is the contents of data/state.yaml.
type State struct {
	Framework   string `yaml:"framework"`
	DisplayName string `yaml:"displayName,omitempty"`
	SourceRef   string `yaml:"sourceRef,omitempty"`
	SourceFile  string `yaml:"sourceFile,omitempty"`
	// DetectionNote explains how the framework was chosen, so the reviewer can
	// judge the guess when confirming it at GATE 1.
	DetectionNote string `yaml:"detectionNote,omitempty"`
	// FrameworkConfirmed records that a human confirmed the framework.
	FrameworkConfirmed bool        `yaml:"frameworkConfirmed"`
	IngestedAt         string      `yaml:"ingestedAt,omitempty"`
	UpdatedAt          string      `yaml:"updatedAt,omitempty"`
	Gates              []GateState `yaml:"gates"`
}

// Gate returns the state of gate n, creating a pending entry if absent.
func (s *State) Gate(n int) *GateState {
	for i := range s.Gates {
		if s.Gates[i].Gate == n {
			return &s.Gates[i]
		}
	}
	s.Gates = append(s.Gates, GateState{Gate: n, Status: GatePending})
	sort.SliceStable(s.Gates, func(i, j int) bool { return s.Gates[i].Gate < s.Gates[j].Gate })
	return s.Gate(n)
}

// GateComplete reports whether gate n has been signed off.
func (s *State) GateComplete(n int) bool {
	return s.Gate(n).Status == GateComplete
}

// Now returns the timestamp format used throughout the audit trail.
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

// Store reads and writes every persisted artifact under a data directory.
type Store struct {
	Dir string
}

// New returns a store rooted at dir.
func New(dir string) *Store { return &Store{Dir: dir} }

func (s *Store) statePath() string        { return filepath.Join(s.Dir, "state.yaml") }
func (s *Store) requirementsPath() string { return filepath.Join(s.Dir, "requirements.yaml") }
func (s *Store) mappingsPath() string     { return filepath.Join(s.Dir, "mappings.yaml") }

// StatePath returns the location of the pipeline state file.
func (s *Store) StatePath() string { return s.statePath() }

// RequirementsPath returns the location of the requirements file.
func (s *Store) RequirementsPath() string { return s.requirementsPath() }

// MappingsPath returns the location of the mappings file.
func (s *Store) MappingsPath() string { return s.mappingsPath() }

// LoadState reads state.yaml, returning an empty state when it does not exist.
func (s *Store) LoadState() (*State, error) {
	var st State
	raw, err := os.ReadFile(s.statePath())
	if os.IsNotExist(err) {
		return &st, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if err := yaml.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", s.statePath(), err)
	}
	return &st, nil
}

// SaveState writes state.yaml atomically.
func (s *Store) SaveState(st *State) error {
	st.UpdatedAt = Now()
	sort.SliceStable(st.Gates, func(i, j int) bool { return st.Gates[i].Gate < st.Gates[j].Gate })
	return s.write(s.statePath(), st, "regmap pipeline state")
}

type requirementsFile struct {
	Framework    string          `yaml:"framework"`
	SourceRef    string          `yaml:"sourceRef,omitempty"`
	Requirements requirement.Set `yaml:"requirements"`
}

// LoadRequirements reads requirements.yaml, returning nil when absent.
func (s *Store) LoadRequirements() (requirement.Set, error) {
	raw, err := os.ReadFile(s.requirementsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read requirements: %w", err)
	}
	var f requirementsFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse requirements %s: %w", s.requirementsPath(), err)
	}
	f.Requirements.Sort()
	return f.Requirements, nil
}

// SaveRequirements writes requirements.yaml atomically in deterministic order.
func (s *Store) SaveRequirements(framework, sourceRef string, reqs requirement.Set) error {
	reqs.Sort()
	f := requirementsFile{Framework: framework, SourceRef: sourceRef, Requirements: reqs}
	return s.write(s.requirementsPath(), f, "regmap requirements (status: draft|approved|rejected)")
}

type mappingsFile struct {
	Framework string      `yaml:"framework"`
	SourceRef string      `yaml:"sourceRef,omitempty"`
	Mappings  mapping.Set `yaml:"mappings"`
}

// LoadMappings reads mappings.yaml, returning nil when absent.
func (s *Store) LoadMappings() (mapping.Set, error) {
	raw, err := os.ReadFile(s.mappingsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read mappings: %w", err)
	}
	var f mappingsFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse mappings %s: %w", s.mappingsPath(), err)
	}
	f.Mappings.Sort()
	return f.Mappings, nil
}

// SaveMappings writes mappings.yaml atomically in deterministic order.
func (s *Store) SaveMappings(framework, sourceRef string, entries mapping.Set) error {
	entries.Sort()
	f := mappingsFile{Framework: framework, SourceRef: sourceRef, Mappings: entries}
	return s.write(s.mappingsPath(), f, "regmap crosswalk (nothing is approved without human review)")
}

// write marshals v to path via a temp file + rename so an interrupted run can
// never leave a half-written state file behind.
func (s *Store) write(path string, v any, banner string) error {
	// 0750: pipeline state carries regulatory source text and review decisions,
	// so it is not world-readable.
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return fmt.Errorf("create data directory %s: %w", s.Dir, err)
	}
	body, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	content := append([]byte("# "+banner+"\n"), body...)

	tmp, err := os.CreateTemp(s.Dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", s.Dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

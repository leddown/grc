package controlcatalog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"grc/internal/controlfile"
	"grc/internal/controlid"
)

type Service struct {
	repo     Repository
	flatPath string
}

var ErrNotFound = errors.New("control not found")

type Control struct {
	SourceKey                    string         `json:"source_key"`
	ID                           string         `json:"id"`
	Type                         string         `json:"type"`
	Name                         string         `json:"name"`
	Family                       string         `json:"family"`
	Baselines                    []string       `json:"baselines"`
	Threats                      []string       `json:"threats"`
	MappingBaselines             Baselines      `json:"mapping_baselines"`
	Confidentiality              string         `json:"confidentiality"`
	Integrity                    string         `json:"integrity"`
	Availability                 string         `json:"availability"`
	Justification                string         `json:"justification"`
	PotentiallyCommonInheritable string         `json:"potentially_common_inheritable"`
	Requirements                 string         `json:"requirements"`
	Discussion                   string         `json:"discussion"`
	RelatedControls              []string       `json:"related_controls"`
	Appendix                     map[string]any `json:"appendix"`
	// The upstream OSCAL structure an OSCAL import stored: the params array,
	// the statement part and the assessment parts, as OSCAL JSON. Empty for a
	// control never imported from OSCAL. Only Get fills them; a list would
	// otherwise carry the whole 800-53A text with every control.
	Params     json.RawMessage `json:"params,omitempty"`
	Statement  json.RawMessage `json:"statement,omitempty"`
	Assessment json.RawMessage `json:"assessment,omitempty"`
}

type FamilySetting struct {
	Family  string `json:"family"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func NewService(repo Repository, flatPath string) *Service {
	return &Service{
		repo:     repo,
		flatPath: flatPath,
	}
}

// ImportResult says what an import did to the catalog.
type ImportResult struct {
	Imported int `json:"imported"`
	Removed  int `json:"removed"`
}

// ErrInvalidImport marks a file that was refused before anything was written.
var ErrInvalidImport = errors.New("invalid control catalog file")

// Import loads a control catalog in the controlfile JSON shape. Every entry in
// the file is inserted or updated; with replace, controls the file does not
// carry are deleted, so the file becomes the whole catalog.
func (s *Service) Import(raw []byte, replace bool) (ImportResult, error) {
	controls, err := controlfile.ReadBytes(raw)
	if err != nil {
		return ImportResult{}, fmt.Errorf("%w: %w", ErrInvalidImport, err)
	}
	if len(controls) == 0 {
		return ImportResult{}, fmt.Errorf("%w: the file holds no controls", ErrInvalidImport)
	}

	keys := make([]string, 0, len(controls))
	for recordKey := range controls {
		keys = append(keys, recordKey)
	}
	sortRecordKeys(keys)

	incoming := make(map[string]struct{}, len(controls))
	toImport := make([]Control, 0, len(keys))
	for _, recordKey := range keys {
		entry := controls[recordKey]
		controlID := controlid.Normalize(entry.NIST.ControlID)
		if controlID == "" {
			controlID = controlid.Normalize(recordKey)
		}
		if controlID == "" {
			return ImportResult{}, fmt.Errorf("%w: record %q has no control id", ErrInvalidImport, recordKey)
		}
		if _, dup := incoming[controlID]; dup {
			return ImportResult{}, fmt.Errorf("%w: control %s appears more than once", ErrInvalidImport, controlID)
		}
		incoming[controlID] = struct{}{}
		control := Control{
			SourceKey:        strings.TrimSpace(recordKey),
			ID:               controlID,
			Type:             strings.TrimSpace(entry.NIST.Type),
			Name:             strings.TrimSpace(entry.NIST.Name),
			Family:           controlFamily(controlID),
			Baselines:        deriveBaselineMembership(flagsToBaselines(entry.NIST.Baselines, controlID)),
			Threats:          uniqueSorted(entry.NIST.Threats),
			MappingBaselines: normalizeBaselines(flagsToBaselines(entry.NIST.Baselines, controlID)),
			Confidentiality:  boolFlag(entry.NIST.CIA.Confidentiality),
			Integrity:        boolFlag(entry.NIST.CIA.Integrity),
			Availability:     boolFlag(entry.NIST.CIA.Availability),
			Justification:    strings.TrimSpace(entry.NIST.CIA.Justification),
			Requirements:     strings.TrimSpace(entry.NIST.Requirements),
			Discussion:       strings.TrimSpace(entry.NIST.Discussion),
			RelatedControls:  uniqueSorted(entry.NIST.RelatedControls),
			Appendix:         entry.Appendix,
		}
		normalizeControl(&control)
		if control.Name == "" {
			return ImportResult{}, fmt.Errorf("%w: control %s has no name", ErrInvalidImport, controlID)
		}
		toImport = append(toImport, control)
	}

	if err := s.repo.UpsertMany(toImport); err != nil {
		return ImportResult{}, fmt.Errorf("store imported controls: %w", err)
	}
	result := ImportResult{Imported: len(toImport)}
	if !replace {
		return result, nil
	}

	existing, err := s.repo.List("", "", "")
	if err != nil {
		return result, err
	}
	for _, control := range existing {
		if _, ok := incoming[control.ID]; ok {
			continue
		}
		if err := s.repo.DeleteByControlID(control.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, fmt.Errorf("remove control %s: %w", control.ID, err)
		}
		result.Removed++
	}
	return result, nil
}

func (s *Service) List(search, baseline, cia string) ([]Control, error) {
	controls, err := s.repo.List(strings.TrimSpace(search), strings.TrimSpace(baseline), strings.ToLower(strings.TrimSpace(cia)))
	if err != nil {
		return nil, err
	}
	return s.filterHiddenFamilies(controls)
}

func (s *Service) ListByFamily(family string) ([]Control, error) {
	family = strings.ToUpper(strings.TrimSpace(family))
	controls, err := s.repo.ListByFamily(family)
	if err != nil {
		return nil, err
	}
	if family == "" {
		return s.filterHiddenFamilies(controls)
	}

	hiddenFamilies, err := s.hiddenFamilySet()
	if err != nil {
		return nil, err
	}
	if _, hidden := hiddenFamilies[family]; hidden {
		return []Control{}, nil
	}
	return controls, nil
}

func (s *Service) filterHiddenFamilies(controls []Control) ([]Control, error) {
	hiddenFamilies, err := s.hiddenFamilySet()
	if err != nil {
		return nil, err
	}
	if len(hiddenFamilies) == 0 {
		return controls, nil
	}
	filtered := make([]Control, 0, len(controls))
	for _, control := range controls {
		if _, hidden := hiddenFamilies[strings.ToUpper(strings.TrimSpace(control.Family))]; hidden {
			continue
		}
		filtered = append(filtered, control)
	}
	return filtered, nil
}

func (s *Service) Get(controlID string) (Control, error) {
	controlID = controlid.Normalize(controlID)

	control, err := s.repo.GetByControlID(controlID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Control{}, ErrNotFound
		}
		return Control{}, err
	}
	cols, err := s.repo.oscalColumnsOf(controlID)
	if err != nil {
		return Control{}, err
	}
	control.Params = storedJSON(cols.Params)
	control.Statement = storedJSON(cols.Statement)
	control.Assessment = storedJSON(cols.Assessment)
	return control, nil
}

func storedJSON(value string) json.RawMessage {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" {
		return nil
	}
	return json.RawMessage(value)
}

func (s *Service) Update(controlID string, control Control) (Control, error) {
	controlID = controlid.Normalize(controlID)
	existing, err := s.repo.GetByControlID(controlID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Control{}, err
	}
	control.ID = controlID
	if strings.TrimSpace(control.SourceKey) == "" {
		control.SourceKey = strings.TrimSpace(existing.SourceKey)
	}
	if strings.TrimSpace(control.Type) == "" {
		control.Type = strings.TrimSpace(existing.Type)
	}
	if len(control.Appendix) == 0 {
		control.Appendix = existing.Appendix
	}
	normalizeControl(&control)

	if strings.TrimSpace(control.Name) == "" {
		return Control{}, fmt.Errorf("name is required")
	}

	if err := s.repo.Upsert(control); err != nil {
		return Control{}, err
	}

	return s.repo.GetByControlID(controlID)
}

func (s *Service) Delete(controlID string) error {
	controlID = controlid.Normalize(controlID)
	err := s.repo.DeleteByControlID(controlID)
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

	controls, err := s.repo.List("", "", "")
	if err != nil {
		return 0, s.flatPath, err
	}

	fileData := make(controlfile.File, len(controls))
	for i, control := range controls {
		normalizeControl(&control)
		entry := controlfile.Entry{
			NIST: controlfile.NISTEntry{
				ControlID: control.ID,
				Type:      control.Type,
				Name:      control.Name,
				Threats:   control.Threats,
				Baselines: baselinesToFlags(control.MappingBaselines),
				CIA: struct {
					Confidentiality bool   `json:"confidentiality"`
					Integrity       bool   `json:"integrity"`
					Availability    bool   `json:"availability"`
					Justification   string `json:"justification"`
				}{
					Confidentiality: control.Confidentiality != "",
					Integrity:       control.Integrity != "",
					Availability:    control.Availability != "",
					Justification:   strings.TrimSpace(control.Justification),
				},
				Requirements:    strings.TrimSpace(control.Requirements),
				Discussion:      strings.TrimSpace(control.Discussion),
				RelatedControls: control.RelatedControls,
			},
			Appendix: control.Appendix,
		}
		recordKey := strings.TrimSpace(control.SourceKey)
		if recordKey == "" {
			recordKey = strconv.Itoa(i + 1)
		}
		fileData[recordKey] = entry
	}

	if err := controlfile.Write(s.flatPath, fileData); err != nil {
		return 0, s.flatPath, err
	}

	return len(controls), s.flatPath, nil
}

func (s *Service) ListFamilySettings() ([]FamilySetting, error) {
	items, err := s.repo.ListFamilyVisibility()
	if err != nil {
		return nil, err
	}

	settings := make([]FamilySetting, 0, len(items))
	for _, item := range items {
		family := strings.ToUpper(strings.TrimSpace(item.Family))
		if family == "" {
			continue
		}
		settings = append(settings, FamilySetting{
			Family:  family,
			Name:    controlFamilyName(family),
			Enabled: item.Enabled,
		})
	}
	return settings, nil
}

func (s *Service) SetFamilyVisibility(family string, enabled bool) error {
	family = strings.ToUpper(strings.TrimSpace(family))
	if family == "" {
		return fmt.Errorf("family is required")
	}
	return s.repo.SetFamilyVisibility(family, enabled)
}

func (s *Service) hiddenFamilySet() (map[string]struct{}, error) {
	items, err := s.repo.ListFamilyVisibility()
	if err != nil {
		return nil, err
	}

	hidden := make(map[string]struct{})
	for _, item := range items {
		if item.Enabled {
			continue
		}
		family := strings.ToUpper(strings.TrimSpace(item.Family))
		if family == "" {
			continue
		}
		hidden[family] = struct{}{}
	}
	return hidden, nil
}

func hasBaseline(baselines []string, want string) bool {
	for _, baseline := range baselines {
		if baseline == want {
			return true
		}
	}
	return false
}

func controlFamily(controlID string) string {
	return controlid.Family(controlID)
}

func normalizeControl(control *Control) {
	control.SourceKey = strings.TrimSpace(control.SourceKey)
	control.ID = controlid.Normalize(control.ID)
	control.Type = strings.TrimSpace(control.Type)
	if control.Type == "" {
		if strings.Contains(control.ID, "(") {
			control.Type = "Control Enhancement"
		} else {
			control.Type = "Control"
		}
	}
	control.Name = strings.TrimSpace(control.Name)
	control.Family = controlFamily(control.ID)
	control.Baselines = uniqueSorted(control.Baselines)
	control.Threats = uniqueSorted(control.Threats)
	control.MappingBaselines = normalizeBaselines(control.MappingBaselines)
	control.Baselines = deriveBaselineMembership(control.MappingBaselines)
	control.Confidentiality = normalizeFlag(control.Confidentiality)
	control.Integrity = normalizeFlag(control.Integrity)
	control.Availability = normalizeFlag(control.Availability)
	control.Justification = strings.TrimSpace(control.Justification)
	control.PotentiallyCommonInheritable = strings.TrimSpace(control.PotentiallyCommonInheritable)
	control.Requirements = strings.TrimSpace(control.Requirements)
	control.Discussion = strings.TrimSpace(control.Discussion)
	control.RelatedControls = uniqueSorted(control.RelatedControls)
	if len(control.Appendix) == 0 {
		control.Appendix = nil
	}
}

func normalizeBaselines(b Baselines) Baselines {
	b.High = uniqueSorted(b.High)
	b.Low = uniqueSorted(b.Low)
	b.Moderate = uniqueSorted(b.Moderate)
	b.Privacy = uniqueSorted(b.Privacy)
	return b
}

func deriveBaselineMembership(b Baselines) []string {
	result := make([]string, 0, 4)
	if len(b.Low) > 0 {
		result = append(result, "Low")
	}
	if len(b.Moderate) > 0 {
		result = append(result, "Moderate")
	}
	if len(b.High) > 0 {
		result = append(result, "High")
	}
	if len(b.Privacy) > 0 {
		result = append(result, "Privacy")
	}
	return result
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	controlid.Sort(out)
	return out
}

func boolFlag(value bool) string {
	if value {
		return "Yes"
	}
	return ""
}

func flagsToBaselines(flags controlfile.BaselineFlags, controlID string) Baselines {
	b := Baselines{}
	if flags.High {
		b.High = []string{controlID}
	}
	if flags.Low {
		b.Low = []string{controlID}
	}
	if flags.Moderate {
		b.Moderate = []string{controlID}
	}
	if flags.Privacy {
		b.Privacy = []string{controlID}
	}
	return b
}

func baselinesToFlags(b Baselines) controlfile.BaselineFlags {
	return controlfile.BaselineFlags{
		High:     len(b.High) > 0,
		Low:      len(b.Low) > 0,
		Moderate: len(b.Moderate) > 0,
		Privacy:  len(b.Privacy) > 0,
	}
}

func normalizeFlag(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "Yes"
}

func controlFamilyName(family string) string {
	switch strings.TrimSpace(strings.ToUpper(family)) {
	case "AC":
		return "Access Control"
	case "AT":
		return "Awareness and Training"
	case "AU":
		return "Audit and Accountability"
	case "CA":
		return "Assessment, Authorization, and Monitoring"
	case "CM":
		return "Configuration Management"
	case "CP":
		return "Contingency Planning"
	case "IA":
		return "Identification and Authentication"
	case "IR":
		return "Incident Response"
	case "MA":
		return "Maintenance"
	case "MP":
		return "Media Protection"
	case "PE":
		return "Physical and Environmental Protection"
	case "PL":
		return "Planning"
	case "PM":
		return "Program Management"
	case "PS":
		return "Personnel Security"
	case "PT":
		return "Personally Identifiable Information Processing and Transparency"
	case "RA":
		return "Risk Assessment"
	case "SA":
		return "System and Services Acquisition"
	case "SC":
		return "System and Communications Protection"
	case "SI":
		return "System and Information Integrity"
	case "SR":
		return "Supply Chain Risk Management"
	default:
		return "Unknown family"
	}
}

func sortRecordKeys(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		left, leftErr := strconv.Atoi(keys[i])
		right, rightErr := strconv.Atoi(keys[j])
		if leftErr == nil && rightErr == nil {
			return left < right
		}
		return keys[i] < keys[j]
	})
}

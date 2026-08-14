package regcoverage

import (
	"sort"
	"strings"
)

// memRepo is an in-memory Repository. The service's interesting logic —
// grounding, version arithmetic, revision replacement — is worth exercising
// without a database in the way, and the SQL implementation is exercised
// separately by the app's integration path.
type memRepo struct {
	nextID      int64
	regulations map[int64]*Regulation
	texts       map[int64]string
	sources     map[int64]Source
	sections    map[int64][]Section
	findings    map[int64][]Finding
	versions    map[int64][]Version
	chat        map[int64][]ChatTurn
}

func newMemRepo() *memRepo {
	return &memRepo{
		regulations: map[int64]*Regulation{},
		texts:       map[int64]string{},
		sources:     map[int64]Source{},
		sections:    map[int64][]Section{},
		findings:    map[int64][]Finding{},
		versions:    map[int64][]Version{},
		chat:        map[int64][]ChatTurn{},
	}
}

func (m *memRepo) id() int64 {
	m.nextID++
	return m.nextID
}

func (m *memRepo) CreateRegulation(reg Regulation, text string, sections []Section, source Source) (Regulation, error) {
	reg.ID = m.id()
	reg.SectionCount = len(sections)
	reg.TextChars = len(text)

	stored := make([]Section, 0, len(sections))
	for _, s := range sections {
		s.ID = m.id()
		s.RegulationID = reg.ID
		stored = append(stored, s)
	}

	copied := reg
	m.regulations[reg.ID] = &copied
	m.texts[reg.ID] = text
	m.sources[reg.ID] = source
	m.sections[reg.ID] = stored
	return copied, nil
}

func (m *memRepo) GetRegulation(id int64) (Regulation, error) {
	reg, ok := m.regulations[id]
	if !ok {
		return Regulation{}, notFound("regulation")
	}
	out := *reg
	out.SectionCount = len(m.sections[id])
	out.LatestVersion = 0
	for _, v := range m.versions[id] {
		if v.Number > out.LatestVersion {
			out.LatestVersion = v.Number
		}
	}
	return out, nil
}

func (m *memRepo) RegulationBySHA(sha string) (Regulation, error) {
	for id, reg := range m.regulations {
		if reg.SHA256 == sha {
			return m.GetRegulation(id)
		}
	}
	return Regulation{}, notFound("regulation")
}

func (m *memRepo) ListRegulations() ([]Regulation, error) {
	ids := make([]int64, 0, len(m.regulations))
	for id := range m.regulations {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })

	out := make([]Regulation, 0, len(ids))
	for _, id := range ids {
		reg, _ := m.GetRegulation(id)
		out = append(out, reg)
	}
	return out, nil
}

func (m *memRepo) DeleteRegulation(id int64) error {
	if _, ok := m.regulations[id]; !ok {
		return notFound("regulation")
	}
	delete(m.regulations, id)
	delete(m.texts, id)
	delete(m.sources, id)
	delete(m.sections, id)
	delete(m.findings, id)
	delete(m.versions, id)
	delete(m.chat, id)
	return nil
}

func (m *memRepo) SetStatus(id int64, status, detail, analyzedAt string) error {
	reg, ok := m.regulations[id]
	if !ok {
		return notFound("regulation")
	}
	reg.Status = status
	reg.StatusDetail = detail
	if analyzedAt != "" {
		reg.AnalyzedAt = analyzedAt
	}
	return nil
}

func (m *memRepo) RegulationText(id int64) (string, error) {
	if _, ok := m.regulations[id]; !ok {
		return "", notFound("regulation")
	}
	return m.texts[id], nil
}

func (m *memRepo) GetSource(id int64) (Source, error) {
	src, ok := m.sources[id]
	if !ok {
		return Source{}, notFound("original document")
	}
	return src, nil
}

func (m *memRepo) ListSections(regulationID int64) ([]Section, error) {
	return append([]Section(nil), m.sections[regulationID]...), nil
}

func (m *memRepo) GetSection(id int64) (Section, error) {
	for _, sections := range m.sections {
		for _, s := range sections {
			if s.ID == id {
				return s, nil
			}
		}
	}
	return Section{}, notFound("section")
}

func (m *memRepo) SectionByRef(regulationID int64, ref string) (Section, error) {
	for _, s := range m.sections[regulationID] {
		if strings.EqualFold(s.Ref, ref) {
			return s, nil
		}
	}
	return Section{}, notFound("section " + ref)
}

func (m *memRepo) ReplaceFindings(regulationID int64, findings []Finding) error {
	stored := make([]Finding, 0, len(findings))
	for _, f := range findings {
		f.ID = m.id()
		stored = append(stored, f)
	}
	m.findings[regulationID] = stored
	return nil
}

func (m *memRepo) SaveFinding(finding Finding) (Finding, error) {
	current := m.findings[finding.RegulationID]
	revision := 0
	for _, f := range current {
		if f.SectionID == finding.SectionID && f.Revision >= revision {
			revision = f.Revision + 1
		}
	}
	finding.ID = m.id()
	finding.Revision = revision
	m.findings[finding.RegulationID] = append(current, finding)
	return finding, nil
}

// CurrentFindings mirrors the SQL: the highest revision per section, in section
// order.
func (m *memRepo) CurrentFindings(regulationID int64) ([]Finding, error) {
	position := map[int64]int{}
	refs := map[int64]string{}
	for _, s := range m.sections[regulationID] {
		position[s.ID] = s.Position
		refs[s.ID] = s.Ref
	}

	best := map[int64]Finding{}
	for _, f := range m.findings[regulationID] {
		if existing, ok := best[f.SectionID]; ok && existing.Revision > f.Revision {
			continue
		}
		f.SectionRef = refs[f.SectionID]
		best[f.SectionID] = f
	}

	out := make([]Finding, 0, len(best))
	for _, f := range best {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return position[out[i].SectionID] < position[out[j].SectionID] })
	return out, nil
}

func (m *memRepo) CreateVersion(v Version) (Version, error) {
	v.ID = m.id()
	v.Number = len(m.versions[v.RegulationID]) + 1
	m.versions[v.RegulationID] = append(m.versions[v.RegulationID], v)
	return v, nil
}

func (m *memRepo) SaveVersionSnapshot(versionID int64, snapshot string) error {
	for id, versions := range m.versions {
		for i := range versions {
			if versions[i].ID == versionID {
				m.versions[id][i].Snapshot = snapshot
				return nil
			}
		}
	}
	return notFound("report version")
}

func (m *memRepo) GetVersion(regulationID int64, number int) (Version, error) {
	for _, v := range m.versions[regulationID] {
		if v.Number == number {
			return v, nil
		}
	}
	return Version{}, notFound("version")
}

func (m *memRepo) LatestVersion(regulationID int64) (Version, error) {
	versions := m.versions[regulationID]
	if len(versions) == 0 {
		return Version{}, notFound("report version")
	}
	return versions[len(versions)-1], nil
}

func (m *memRepo) ListVersions(regulationID int64) ([]Version, error) {
	out := append([]Version(nil), m.versions[regulationID]...)
	sort.Slice(out, func(i, j int) bool { return out[i].Number > out[j].Number })
	return out, nil
}

func (m *memRepo) AppendChat(turn ChatTurn) (ChatTurn, error) {
	turn.ID = m.id()
	m.chat[turn.RegulationID] = append(m.chat[turn.RegulationID], turn)
	return turn, nil
}

func (m *memRepo) ListChat(regulationID int64) ([]ChatTurn, error) {
	return append([]ChatTurn(nil), m.chat[regulationID]...), nil
}

func (m *memRepo) ClearChat(regulationID int64) error {
	delete(m.chat, regulationID)
	return nil
}

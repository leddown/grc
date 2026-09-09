package regcoverage

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"grc/internal/db"
)

// Repository is the persistence surface. It is an interface so the service can
// be tested without a database: the interesting logic here — grounding checks,
// version arithmetic, revision replacement — is the part that must not be
// exercised only through SQL.
type Repository interface {
	CreateRegulation(reg Regulation, text string, sections []Section) (Regulation, error)
	GetRegulation(id int64) (Regulation, error)
	RegulationBySHA(sha string) (Regulation, error)
	RegulationByLibraryID(libraryDocID int64) (Regulation, error)
	ListRegulations() ([]Regulation, error)
	DeleteRegulation(id int64) error
	SetStatus(id int64, status, detail, analyzedAt string) error
	RegulationText(id int64) (string, error)

	ListSections(regulationID int64) ([]Section, error)
	GetSection(id int64) (Section, error)
	SectionByRef(regulationID int64, ref string) (Section, error)

	// ReplaceFindings writes the findings of a full analysis run, discarding
	// anything from a previous run.
	ReplaceFindings(regulationID int64, findings []Finding) error
	// SaveFinding writes one revised finding, superseding the current one for
	// that section.
	SaveFinding(finding Finding) (Finding, error)
	// CurrentFindings returns the highest-revision finding per section.
	CurrentFindings(regulationID int64) ([]Finding, error)

	CreateVersion(v Version) (Version, error)
	// SaveVersionSnapshot fills in a version's snapshot, which can only be
	// rendered once the version has a number.
	SaveVersionSnapshot(versionID int64, snapshot string) error
	GetVersion(regulationID int64, number int) (Version, error)
	LatestVersion(regulationID int64) (Version, error)
	ListVersions(regulationID int64) ([]Version, error)

	AppendChat(turn ChatTurn) (ChatTurn, error)
	ListChat(regulationID int64) ([]ChatTurn, error)
	ClearChat(regulationID int64) error
}

type SQLiteRepository struct {
	conn *db.Conn
}

func NewSQLiteRepository(conn *db.Conn) *SQLiteRepository {
	return &SQLiteRepository{conn: conn}
}

// ---- regulations ----

// CreateRegulation writes the regulation, its extracted text and its sections
// in one transaction. A regulation row with no sections would present as an
// analysable document that silently has nothing to analyse.
//
// The file itself is not written: it lives in the agent's library on the
// Wintermute server, and a second copy here would be a second thing to keep in
// step with a document that can be re-read there when a better tool arrives.
func (r *SQLiteRepository) CreateRegulation(reg Regulation, text string, sections []Section) (Regulation, error) {
	tx, err := r.conn.Begin()
	if err != nil {
		return Regulation{}, err
	}
	defer func() { _ = tx.Rollback() }()

	const insertReg = `INSERT INTO reg_coverage_regulations
		(title, framework, framework_name, source_ref, detected, library_doc_id, filename,
		 media_type, sha256, byte_size, extract_method, extract_notes, body_text, status,
		 status_detail, uploaded_by, created_at, analyzed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '')`

	id, err := tx.Insert(insertReg,
		reg.Title, reg.Framework, reg.FrameworkName, reg.SourceRef, boolToInt(reg.Detected),
		reg.LibraryDocID, reg.Filename, reg.MediaType, reg.SHA256, reg.ByteSize, reg.ExtractMethod,
		strings.Join(reg.ExtractNotes, "\n"), text, reg.Status, reg.StatusDetail,
		reg.ImportedBy, reg.CreatedAt)
	if err != nil {
		return Regulation{}, fmt.Errorf("insert regulation: %w", err)
	}
	reg.ID = id

	stmt, err := tx.Prepare(`INSERT INTO reg_coverage_sections
		(regulation_id, ref, label, title, category, body, position, confidence)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return Regulation{}, err
	}
	defer func() { _ = stmt.Close() }()

	for _, s := range sections {
		if _, err := stmt.Exec(id, s.Ref, s.Label, s.Title, s.Category, s.Body, s.Position, s.Confidence); err != nil {
			return Regulation{}, fmt.Errorf("insert section %s: %w", s.Ref, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return Regulation{}, err
	}
	reg.SectionCount = len(sections)
	reg.TextChars = len(text)
	return reg, nil
}

// regulationColumns deliberately omits body_text: a listing does not read it,
// and it is large.
const regulationColumns = `g.id, g.title, g.framework, g.framework_name, g.source_ref, g.detected,
	g.library_doc_id, g.filename, g.media_type, g.sha256, g.byte_size, g.extract_method,
	g.extract_notes, LENGTH(g.body_text), g.status, g.status_detail, g.uploaded_by,
	g.created_at, g.analyzed_at,
	(SELECT COUNT(*) FROM reg_coverage_sections s WHERE s.regulation_id = g.id),
	(SELECT COALESCE(MAX(v.number), 0) FROM reg_coverage_versions v WHERE v.regulation_id = g.id)`

func scanRegulation(scan func(...any) error) (Regulation, error) {
	var reg Regulation
	var detected int
	var notes string
	err := scan(&reg.ID, &reg.Title, &reg.Framework, &reg.FrameworkName, &reg.SourceRef, &detected,
		&reg.LibraryDocID, &reg.Filename, &reg.MediaType, &reg.SHA256, &reg.ByteSize,
		&reg.ExtractMethod, &notes, &reg.TextChars, &reg.Status, &reg.StatusDetail,
		&reg.ImportedBy, &reg.CreatedAt, &reg.AnalyzedAt, &reg.SectionCount, &reg.LatestVersion)
	reg.Detected = detected != 0
	if notes != "" {
		reg.ExtractNotes = strings.Split(notes, "\n")
	}
	return reg, err
}

func (r *SQLiteRepository) GetRegulation(id int64) (Regulation, error) {
	row := r.conn.QueryRow(`SELECT `+regulationColumns+` FROM reg_coverage_regulations g WHERE g.id = ?`, id)
	reg, err := scanRegulation(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Regulation{}, notFound("regulation")
	}
	return reg, err
}

// RegulationBySHA backs duplicate detection on the extracted text.
func (r *SQLiteRepository) RegulationBySHA(sha string) (Regulation, error) {
	row := r.conn.QueryRow(`SELECT `+regulationColumns+` FROM reg_coverage_regulations g WHERE g.sha256 = ?`, sha)
	reg, err := scanRegulation(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Regulation{}, notFound("regulation")
	}
	return reg, err
}

// RegulationByLibraryID recognises a library document that is already imported,
// before it is read again.
func (r *SQLiteRepository) RegulationByLibraryID(libraryDocID int64) (Regulation, error) {
	row := r.conn.QueryRow(`SELECT `+regulationColumns+
		` FROM reg_coverage_regulations g WHERE g.library_doc_id = ? AND g.library_doc_id != 0`,
		libraryDocID)
	reg, err := scanRegulation(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Regulation{}, notFound("regulation")
	}
	return reg, err
}

func (r *SQLiteRepository) ListRegulations() ([]Regulation, error) {
	rows, err := r.conn.Query(`SELECT ` + regulationColumns +
		` FROM reg_coverage_regulations g ORDER BY g.id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Regulation{}
	for rows.Next() {
		reg, err := scanRegulation(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, reg)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) DeleteRegulation(id int64) error {
	// The child rows cascade in both dialects, but only when foreign keys are
	// enforced; deleting them explicitly means a build without the pragma does
	// not leave orphans behind.
	tx, err := r.conn.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range []string{
		`DELETE FROM reg_coverage_mappings WHERE finding_id IN
			(SELECT id FROM reg_coverage_findings WHERE regulation_id = ?)`,
		`DELETE FROM reg_coverage_findings WHERE regulation_id = ?`,
		`DELETE FROM reg_coverage_sections WHERE regulation_id = ?`,
		`DELETE FROM reg_coverage_versions WHERE regulation_id = ?`,
		`DELETE FROM reg_coverage_chat WHERE regulation_id = ?`,
		`DELETE FROM reg_coverage_sources WHERE regulation_id = ?`,
	} {
		if _, err := tx.Exec(stmt, id); err != nil {
			return err
		}
	}
	res, err := tx.Exec(`DELETE FROM reg_coverage_regulations WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return notFound("regulation")
	}
	return tx.Commit()
}

func (r *SQLiteRepository) SetStatus(id int64, status, detail, analyzedAt string) error {
	_, err := r.conn.Exec(
		`UPDATE reg_coverage_regulations SET status = ?, status_detail = ?,
			analyzed_at = CASE WHEN ? = '' THEN analyzed_at ELSE ? END WHERE id = ?`,
		status, detail, analyzedAt, analyzedAt, id)
	return err
}

func (r *SQLiteRepository) RegulationText(id int64) (string, error) {
	var text string
	err := r.conn.QueryRow(`SELECT body_text FROM reg_coverage_regulations WHERE id = ?`, id).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", notFound("regulation")
	}
	return text, err
}

// ---- sections ----

const sectionColumns = `id, regulation_id, ref, label, title, category, body, position, confidence`

func scanSection(scan func(...any) error) (Section, error) {
	var s Section
	err := scan(&s.ID, &s.RegulationID, &s.Ref, &s.Label, &s.Title, &s.Category, &s.Body, &s.Position, &s.Confidence)
	return s, err
}

func (r *SQLiteRepository) ListSections(regulationID int64) ([]Section, error) {
	rows, err := r.conn.Query(`SELECT `+sectionColumns+
		` FROM reg_coverage_sections WHERE regulation_id = ? ORDER BY position`, regulationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Section{}
	for rows.Next() {
		s, err := scanSection(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) GetSection(id int64) (Section, error) {
	row := r.conn.QueryRow(`SELECT `+sectionColumns+` FROM reg_coverage_sections WHERE id = ?`, id)
	s, err := scanSection(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Section{}, notFound("section")
	}
	return s, err
}

func (r *SQLiteRepository) SectionByRef(regulationID int64, ref string) (Section, error) {
	row := r.conn.QueryRow(`SELECT `+sectionColumns+
		` FROM reg_coverage_sections WHERE regulation_id = ? AND UPPER(ref) = UPPER(?)`, regulationID, ref)
	s, err := scanSection(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Section{}, notFound("section " + ref)
	}
	return s, err
}

// ---- findings ----

func (r *SQLiteRepository) ReplaceFindings(regulationID int64, findings []Finding) error {
	tx, err := r.conn.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM reg_coverage_mappings WHERE finding_id IN
		(SELECT id FROM reg_coverage_findings WHERE regulation_id = ?)`, regulationID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM reg_coverage_findings WHERE regulation_id = ?`, regulationID); err != nil {
		return err
	}
	for _, f := range findings {
		if err := insertFinding(tx, f); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *SQLiteRepository) SaveFinding(finding Finding) (Finding, error) {
	tx, err := r.conn.Begin()
	if err != nil {
		return Finding{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var current sql.NullInt64
	if err := tx.QueryRow(
		`SELECT MAX(revision) FROM reg_coverage_findings WHERE section_id = ?`,
		finding.SectionID).Scan(&current); err != nil {
		return Finding{}, err
	}
	finding.Revision = int(current.Int64) + 1

	if err := insertFinding(tx, finding); err != nil {
		return Finding{}, err
	}
	if err := tx.Commit(); err != nil {
		return Finding{}, err
	}
	return finding, nil
}

// execer is the shared write surface of db.Conn and db.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Insert(query string, args ...any) (int64, error)
}

func insertFinding(tx execer, f Finding) error {
	id, err := tx.Insert(`INSERT INTO reg_coverage_findings
		(regulation_id, section_id, relevant, requirement, commentary, gaps, quote,
		 grounded, confidence, model, prompt_hash, revision, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.RegulationID, f.SectionID, boolToInt(f.Relevant), f.Requirement, f.Commentary,
		f.Gaps, f.Quote, boolToInt(f.Grounded), f.Confidence, f.Model, f.PromptHash,
		f.Revision, f.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert finding: %w", err)
	}
	for _, m := range f.Mappings {
		if _, err := tx.Exec(`INSERT INTO reg_coverage_mappings
			(finding_id, kind, ref, title, rationale, confidence, source, known)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, m.Kind, m.Ref, m.Title, m.Rationale, m.Confidence, m.Source, boolToInt(m.Known)); err != nil {
			return fmt.Errorf("insert mapping %s: %w", m.Ref, err)
		}
	}
	return nil
}

// CurrentFindings returns one finding per section: the highest revision, which
// is the one a revision cycle leaves in force.
func (r *SQLiteRepository) CurrentFindings(regulationID int64) ([]Finding, error) {
	rows, err := r.conn.Query(`SELECT f.id, f.regulation_id, f.section_id, s.ref, f.relevant,
		f.requirement, f.commentary, f.gaps, f.quote, f.grounded, f.confidence, f.model,
		f.prompt_hash, f.revision, f.created_at
		FROM reg_coverage_findings f
		JOIN reg_coverage_sections s ON s.id = f.section_id
		WHERE f.regulation_id = ?
		  AND f.revision = (SELECT MAX(f2.revision) FROM reg_coverage_findings f2 WHERE f2.section_id = f.section_id)
		ORDER BY s.position`, regulationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Finding{}
	byID := map[int64]int{}
	for rows.Next() {
		var f Finding
		var relevant, grounded int
		if err := rows.Scan(&f.ID, &f.RegulationID, &f.SectionID, &f.SectionRef, &relevant,
			&f.Requirement, &f.Commentary, &f.Gaps, &f.Quote, &grounded, &f.Confidence,
			&f.Model, &f.PromptHash, &f.Revision, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.Relevant = relevant != 0
		f.Grounded = grounded != 0
		byID[f.ID] = len(out)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	mrows, err := r.conn.Query(`SELECT m.id, m.finding_id, m.kind, m.ref, m.title, m.rationale,
		m.confidence, m.source, m.known
		FROM reg_coverage_mappings m
		JOIN reg_coverage_findings f ON f.id = m.finding_id
		WHERE f.regulation_id = ?
		ORDER BY m.kind, m.id`, regulationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mrows.Close() }()

	for mrows.Next() {
		var m Mapping
		var known int
		if err := mrows.Scan(&m.ID, &m.FindingID, &m.Kind, &m.Ref, &m.Title, &m.Rationale,
			&m.Confidence, &m.Source, &known); err != nil {
			return nil, err
		}
		m.Known = known != 0
		if idx, ok := byID[m.FindingID]; ok {
			out[idx].Mappings = append(out[idx].Mappings, m)
		}
	}
	return out, mrows.Err()
}

// ---- versions ----

func (r *SQLiteRepository) CreateVersion(v Version) (Version, error) {
	tx, err := r.conn.Begin()
	if err != nil {
		return Version{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var current sql.NullInt64
	if err := tx.QueryRow(
		`SELECT MAX(number) FROM reg_coverage_versions WHERE regulation_id = ?`,
		v.RegulationID).Scan(&current); err != nil {
		return Version{}, err
	}
	v.Number = int(current.Int64) + 1

	id, err := tx.Insert(`INSERT INTO reg_coverage_versions
		(regulation_id, number, summary, note, snapshot, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		v.RegulationID, v.Number, v.Summary, v.Note, v.Snapshot, v.CreatedAt, v.CreatedBy)
	if err != nil {
		return Version{}, fmt.Errorf("insert version: %w", err)
	}
	v.ID = id
	if err := tx.Commit(); err != nil {
		return Version{}, err
	}
	return v, nil
}

func (r *SQLiteRepository) SaveVersionSnapshot(versionID int64, snapshot string) error {
	res, err := r.conn.Exec(`UPDATE reg_coverage_versions SET snapshot = ? WHERE id = ?`, snapshot, versionID)
	if err != nil {
		return fmt.Errorf("save version snapshot: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return notFound("report version")
	}
	return nil
}

const versionColumns = `id, regulation_id, number, summary, note, snapshot, created_at, created_by`

func scanVersion(scan func(...any) error) (Version, error) {
	var v Version
	err := scan(&v.ID, &v.RegulationID, &v.Number, &v.Summary, &v.Note, &v.Snapshot, &v.CreatedAt, &v.CreatedBy)
	return v, err
}

func (r *SQLiteRepository) GetVersion(regulationID int64, number int) (Version, error) {
	row := r.conn.QueryRow(`SELECT `+versionColumns+
		` FROM reg_coverage_versions WHERE regulation_id = ? AND number = ?`, regulationID, number)
	v, err := scanVersion(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, notFound(fmt.Sprintf("version %d", number))
	}
	return v, err
}

func (r *SQLiteRepository) LatestVersion(regulationID int64) (Version, error) {
	row := r.conn.QueryRow(`SELECT `+versionColumns+
		` FROM reg_coverage_versions WHERE regulation_id = ? ORDER BY number DESC LIMIT 1`, regulationID)
	v, err := scanVersion(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, notFound("report version")
	}
	return v, err
}

func (r *SQLiteRepository) ListVersions(regulationID int64) ([]Version, error) {
	rows, err := r.conn.Query(`SELECT `+versionColumns+
		` FROM reg_coverage_versions WHERE regulation_id = ? ORDER BY number DESC`, regulationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Version{}
	for rows.Next() {
		v, err := scanVersion(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---- chat ----

func (r *SQLiteRepository) AppendChat(turn ChatTurn) (ChatTurn, error) {
	id, err := r.conn.Insert(`INSERT INTO reg_coverage_chat
		(regulation_id, version, role, content, model, actor, session_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		turn.RegulationID, turn.Version, turn.Role, turn.Content, turn.Model,
		turn.Actor, turn.SessionID, turn.CreatedAt)
	if err != nil {
		return ChatTurn{}, fmt.Errorf("append chat turn: %w", err)
	}
	turn.ID = id
	return turn, nil
}

func (r *SQLiteRepository) ListChat(regulationID int64) ([]ChatTurn, error) {
	rows, err := r.conn.Query(`SELECT id, regulation_id, version, role, content, model,
		actor, session_id, created_at FROM reg_coverage_chat
		WHERE regulation_id = ? ORDER BY id`, regulationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []ChatTurn{}
	for rows.Next() {
		var t ChatTurn
		if err := rows.Scan(&t.ID, &t.RegulationID, &t.Version, &t.Role, &t.Content,
			&t.Model, &t.Actor, &t.SessionID, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) ClearChat(regulationID int64) error {
	_, err := r.conn.Exec(`DELETE FROM reg_coverage_chat WHERE regulation_id = ?`, regulationID)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

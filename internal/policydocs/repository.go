package policydocs

import (
	"database/sql"
	"encoding/json"
	"strings"

	"carelockconsulting/internal/db"
)

// Repository is the policy-document persistence boundary.
type Repository interface {
	ListDocuments(f Filter) ([]Document, error)
	GetDocument(id int64) (Document, error)
	CreateDocument(d Document) (Document, error)
	UpdateDocument(id int64, d Document) (Document, error)
	DeleteDocument(id int64) error
	SetStatus(id int64, status, nextReview, updatedAt string) error

	ListSections(documentID int64) ([]Section, error)
	GetSection(id int64) (Section, error)
	CreateSection(s Section) (Section, error)
	UpdateSection(id int64, s Section) (Section, error)
	DeleteSection(id int64) error
	ReorderSections(documentID int64, orderedIDs []int64, updatedAt string) error

	ListVersions(documentID int64) ([]Version, error)
	CreateVersion(v Version) (Version, error)
	NextVersionNumber(documentID int64) (int, error)

	ListControlRefsForSection(sectionID int64) ([]ControlRef, error)
	ListControlRefsForDocument(documentID int64) ([]ControlRef, error)
	GetControlRef(id int64) (ControlRef, error)
	CreateControlRef(ref ControlRef) (ControlRef, error)
	DeleteControlRef(id int64) error
	ControlRefExists(sectionID int64, controlID string) (bool, error)
	LookupControl(controlID string) (ControlOption, error)
	SearchControls(query string, limit int) ([]ControlOption, error)
	Coverage(f CoverageFilter) (CoverageReport, error)
}

type SQLiteRepository struct {
	db *db.Conn
}

func NewSQLiteRepository(conn *db.Conn) *SQLiteRepository {
	return &SQLiteRepository{db: conn}
}

// documentColumns is shared by the list and get queries so the two can never
// drift out of sync with scanDocument.
const documentColumns = `
	d.id, d.client_id, d.client_name, d.doc_type, d.reference, d.title, d.status, d.owner_role,
	d.approver, d.classification, d.frameworks_json, d.effective_date,
	d.review_cadence_months, d.next_review_date, d.parent_document_id,
	d.summary, d.author, d.created_at, d.updated_at,
	COALESCE((SELECT p.title FROM policy_documents p WHERE p.id = d.parent_document_id), '') AS parent_title,
	(SELECT COUNT(*) FROM policy_sections s WHERE s.document_id = d.id) AS section_count,
	(SELECT COUNT(*) FROM policy_versions v WHERE v.document_id = d.id) AS version_count,
	COALESCE((SELECT v.version_label FROM policy_versions v WHERE v.document_id = d.id
		ORDER BY v.id DESC LIMIT 1), '') AS latest_label,
	(SELECT COUNT(*) FROM policy_documents ch WHERE ch.parent_document_id = d.id) AS child_count`

func (r *SQLiteRepository) ListDocuments(f Filter) ([]Document, error) {
	query := `SELECT ` + documentColumns + ` FROM policy_documents d WHERE 1=1`
	args := make([]any, 0, 6)

	if f.ClientID > 0 {
		query += ` AND d.client_id = ?`
		args = append(args, f.ClientID)
	}
	if f.DocType != "" {
		query += ` AND d.doc_type = ?`
		args = append(args, f.DocType)
	}
	if f.Status != "" {
		query += ` AND d.status = ?`
		args = append(args, f.Status)
	}
	if f.Framework != "" {
		// frameworks_json is a JSON array of names; a LIKE on the quoted name
		// is exact enough because names cannot contain a quote.
		query += ` AND d.frameworks_json LIKE ?`
		args = append(args, `%"`+f.Framework+`"%`)
	}
	if f.Search != "" {
		query += ` AND (UPPER(d.title) LIKE ? OR UPPER(d.reference) LIKE ? OR UPPER(d.summary) LIKE ?)`
		like := "%" + strings.ToUpper(f.Search) + "%"
		args = append(args, like, like, like)
	}
	if f.DueReview {
		query += ` AND d.next_review_date <> '' AND d.next_review_date <= ? AND d.status <> ?`
		args = append(args, today(), StatusRetired)
	}
	query += ` ORDER BY d.doc_type ASC, LOWER(d.reference) ASC, LOWER(d.title) ASC`

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Document, 0)
	for rows.Next() {
		item, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) GetDocument(id int64) (Document, error) {
	row := r.db.QueryRow(`SELECT `+documentColumns+` FROM policy_documents d WHERE d.id = ?`, id)
	return scanDocument(row)
}

func (r *SQLiteRepository) CreateDocument(d Document) (Document, error) {
	id, err := r.db.Insert(`INSERT INTO policy_documents
		(client_id, client_name, doc_type, reference, title, status, owner_role, approver,
		 classification, frameworks_json, effective_date, review_cadence_months,
		 next_review_date, parent_document_id, summary, author, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ClientID, d.ClientName, d.DocType, d.Reference, d.Title, d.Status, d.OwnerRole, d.Approver,
		d.Classification, encodeFrameworks(d.Frameworks), d.EffectiveDate, d.ReviewCadenceMonths,
		d.NextReviewDate, d.ParentDocumentID, d.Summary, d.Author, d.CreatedAt, d.UpdatedAt)
	if err != nil {
		return Document{}, err
	}
	return r.GetDocument(id)
}

func (r *SQLiteRepository) UpdateDocument(id int64, d Document) (Document, error) {
	res, err := r.db.Exec(`UPDATE policy_documents SET
		client_id = ?, client_name = ?, doc_type = ?, reference = ?, title = ?, owner_role = ?,
		approver = ?, classification = ?, frameworks_json = ?, effective_date = ?,
		review_cadence_months = ?, next_review_date = ?, parent_document_id = ?,
		summary = ?, updated_at = ?
		WHERE id = ?`,
		d.ClientID, d.ClientName, d.DocType, d.Reference, d.Title, d.OwnerRole, d.Approver,
		d.Classification, encodeFrameworks(d.Frameworks), d.EffectiveDate,
		d.ReviewCadenceMonths, d.NextReviewDate, d.ParentDocumentID,
		d.Summary, d.UpdatedAt, id)
	if err != nil {
		return Document{}, err
	}
	if err := requireAffected(res); err != nil {
		return Document{}, err
	}
	return r.GetDocument(id)
}

func (r *SQLiteRepository) DeleteDocument(id int64) error {
	res, err := r.db.Exec(`DELETE FROM policy_documents WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (r *SQLiteRepository) SetStatus(id int64, status, nextReview, updatedAt string) error {
	res, err := r.db.Exec(`UPDATE policy_documents SET status = ?, next_review_date = ?, updated_at = ? WHERE id = ?`,
		status, nextReview, updatedAt, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ---- Sections ----

const sectionColumns = `id, document_id, ordinal, heading, body, section_kind,
	provenance, provenance_detail, updated_at`

func (r *SQLiteRepository) ListSections(documentID int64) ([]Section, error) {
	rows, err := r.db.Query(`SELECT `+sectionColumns+`
		FROM policy_sections WHERE document_id = ? ORDER BY ordinal ASC, id ASC`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Section, 0)
	for rows.Next() {
		item, err := scanSection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) GetSection(id int64) (Section, error) {
	return scanSection(r.db.QueryRow(`SELECT `+sectionColumns+` FROM policy_sections WHERE id = ?`, id))
}

func (r *SQLiteRepository) CreateSection(s Section) (Section, error) {
	id, err := r.db.Insert(`INSERT INTO policy_sections
		(document_id, ordinal, heading, body, section_kind, provenance, provenance_detail, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.DocumentID, s.Ordinal, s.Heading, s.Body, s.SectionKind, s.Provenance, s.ProvenanceDetail, s.UpdatedAt)
	if err != nil {
		return Section{}, err
	}
	return r.GetSection(id)
}

func (r *SQLiteRepository) UpdateSection(id int64, s Section) (Section, error) {
	res, err := r.db.Exec(`UPDATE policy_sections SET
		ordinal = ?, heading = ?, body = ?, section_kind = ?,
		provenance = ?, provenance_detail = ?, updated_at = ?
		WHERE id = ?`,
		s.Ordinal, s.Heading, s.Body, s.SectionKind, s.Provenance, s.ProvenanceDetail, s.UpdatedAt, id)
	if err != nil {
		return Section{}, err
	}
	if err := requireAffected(res); err != nil {
		return Section{}, err
	}
	return r.GetSection(id)
}

func (r *SQLiteRepository) DeleteSection(id int64) error {
	res, err := r.db.Exec(`DELETE FROM policy_sections WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ReorderSections rewrites the ordinals of a document's sections in one
// transaction. Ordinals are assigned from the caller's order rather than
// swapped pairwise so a partially-applied reorder cannot leave duplicates.
func (r *SQLiteRepository) ReorderSections(documentID int64, orderedIDs []int64, updatedAt string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for i, id := range orderedIDs {
		if _, err := tx.Exec(`UPDATE policy_sections SET ordinal = ?, updated_at = ?
			WHERE id = ? AND document_id = ?`, i, updatedAt, id, documentID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- Versions ----

func (r *SQLiteRepository) ListVersions(documentID int64) ([]Version, error) {
	rows, err := r.db.Query(`SELECT id, document_id, version_label, approved_by,
		approved_at, change_summary, snapshot
		FROM policy_versions WHERE document_id = ? ORDER BY id DESC`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Version, 0)
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.ID, &v.DocumentID, &v.VersionLabel, &v.ApprovedBy,
			&v.ApprovedAt, &v.ChangeSummary, &v.Snapshot); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) CreateVersion(v Version) (Version, error) {
	id, err := r.db.Insert(`INSERT INTO policy_versions
		(document_id, version_label, approved_by, approved_at, change_summary, snapshot)
		VALUES (?, ?, ?, ?, ?, ?)`,
		v.DocumentID, v.VersionLabel, v.ApprovedBy, v.ApprovedAt, v.ChangeSummary, v.Snapshot)
	if err != nil {
		return Version{}, err
	}
	v.ID = id
	return v, nil
}

func (r *SQLiteRepository) NextVersionNumber(documentID int64) (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM policy_versions WHERE document_id = ?`, documentID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count + 1, nil
}

// ---- Scanning helpers ----

type scanner interface {
	Scan(dest ...any) error
}

func scanDocument(s scanner) (Document, error) {
	var d Document
	var frameworksJSON string
	err := s.Scan(&d.ID, &d.ClientID, &d.ClientName, &d.DocType, &d.Reference, &d.Title, &d.Status,
		&d.OwnerRole, &d.Approver, &d.Classification, &frameworksJSON, &d.EffectiveDate,
		&d.ReviewCadenceMonths, &d.NextReviewDate, &d.ParentDocumentID, &d.Summary,
		&d.Author, &d.CreatedAt, &d.UpdatedAt, &d.ParentTitle, &d.SectionCount,
		&d.VersionCount, &d.LatestLabel, &d.ChildCount)
	if err != nil {
		return Document{}, err
	}
	d.Frameworks = decodeFrameworks(frameworksJSON)
	return d, nil
}

func scanSection(s scanner) (Section, error) {
	var sec Section
	err := s.Scan(&sec.ID, &sec.DocumentID, &sec.Ordinal, &sec.Heading, &sec.Body,
		&sec.SectionKind, &sec.Provenance, &sec.ProvenanceDetail, &sec.UpdatedAt)
	if err != nil {
		return Section{}, err
	}
	return sec, nil
}

func encodeFrameworks(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func decodeFrameworks(raw string) []string {
	out := make([]string, 0)
	if strings.TrimSpace(raw) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return make([]string, 0)
	}
	return out
}

// errNoRows lets the coverage repository signal "not found" with the same
// sentinel the rest of the package maps through mapNotFound.
func errNoRows() error { return sql.ErrNoRows }

func requireAffected(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

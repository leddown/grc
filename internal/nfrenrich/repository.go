package nfrenrich

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"grc/internal/db"
)

// Repository is the persistence surface. It is an interface so the service can
// be tested without a database, which matters here because the interesting
// logic — citation validation, the accept path — is the part that must not be
// exercised only through SQL.
type Repository interface {
	CreateDocument(doc Document, chunks []Chunk) (Document, error)
	GetDocument(id int64) (Document, error)
	DocumentBySHA(sha string) (Document, error)
	DocumentByLibraryID(libraryDocID int64) (Document, error)
	ListDocuments() ([]Document, error)
	DeleteDocument(id int64) error
	ListChunks(documentID int64) ([]Chunk, error)

	CreateProposal(p Proposal) (Proposal, error)
	GetProposal(id int64) (Proposal, error)
	ListProposals(filter ProposalFilter) ([]Proposal, error)
	SetProposalDecision(id int64, status, decidedAt, decidedBy, note string) error
	SupersedePending(nfrKey, field string, exceptID int64, at string) (int64, error)
}

// ProposalFilter narrows a proposal listing.
type ProposalFilter struct {
	Status     string
	NFRKey     string
	DocumentID int64
}

type SQLiteRepository struct {
	conn *db.Conn
}

func NewSQLiteRepository(conn *db.Conn) *SQLiteRepository {
	return &SQLiteRepository{conn: conn}
}

// CreateDocument writes the document and its chunks in one transaction. A
// half-imported document — rows in nfr_source_documents with no chunks — would
// present in the UI as an analysable source that silently retrieves nothing.
func (r *SQLiteRepository) CreateDocument(doc Document, chunks []Chunk) (Document, error) {
	tx, err := r.conn.Begin()
	if err != nil {
		return Document{}, err
	}
	defer func() { _ = tx.Rollback() }()

	const insertDoc = `INSERT INTO nfr_source_documents
		(title, library_doc_id, url, filename, media_type, sha256, byte_size,
		 extract_via, uploaded_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	id, err := tx.Insert(insertDoc,
		doc.Title, doc.LibraryDocID, doc.URL, doc.Filename, doc.MediaType,
		doc.SHA256, doc.ByteSize, doc.ExtractVia, doc.ImportedBy, doc.CreatedAt)
	if err != nil {
		return Document{}, fmt.Errorf("insert source document: %w", err)
	}
	doc.ID = id

	stmt, err := tx.Prepare(
		`INSERT INTO nfr_source_chunks (document_id, ordinal, heading, body) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return Document{}, err
	}
	defer stmt.Close()

	for _, chunk := range chunks {
		if _, err := stmt.Exec(doc.ID, chunk.Ordinal, chunk.Heading, chunk.Text); err != nil {
			return Document{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Document{}, err
	}
	doc.ChunkCount = len(chunks)
	return doc, nil
}

const documentColumns = `d.id, d.title, d.library_doc_id, d.url, d.filename, d.media_type,
	d.sha256, d.byte_size, d.extract_via, d.uploaded_by, d.created_at,
	(SELECT COUNT(*) FROM nfr_source_chunks c WHERE c.document_id = d.id)`

func scanDocument(scan func(...any) error) (Document, error) {
	var doc Document
	err := scan(&doc.ID, &doc.Title, &doc.LibraryDocID, &doc.URL, &doc.Filename,
		&doc.MediaType, &doc.SHA256, &doc.ByteSize, &doc.ExtractVia, &doc.ImportedBy,
		&doc.CreatedAt, &doc.ChunkCount)
	return doc, err
}

func (r *SQLiteRepository) GetDocument(id int64) (Document, error) {
	row := r.conn.QueryRow(`SELECT `+documentColumns+` FROM nfr_source_documents d WHERE d.id = ?`, id)
	doc, err := scanDocument(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	return doc, err
}

// DocumentBySHA backs duplicate detection on the extracted text.
func (r *SQLiteRepository) DocumentBySHA(sha string) (Document, error) {
	row := r.conn.QueryRow(`SELECT `+documentColumns+` FROM nfr_source_documents d WHERE d.sha256 = ?`, sha)
	doc, err := scanDocument(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	return doc, err
}

// DocumentByLibraryID recognises a library document that is already imported,
// before it is read again.
func (r *SQLiteRepository) DocumentByLibraryID(libraryDocID int64) (Document, error) {
	row := r.conn.QueryRow(`SELECT `+documentColumns+
		` FROM nfr_source_documents d WHERE d.library_doc_id = ? AND d.library_doc_id != 0`, libraryDocID)
	doc, err := scanDocument(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	return doc, err
}

func (r *SQLiteRepository) ListDocuments() ([]Document, error) {
	rows, err := r.conn.Query(`SELECT ` + documentColumns +
		` FROM nfr_source_documents d ORDER BY d.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Document{}
	for rows.Next() {
		doc, err := scanDocument(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

// DeleteDocument removes a document and its chunks.
//
// Proposals are deliberately kept. An accepted proposal is the provenance for
// text now sitting in the catalog, and deleting the source document must not
// erase the record of where that text came from — the reviewer's audit trail
// outlives the corpus. Pending proposals for the document are superseded by the
// service before it calls this.
func (r *SQLiteRepository) DeleteDocument(id int64) error {
	tx, err := r.conn.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM nfr_source_chunks WHERE document_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM nfr_source_documents WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err == nil && affected == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (r *SQLiteRepository) ListChunks(documentID int64) ([]Chunk, error) {
	rows, err := r.conn.Query(
		`SELECT id, document_id, ordinal, heading, body
		 FROM nfr_source_chunks WHERE document_id = ? ORDER BY ordinal`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Chunk{}
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.Ordinal, &c.Heading, &c.Text); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) CreateProposal(p Proposal) (Proposal, error) {
	const insert = `INSERT INTO nfr_enrichment_proposals
		(nfr_key, document_id, field, suggested_text, rationale, confidence,
		 citations_json, status, model, prompt_hash, created_at, decided_at, decided_by, decided_note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', '')`

	id, err := r.conn.Insert(insert,
		p.NFRKey, p.DocumentID, p.Field, p.SuggestedText, p.Rationale, p.Confidence,
		marshalCitations(p.Citations), p.Status, p.Model, p.PromptHash, p.CreatedAt)
	if err != nil {
		return Proposal{}, fmt.Errorf("insert enrichment proposal: %w", err)
	}
	p.ID = id
	return p, nil
}

// proposalColumns joins the NFR summary and document title so the review list
// is one query. Both are LEFT JOINs: a proposal whose document was deleted is
// still reviewable, and must not vanish from the list because of the join.
const proposalColumns = `p.id, p.nfr_key, COALESCE(n.summary, ''), p.document_id,
	COALESCE(d.title, ''), p.field, p.suggested_text, p.rationale, p.confidence,
	p.citations_json, p.status, p.model, p.prompt_hash, p.created_at,
	p.decided_at, p.decided_by, p.decided_note`

const proposalJoins = `FROM nfr_enrichment_proposals p
	LEFT JOIN security_nfrs n ON n.record_key = p.nfr_key
	LEFT JOIN nfr_source_documents d ON d.id = p.document_id`

func scanProposal(scan func(...any) error) (Proposal, error) {
	var (
		p         Proposal
		citations string
	)
	err := scan(&p.ID, &p.NFRKey, &p.NFRSummary, &p.DocumentID, &p.DocumentTitle,
		&p.Field, &p.SuggestedText, &p.Rationale, &p.Confidence, &citations,
		&p.Status, &p.Model, &p.PromptHash, &p.CreatedAt,
		&p.DecidedAt, &p.DecidedBy, &p.DecidedNote)
	if err != nil {
		return Proposal{}, err
	}
	p.Citations = unmarshalCitations(citations)
	return p, nil
}

func (r *SQLiteRepository) GetProposal(id int64) (Proposal, error) {
	row := r.conn.QueryRow(`SELECT `+proposalColumns+` `+proposalJoins+` WHERE p.id = ?`, id)
	p, err := scanProposal(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, ErrNotFound
	}
	return p, err
}

func (r *SQLiteRepository) ListProposals(filter ProposalFilter) ([]Proposal, error) {
	var (
		where []string
		args  []any
	)
	if filter.Status != "" {
		where = append(where, "p.status = ?")
		args = append(args, filter.Status)
	}
	if filter.NFRKey != "" {
		where = append(where, "p.nfr_key = ?")
		args = append(args, filter.NFRKey)
	}
	if filter.DocumentID > 0 {
		where = append(where, "p.document_id = ?")
		args = append(args, filter.DocumentID)
	}

	query := `SELECT ` + proposalColumns + ` ` + proposalJoins
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	// Pending first, then highest confidence: the review queue should open on
	// the decisions that are still owed, strongest case first.
	query += ` ORDER BY CASE p.status WHEN 'pending' THEN 0 ELSE 1 END, p.confidence DESC, p.id DESC`

	rows, err := r.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Proposal{}
	for rows.Next() {
		p, err := scanProposal(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) SetProposalDecision(id int64, status, decidedAt, decidedBy, note string) error {
	res, err := r.conn.Exec(
		`UPDATE nfr_enrichment_proposals
		 SET status = ?, decided_at = ?, decided_by = ?, decided_note = ?
		 WHERE id = ?`,
		status, decidedAt, decidedBy, note, id)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

// SupersedePending marks every other pending proposal for the same NFR field.
// Accepting one rewrites the field, so the rest were written against text that
// no longer exists; leaving them pending invites a reviewer to accept two
// suggestions that each assumed they were first.
func (r *SQLiteRepository) SupersedePending(nfrKey, field string, exceptID int64, at string) (int64, error) {
	res, err := r.conn.Exec(
		`UPDATE nfr_enrichment_proposals
		 SET status = ?, decided_at = ?, decided_note = ?
		 WHERE nfr_key = ? AND field = ? AND status = ? AND id <> ?`,
		StatusSuperseded, at, "superseded by a newer accepted enrichment",
		nfrKey, field, StatusPending, exceptID)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return affected, nil
}

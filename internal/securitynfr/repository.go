package securitynfr

import (
	"database/sql"
	"sort"
	"strconv"
	"strings"

	"grc/internal/db"
)

type Repository interface {
	Upsert(nfr NFR) error
	UpsertMany(nfrs []NFR) error
	List(search, domain string) ([]NFR, error)
	GetByKey(key string) (NFR, error)
	DeleteByKey(key string) error
}

type SQLiteRepository struct {
	db *db.Conn
}

func NewSQLiteRepository(conn *db.Conn) *SQLiteRepository {
	return &SQLiteRepository{db: conn}
}

const upsertNFRSQL = `INSERT INTO security_nfrs (
		record_key, nfr_id, summary, issue_type, description,
		nist_mapping, additional_details, implementation, domain
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(record_key) DO UPDATE SET
		nfr_id = excluded.nfr_id,
		summary = excluded.summary,
		issue_type = excluded.issue_type,
		description = excluded.description,
		nist_mapping = excluded.nist_mapping,
		additional_details = excluded.additional_details,
		implementation = excluded.implementation,
		domain = excluded.domain`

func upsertNFRArgs(nfr NFR) []any {
	return []any{
		nfr.Key,
		nfr.ID,
		nfr.Summary,
		nfr.IssueType,
		nfr.Description,
		nfr.NISTMapping,
		nfr.AdditionalDetails,
		nfr.Implementation,
		nfr.Domain,
	}
}

func (r *SQLiteRepository) Upsert(nfr NFR) error {
	_, err := r.db.Exec(upsertNFRSQL, upsertNFRArgs(nfr)...)
	return err
}

// UpsertMany upserts all NFRs within a single transaction, so a full catalog
// reseed costs one commit instead of one per NFR.
func (r *SQLiteRepository) UpsertMany(nfrs []NFR) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(upsertNFRSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, nfr := range nfrs {
		if _, err := stmt.Exec(upsertNFRArgs(nfr)...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *SQLiteRepository) List(search, domain string) ([]NFR, error) {
	query := `SELECT
		record_key, nfr_id, summary, issue_type, description,
		nist_mapping, additional_details, implementation, domain
	FROM security_nfrs
	WHERE 1=1`

	args := make([]any, 0, 3)
	if search != "" {
		query += ` AND (
			UPPER(record_key) LIKE ? OR
			UPPER(nfr_id) LIKE ? OR
			UPPER(summary) LIKE ?
		)`
		like := "%" + strings.ToUpper(search) + "%"
		args = append(args, like, like, like)
	}

	if domain != "" {
		query += ` AND UPPER(domain) = ?`
		args = append(args, strings.ToUpper(domain))
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]NFR, 0)
	for rows.Next() {
		item, err := scanNFR(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool {
		return keyLess(out[i].Key, out[j].Key)
	})
	return out, nil
}

func (r *SQLiteRepository) GetByKey(key string) (NFR, error) {
	row := r.db.QueryRow(
		`SELECT
			record_key, nfr_id, summary, issue_type, description,
			nist_mapping, additional_details, implementation, domain
		FROM security_nfrs
		WHERE record_key = ?`,
		key,
	)
	return scanNFR(row)
}

func (r *SQLiteRepository) DeleteByKey(key string) error {
	result, err := r.db.Exec(`DELETE FROM security_nfrs WHERE record_key = ?`, key)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanNFR(scanner interface{ Scan(dest ...any) error }) (NFR, error) {
	var item NFR
	if err := scanner.Scan(
		&item.Key,
		&item.ID,
		&item.Summary,
		&item.IssueType,
		&item.Description,
		&item.NISTMapping,
		&item.AdditionalDetails,
		&item.Implementation,
		&item.Domain,
	); err != nil {
		return NFR{}, err
	}
	return item, nil
}

func keyLess(left, right string) bool {
	l, lErr := strconv.Atoi(left)
	r, rErr := strconv.Atoi(right)
	if lErr == nil && rErr == nil {
		return l < r
	}
	return left < right
}

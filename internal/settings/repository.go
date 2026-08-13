package settings

import (
	"database/sql"
	"errors"
	"fmt"

	"grc/internal/db"
)

// Repository reads and writes stored credentials.
type Repository interface {
	Get(name string) (Record, error)
	Put(rec Record) error
	Delete(name string) error
	List() ([]Record, error)
}

// ErrNotFound reports that no credential is stored under a name.
var ErrNotFound = errors.New("credential not stored")

// SQLRepository is the app_secrets-backed implementation. It works on both
// backends: db.Conn rewrites ? placeholders, and ON CONFLICT DO UPDATE is
// supported by SQLite and PostgreSQL alike.
type SQLRepository struct {
	db *db.Conn
}

// NewSQLRepository returns a repository over the given connection.
func NewSQLRepository(conn *db.Conn) *SQLRepository { return &SQLRepository{db: conn} }

func (r *SQLRepository) Get(name string) (Record, error) {
	var rec Record
	err := r.db.QueryRow(
		`SELECT name, ciphertext, updated_at, updated_by FROM app_secrets WHERE name = ?`, name,
	).Scan(&rec.Name, &rec.Ciphertext, &rec.UpdatedAt, &rec.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("read credential %q: %w", name, err)
	}
	return rec, nil
}

func (r *SQLRepository) Put(rec Record) error {
	_, err := r.db.Exec(`
		INSERT INTO app_secrets (name, ciphertext, updated_at, updated_by)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			ciphertext = excluded.ciphertext,
			updated_at = excluded.updated_at,
			updated_by = excluded.updated_by`,
		rec.Name, rec.Ciphertext, rec.UpdatedAt, rec.UpdatedBy)
	if err != nil {
		return fmt.Errorf("store credential %q: %w", rec.Name, err)
	}
	return nil
}

func (r *SQLRepository) Delete(name string) error {
	if _, err := r.db.Exec(`DELETE FROM app_secrets WHERE name = ?`, name); err != nil {
		return fmt.Errorf("clear credential %q: %w", name, err)
	}
	return nil
}

func (r *SQLRepository) List() ([]Record, error) {
	rows, err := r.db.Query(`SELECT name, ciphertext, updated_at, updated_by FROM app_secrets ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Record
	for rows.Next() {
		var rec Record
		if err := rows.Scan(&rec.Name, &rec.Ciphertext, &rec.UpdatedAt, &rec.UpdatedBy); err != nil {
			return nil, fmt.Errorf("scan credential: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	return out, nil
}

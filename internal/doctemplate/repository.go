package doctemplate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"grc/internal/db"
)

// Repository is the brand-settings persistence boundary. One row, because the
// brand is a property of the install: these are consulting deliverables issued
// under one firm's identity, and a per-client brand would be a different
// feature — client white-labelling — with its own questions about which
// identity a document carries when it is filed.
type Repository interface {
	LoadBrand() (Brand, bool, error)
	SaveBrand(brand Brand) error
	DeleteBrand() error
}

type SQLiteRepository struct {
	db *db.Conn
}

func NewSQLiteRepository(conn *db.Conn) *SQLiteRepository {
	return &SQLiteRepository{db: conn}
}

// LoadBrand returns the saved brand and whether one exists. A fresh install has
// no row, and the caller falls back to DefaultBrand.
//
// The settings are stored as a JSON blob rather than a column per field. The
// alternative would be twenty-odd columns plus a migration every time the
// templates gain a knob, for a single row that is never queried by any of them.
func (r *SQLiteRepository) LoadBrand() (Brand, bool, error) {
	var (
		payload   string
		updatedAt sql.NullString
		updatedBy sql.NullString
	)
	query := r.db.Rebind(`SELECT settings_json, updated_at, updated_by FROM doc_template_brand WHERE id = 1`)
	err := r.db.QueryRow(query).Scan(&payload, &updatedAt, &updatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Brand{}, false, nil
	}
	if err != nil {
		return Brand{}, false, fmt.Errorf("loading brand settings: %w", err)
	}

	// Decoded over the defaults, not into a zero value: a blob written before a
	// field existed would otherwise come back with that field empty, and
	// Validate would reject settings the user never touched.
	brand := DefaultBrand()
	if err := json.Unmarshal([]byte(payload), &brand); err != nil {
		return Brand{}, false, fmt.Errorf("decoding brand settings: %w", err)
	}
	brand.UpdatedAt = updatedAt.String
	brand.UpdatedBy = updatedBy.String
	return brand, true, nil
}

func (r *SQLiteRepository) SaveBrand(brand Brand) error {
	payload, err := json.Marshal(brand)
	if err != nil {
		return fmt.Errorf("encoding brand settings: %w", err)
	}
	query := r.db.Rebind(`
		INSERT INTO doc_template_brand (id, settings_json, updated_at, updated_by)
		VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			settings_json = excluded.settings_json,
			updated_at = excluded.updated_at,
			updated_by = excluded.updated_by`)
	if _, err := r.db.Exec(query, string(payload), brand.UpdatedAt, brand.UpdatedBy); err != nil {
		return fmt.Errorf("saving brand settings: %w", err)
	}
	return nil
}

// DeleteBrand drops the saved row so the install falls back to DefaultBrand.
// That is what Reset does: it restores the brand the templates ship with rather
// than writing a copy of it, so a later change to the shipped defaults reaches
// an install that never customised anything.
func (r *SQLiteRepository) DeleteBrand() error {
	query := r.db.Rebind(`DELETE FROM doc_template_brand WHERE id = 1`)
	if _, err := r.db.Exec(query); err != nil {
		return fmt.Errorf("resetting brand settings: %w", err)
	}
	return nil
}

package controlcatalog

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// oscalColumns is the upstream OSCAL structure stored beside a control. Each is
// compact JSON, or "null" when the control was never imported from OSCAL.
type oscalColumns struct {
	Params     string
	Statement  string
	Assessment string
}

// oscalWrite is one control an OSCAL import inserts or updates. An update
// sets only the columns OSCAL owns: type, name, family, requirements,
// discussion, related controls, the three structure columns, and the baseline
// columns when Baselines is set. It never names the CIA flags, justification,
// threats, potentially_common_inheritable, appendix_json or source_key, so it
// cannot change them.
type oscalWrite struct {
	Insert    bool
	Control   Control
	Columns   oscalColumns
	Baselines *Baselines
}

func (r *SQLiteRepository) oscalColumns() (map[string]oscalColumns, error) {
	rows, err := r.db.Query(`SELECT control_id, params_json, statement_json, assessment_json FROM nist_controls`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]oscalColumns{}
	for rows.Next() {
		var id string
		var c oscalColumns
		if err := rows.Scan(&id, &c.Params, &c.Statement, &c.Assessment); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) oscalColumnsOf(controlID string) (oscalColumns, error) {
	var c oscalColumns
	err := r.db.QueryRow(`SELECT params_json, statement_json, assessment_json FROM nist_controls WHERE control_id = ?`, controlID).
		Scan(&c.Params, &c.Statement, &c.Assessment)
	return c, err
}

// catalogDigest is a SHA-256 over every column of every control, in control_id
// order. An OSCAL import is applied only against the digest its preview was
// made on, so an edit or another import in between makes the apply refuse
// rather than land on a catalog nobody previewed.
func (r *SQLiteRepository) catalogDigest() (string, error) {
	return catalogDigestOf(r.db)
}

type rowQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func catalogDigestOf(q rowQuerier) (string, error) {
	rows, err := q.Query(`SELECT * FROM nist_controls ORDER BY control_id`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	values := make([]sql.NullString, len(cols))
	dest := make([]any, len(cols))
	for i := range values {
		dest[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return "", err
		}
		for i, v := range values {
			// Length-prefixed, so no two different rows hash alike.
			_, _ = fmt.Fprintf(h, "%s:%d:%t:%d:%s;", cols[i], len(v.String), v.Valid, len(v.String), v.String)
		}
		_, _ = h.Write([]byte{'\n'})
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

const insertOSCALControlSQL = `INSERT INTO nist_controls (
		source_key, control_id, control_type, name, family, in_low, in_moderate, in_high, in_privacy,
		mapping_baselines_json, threats_json, requirements, discussion, related_controls_json, appendix_json,
		params_json, statement_json, assessment_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '[]', ?, ?, ?, 'null', ?, ?, ?)`

const updateOSCALControlSQL = `UPDATE nist_controls SET
		control_type = ?, name = ?, family = ?, requirements = ?, discussion = ?, related_controls_json = ?,
		params_json = ?, statement_json = ?, assessment_json = ?
	WHERE control_id = ?`

const updateOSCALBaselinesSQL = `UPDATE nist_controls SET
		in_low = ?, in_moderate = ?, in_high = ?, in_privacy = ?, mapping_baselines_json = ?
	WHERE control_id = ?`

// applyOSCAL writes an OSCAL import in one transaction, after checking that the
// catalog is still the one the import was worked out against.
func (r *SQLiteRepository) applyOSCAL(writes []oscalWrite, expectDigest string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// The digest is read again inside the transaction, so a write that landed
	// after the service's own check is still caught. On SQLite the read pins a
	// snapshot: if another writer commits before the first write here, that
	// write fails rather than applying over the newer catalog.
	if expectDigest != "" {
		got, err := catalogDigestOf(tx)
		if err != nil {
			return err
		}
		if got != expectDigest {
			return errCatalogMoved
		}
	}

	for _, w := range writes {
		c := w.Control
		related, err := json.Marshal(c.RelatedControls)
		if err != nil {
			return fmt.Errorf("%s: %w", c.ID, err)
		}
		if w.Insert {
			b := Baselines{}
			if w.Baselines != nil {
				b = *w.Baselines
			}
			b = normalizeBaselines(b)
			mapping, err := json.Marshal(b)
			if err != nil {
				return fmt.Errorf("%s: %w", c.ID, err)
			}
			if _, err := tx.Exec(insertOSCALControlSQL,
				c.SourceKey, c.ID, c.Type, c.Name, c.Family,
				boolToInt(len(b.Low) > 0), boolToInt(len(b.Moderate) > 0), boolToInt(len(b.High) > 0), boolToInt(len(b.Privacy) > 0),
				string(mapping), c.Requirements, c.Discussion, string(related),
				w.Columns.Params, w.Columns.Statement, w.Columns.Assessment,
			); err != nil {
				return fmt.Errorf("insert %s: %w", c.ID, err)
			}
			continue
		}
		res, err := tx.Exec(updateOSCALControlSQL,
			c.Type, c.Name, c.Family, c.Requirements, c.Discussion, string(related),
			w.Columns.Params, w.Columns.Statement, w.Columns.Assessment, c.ID)
		if err != nil {
			return fmt.Errorf("update %s: %w", c.ID, err)
		}
		if n, err := res.RowsAffected(); err == nil && n != 1 {
			return fmt.Errorf("update %s: %s rows changed", c.ID, strconv.FormatInt(n, 10))
		}
		if w.Baselines != nil {
			b := normalizeBaselines(*w.Baselines)
			mapping, err := json.Marshal(b)
			if err != nil {
				return fmt.Errorf("%s: %w", c.ID, err)
			}
			if _, err := tx.Exec(updateOSCALBaselinesSQL,
				boolToInt(len(b.Low) > 0), boolToInt(len(b.Moderate) > 0), boolToInt(len(b.High) > 0), boolToInt(len(b.Privacy) > 0),
				string(mapping), c.ID); err != nil {
				return fmt.Errorf("update baselines of %s: %w", c.ID, err)
			}
		}
	}
	return tx.Commit()
}

var errCatalogMoved = errors.New("the catalog changed since the preview")

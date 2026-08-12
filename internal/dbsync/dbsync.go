// Package dbsync copies application data between two database backends
// (SQLite or PostgreSQL, in either direction) using the shared logical schema
// defined in internal/db. It is used by the application's -sync-to / -sync-from
// modes to export a local database up to a remote one (or pull the other way).
//
// Sync is merge-oriented: rows are upserted by each table's natural key, so
// rows that exist only on the destination are left untouched. The lone derived
// table (security_nfr_control_links) is fully replaced because it has no stable
// key and is regenerated from its sources; ephemeral login sessions
// (auth_sessions) are skipped.
package dbsync

import (
	"fmt"
	"strings"

	"carelockconsulting/internal/db"
)

// strategy selects how a table's rows are reconciled on the destination.
type strategy int

const (
	// upsert inserts rows and updates existing ones matched on conflictCols.
	upsert strategy = iota
	// replace clears the destination table, then inserts every source row.
	replace
)

// tableSpec describes how one table is copied. cols is the ordered set of
// columns read from the source and written to the destination; for tables
// matched on a natural/business key the auto-increment id is intentionally
// omitted so the destination assigns its own. idKeyed marks tables whose id is
// referenced by foreign keys (so the id is carried over and, on PostgreSQL,
// the identity sequence is realigned afterwards).
type tableSpec struct {
	table        string
	cols         []string
	conflictCols []string
	strat        strategy
	idKeyed      bool
}

// syncOrder lists tables parent-before-child so foreign keys are satisfied as
// each table is committed in turn.
var syncOrder = []tableSpec{
	{table: "users", cols: []string{"name", "email"}, conflictCols: []string{"email"}},
	{table: "nist_stride_mappings", cols: []string{"control_id", "baselines_json", "threats_json"}, conflictCols: []string{"control_id"}},
	{
		table: "rcsa_controls",
		cols: []string{
			"source_key", "control_id", "control_type", "name", "family",
			"in_low", "in_moderate", "in_high", "in_privacy",
			"mapping_baselines_json", "threats_json",
			"confidentiality_status", "integrity_status", "availability_status",
			"justification", "potentially_common_inheritable", "requirements",
			"discussion", "related_controls_json", "appendix_json",
		},
		conflictCols: []string{"control_id"},
	},
	{table: "control_family_visibility", cols: []string{"family", "enabled"}, conflictCols: []string{"family"}},
	{
		table: "security_nfrs",
		cols: []string{
			"record_key", "nfr_id", "summary", "issue_type", "description",
			"nist_mapping", "additional_details", "implementation", "domain",
		},
		conflictCols: []string{"record_key"},
	},
	{
		table: "nfr_control_link_overrides",
		cols:  []string{"nfr_key", "mapping_control_id", "override_control_id", "matched"},
		// Composite primary key.
		conflictCols: []string{"nfr_key", "mapping_control_id"},
	},
	{
		// The uploaded/fetched security-document corpus behind the NFR
		// enrichment module, and the AI proposals derived from it. Chunks and
		// proposals reference a document id, so documents sync first.
		table:        "nfr_source_documents",
		cols:         []string{"id", "title", "origin", "url", "filename", "media_type", "sha256", "byte_size", "uploaded_by", "created_at"},
		conflictCols: []string{"id"},
	},
	{
		table:        "nfr_source_chunks",
		cols:         []string{"id", "document_id", "ordinal", "heading", "body"},
		conflictCols: []string{"id"},
	},
	{
		table:        "nfr_enrichment_proposals",
		cols:         []string{"id", "nfr_key", "document_id", "field", "suggested_text", "rationale", "confidence", "citations_json", "status", "model", "prompt_hash", "created_at", "decided_at", "decided_by", "decided_note"},
		conflictCols: []string{"id"},
	},
	{
		// Per-call token/cost tracking behind the AI Chat usage dashboard. Not
		// referenced by any foreign key, so it is id-keyed only to keep the
		// primary key stable across a round trip rather than out of necessity.
		table:        "ai_usage_log",
		cols:         []string{"id", "provider", "model", "input_tokens", "output_tokens", "created_at"},
		conflictCols: []string{"id"},
		idKeyed:      true,
	},
	{
		table: "auth_users",
		cols:  []string{"username", "password_hash", "is_admin", "allowed_pages_json", "created_at", "updated_at"},
		// auth_sessions (FK -> auth_users) is intentionally not synced.
		conflictCols: []string{"username"},
	},
	{
		table: "security_risk_register",
		cols: []string{
			"risk_id", "title", "business_unit", "asset", "threat_source", "vulnerability",
			"likelihood", "impact", "inherent_score", "current_controls",
			"residual_likelihood", "residual_impact", "residual_score",
			"response_strategy", "response_action", "owner", "status",
			"target_date", "last_review_date", "next_review_date", "risk_appetite_aligned", "notes",
		},
		conflictCols: []string{"risk_id"},
	},
	// Policy documents are id-keyed: sections and versions carry a foreign key
	// to the document, and a document's parent_document_id points at another
	// row in this same table, so the ids have to survive the copy.
	{
		table: "policy_documents",
		cols: []string{
			"id", "client_id", "client_name", "doc_type", "reference", "title", "status",
			"owner_role", "approver", "classification", "frameworks_json",
			"effective_date", "review_cadence_months", "next_review_date",
			"parent_document_id", "summary", "author", "created_at", "updated_at",
		},
		conflictCols: []string{"id"},
		idKeyed:      true,
	},
	{
		table: "policy_sections",
		cols: []string{
			"id", "document_id", "ordinal", "heading", "body", "section_kind",
			"provenance", "provenance_detail", "updated_at",
		},
		conflictCols: []string{"id"},
		idKeyed:      true,
	},
	{
		table: "policy_versions",
		cols: []string{
			"id", "document_id", "version_label", "approved_by", "approved_at",
			"change_summary", "snapshot",
		},
		conflictCols: []string{"id"},
		idKeyed:      true,
	},
	{
		table: "policy_section_controls",
		cols: []string{
			"id", "section_id", "control_id", "framework", "coverage", "note", "created_at",
		},
		conflictCols: []string{"id"},
		idKeyed:      true,
	},
	{
		table:        "stored_json_documents",
		cols:         []string{"id", "name", "source", "content_json", "created_at"},
		conflictCols: []string{"id"},
		idKeyed:      true,
	},
	{
		// The document-template brand: one row, id fixed at 1 by a CHECK.
		// Included because it is authored content rather than machine-local
		// state — a firm's palette and wordmark should survive a migration, and
		// losing them would be silent until the next deliverable came out in
		// the shipped default maroon.
		table:        "doc_template_brand",
		cols:         []string{"id", "settings_json", "updated_at", "updated_by"},
		conflictCols: []string{"id"},
		idKeyed:      true,
	},
	{
		// Derived from the catalogs + overrides; no stable key, so replace it
		// wholesale to keep it consistent with the rows just copied.
		table: "security_nfr_control_links",
		cols: []string{
			"nfr_key", "nfr_id", "nfr_summary", "nfr_domain", "nist_mapping_raw",
			"mapping_control_id", "control_id", "control_name", "control_family", "matched",
		},
		strat: replace,
	},
}

// Report records how many rows were copied per table, in source order.
type Report struct {
	Tables []TableResult
}

// TableResult is the per-table outcome of a sync.
type TableResult struct {
	Table string
	Rows  int
}

// Total returns the number of rows copied across all tables.
func (r Report) Total() int {
	total := 0
	for _, t := range r.Tables {
		total += t.Rows
	}
	return total
}

// Sync copies every managed table from src to dst. Both connections must
// already have their schema initialized (db.Open / OpenSQLite / OpenPostgres
// all do this). It returns a per-table Report; on the first error the partial
// report is returned alongside it.
func Sync(src, dst *db.Conn) (Report, error) {
	var report Report
	for _, spec := range syncOrder {
		rows, err := copyTable(src, dst, spec)
		report.Tables = append(report.Tables, TableResult{Table: spec.table, Rows: rows})
		if err != nil {
			return report, fmt.Errorf("sync %s: %w", spec.table, err)
		}
		if spec.idKeyed {
			if err := resetSequence(dst, spec.table); err != nil {
				return report, fmt.Errorf("reset %s id sequence: %w", spec.table, err)
			}
		}
	}
	return report, nil
}

func copyTable(src, dst *db.Conn, spec tableSpec) (int, error) {
	insertSQL := buildInsertSQL(spec)

	srcRows, err := src.Query(fmt.Sprintf("SELECT %s FROM %s", strings.Join(spec.cols, ", "), spec.table))
	if err != nil {
		return 0, fmt.Errorf("read source: %w", err)
	}
	defer srcRows.Close()

	tx, err := dst.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if spec.strat == replace {
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s", spec.table)); err != nil {
			return 0, fmt.Errorf("clear destination: %w", err)
		}
	}

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	for srcRows.Next() {
		values := make([]any, len(spec.cols))
		ptrs := make([]any, len(spec.cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := srcRows.Scan(ptrs...); err != nil {
			return count, err
		}
		for i := range values {
			// SQLite hands back TEXT columns as []byte; normalize to string so
			// they bind as text (not bytea) on PostgreSQL.
			if b, ok := values[i].([]byte); ok {
				values[i] = string(b)
			}
		}
		if _, err := stmt.Exec(values...); err != nil {
			return count, err
		}
		count++
	}
	if err := srcRows.Err(); err != nil {
		return count, err
	}
	if err := tx.Commit(); err != nil {
		return count, err
	}
	return count, nil
}

// buildInsertSQL renders an INSERT with ? placeholders (db.Conn/db.Tx rebind
// these for PostgreSQL). For upsert specs it appends an ON CONFLICT clause that
// updates the non-key columns; replace specs are plain inserts.
func buildInsertSQL(spec tableSpec) string {
	placeholders := make([]string, len(spec.cols))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	insert := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		spec.table,
		strings.Join(spec.cols, ", "),
		strings.Join(placeholders, ", "),
	)
	if spec.strat == replace || len(spec.conflictCols) == 0 {
		return insert
	}

	conflict := make(map[string]struct{}, len(spec.conflictCols))
	for _, c := range spec.conflictCols {
		conflict[c] = struct{}{}
	}
	var sets []string
	for _, c := range spec.cols {
		if _, isKey := conflict[c]; isKey {
			continue
		}
		sets = append(sets, fmt.Sprintf("%s = excluded.%s", c, c))
	}
	if len(sets) == 0 {
		return insert + fmt.Sprintf(" ON CONFLICT (%s) DO NOTHING", strings.Join(spec.conflictCols, ", "))
	}
	return insert + fmt.Sprintf(
		" ON CONFLICT (%s) DO UPDATE SET %s",
		strings.Join(spec.conflictCols, ", "),
		strings.Join(sets, ", "),
	)
}

// resetSequence realigns a PostgreSQL identity/serial sequence to the table's
// current max id after rows with explicit ids were inserted, so the
// destination's own future inserts don't collide. It is a no-op on SQLite.
func resetSequence(dst *db.Conn, table string) error {
	if dst.Dialect() != db.DialectPostgres {
		return nil
	}
	// setval(..., max(id), true) leaves nextval = max+1; coalesce handles an
	// empty table (start the sequence at 1 without marking it called).
	query := fmt.Sprintf(
		"SELECT setval(pg_get_serial_sequence('%s', 'id'), COALESCE((SELECT MAX(id) FROM %s), 1), (SELECT COUNT(*) > 0 FROM %s))",
		table, table, table,
	)
	_, err := dst.Exec(query)
	return err
}

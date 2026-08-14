package db

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenPostgres connects to an external PostgreSQL database using a standard
// connection string (e.g. postgres://user:pass@host:5432/db?sslmode=require)
// and ensures the schema exists. It mirrors OpenSQLite's tables/indexes using
// Postgres-native types (BIGSERIAL/BIGINT identities, DOUBLE PRECISION) so the
// application's backend-agnostic repositories work unchanged.
func OpenPostgres(dsn string) (*Conn, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if _, err := db.Exec(postgresSchema); err != nil {
		return nil, fmt.Errorf("failed to initialize postgres schema: %w", err)
	}

	return &Conn{DB: db, dialect: DialectPostgres}, nil
}

const postgresSchema = `
CREATE TABLE IF NOT EXISTS users (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	email TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS nist_stride_mappings (
	id BIGSERIAL PRIMARY KEY,
	control_id TEXT NOT NULL UNIQUE,
	baselines_json TEXT NOT NULL,
	threats_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS rcsa_controls (
	id BIGSERIAL PRIMARY KEY,
	source_key TEXT NOT NULL DEFAULT '',
	control_id TEXT NOT NULL UNIQUE,
	control_type TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL DEFAULT '',
	family TEXT NOT NULL,
	in_low INTEGER NOT NULL DEFAULT 0,
	in_moderate INTEGER NOT NULL DEFAULT 0,
	in_high INTEGER NOT NULL DEFAULT 0,
	in_privacy INTEGER NOT NULL DEFAULT 0,
	mapping_baselines_json TEXT NOT NULL,
	threats_json TEXT NOT NULL,
	confidentiality_status TEXT NOT NULL DEFAULT '',
	integrity_status TEXT NOT NULL DEFAULT '',
	availability_status TEXT NOT NULL DEFAULT '',
	justification TEXT NOT NULL DEFAULT '',
	potentially_common_inheritable TEXT NOT NULL DEFAULT '',
	requirements TEXT NOT NULL DEFAULT '',
	discussion TEXT NOT NULL DEFAULT '',
	related_controls_json TEXT NOT NULL DEFAULT '[]',
	appendix_json TEXT NOT NULL DEFAULT 'null'
);

CREATE TABLE IF NOT EXISTS control_family_visibility (
	family TEXT PRIMARY KEY,
	enabled INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS security_nfrs (
	id BIGSERIAL PRIMARY KEY,
	record_key TEXT NOT NULL UNIQUE,
	nfr_id TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	issue_type TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	nist_mapping TEXT NOT NULL DEFAULT '',
	additional_details TEXT NOT NULL DEFAULT '',
	implementation TEXT NOT NULL DEFAULT '',
	domain TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS security_nfr_control_links (
	id BIGSERIAL PRIMARY KEY,
	nfr_key TEXT NOT NULL,
	nfr_id TEXT NOT NULL DEFAULT '',
	nfr_summary TEXT NOT NULL DEFAULT '',
	nfr_domain TEXT NOT NULL DEFAULT '',
	nist_mapping_raw TEXT NOT NULL DEFAULT '',
	mapping_control_id TEXT NOT NULL DEFAULT '',
	control_id TEXT NOT NULL DEFAULT '',
	control_name TEXT NOT NULL DEFAULT '',
	control_family TEXT NOT NULL DEFAULT '',
	matched INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS nfr_control_link_overrides (
	nfr_key TEXT NOT NULL,
	mapping_control_id TEXT NOT NULL,
	override_control_id TEXT NOT NULL DEFAULT '',
	matched INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (nfr_key, mapping_control_id)
);

CREATE TABLE IF NOT EXISTS nfr_source_documents (
	id BIGSERIAL PRIMARY KEY,
	title TEXT NOT NULL DEFAULT '',
	origin TEXT NOT NULL DEFAULT '',
	url TEXT NOT NULL DEFAULT '',
	filename TEXT NOT NULL DEFAULT '',
	media_type TEXT NOT NULL DEFAULT '',
	sha256 TEXT NOT NULL DEFAULT '',
	byte_size BIGINT NOT NULL DEFAULT 0,
	uploaded_by TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS nfr_source_chunks (
	id BIGSERIAL PRIMARY KEY,
	document_id BIGINT NOT NULL,
	ordinal INTEGER NOT NULL DEFAULT 0,
	heading TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_nfr_source_chunks_document
	ON nfr_source_chunks (document_id, ordinal);

CREATE TABLE IF NOT EXISTS nfr_enrichment_proposals (
	id BIGSERIAL PRIMARY KEY,
	nfr_key TEXT NOT NULL DEFAULT '',
	document_id BIGINT NOT NULL,
	field TEXT NOT NULL DEFAULT '',
	suggested_text TEXT NOT NULL DEFAULT '',
	rationale TEXT NOT NULL DEFAULT '',
	confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
	citations_json TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL DEFAULT 'pending',
	model TEXT NOT NULL DEFAULT '',
	prompt_hash TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT '',
	decided_at TEXT NOT NULL DEFAULT '',
	decided_by TEXT NOT NULL DEFAULT '',
	decided_note TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_nfr_enrichment_proposals_status
	ON nfr_enrichment_proposals (status, nfr_key);

CREATE TABLE IF NOT EXISTS ai_usage_log (
	id BIGSERIAL PRIMARY KEY,
	provider TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	input_tokens BIGINT NOT NULL DEFAULT 0,
	output_tokens BIGINT NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS stored_json_documents (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT '',
	content_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS auth_users (
	id BIGSERIAL PRIMARY KEY,
	username TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	is_admin INTEGER NOT NULL DEFAULT 0,
	allowed_pages_json TEXT NOT NULL DEFAULT '[]',
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS auth_sessions (
	session_token TEXT PRIMARY KEY,
	user_id BIGINT NOT NULL,
	expires_at TEXT NOT NULL DEFAULT '',
	FOREIGN KEY(user_id) REFERENCES auth_users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS security_risk_register (
	id BIGSERIAL PRIMARY KEY,
	risk_id TEXT NOT NULL UNIQUE,
	title TEXT NOT NULL DEFAULT '',
	business_unit TEXT NOT NULL DEFAULT '',
	asset TEXT NOT NULL DEFAULT '',
	threat_source TEXT NOT NULL DEFAULT '',
	vulnerability TEXT NOT NULL DEFAULT '',
	likelihood INTEGER NOT NULL DEFAULT 1,
	impact INTEGER NOT NULL DEFAULT 1,
	inherent_score INTEGER NOT NULL DEFAULT 1,
	current_controls TEXT NOT NULL DEFAULT '',
	residual_likelihood INTEGER NOT NULL DEFAULT 1,
	residual_impact INTEGER NOT NULL DEFAULT 1,
	residual_score INTEGER NOT NULL DEFAULT 1,
	response_strategy TEXT NOT NULL DEFAULT 'Mitigate',
	response_action TEXT NOT NULL DEFAULT '',
	owner TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'Open',
	target_date TEXT NOT NULL DEFAULT '',
	last_review_date TEXT NOT NULL DEFAULT '',
	next_review_date TEXT NOT NULL DEFAULT '',
	risk_appetite_aligned INTEGER NOT NULL DEFAULT 0,
	notes TEXT NOT NULL DEFAULT ''
);


CREATE TABLE IF NOT EXISTS policy_documents (
	id BIGSERIAL PRIMARY KEY,
	client_id BIGINT NOT NULL DEFAULT 0,
	client_name TEXT NOT NULL DEFAULT '',
	doc_type TEXT NOT NULL DEFAULT 'policy',
	reference TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'draft',
	owner_role TEXT NOT NULL DEFAULT '',
	approver TEXT NOT NULL DEFAULT '',
	classification TEXT NOT NULL DEFAULT 'Internal',
	frameworks_json TEXT NOT NULL DEFAULT '[]',
	effective_date TEXT NOT NULL DEFAULT '',
	review_cadence_months INTEGER NOT NULL DEFAULT 12,
	next_review_date TEXT NOT NULL DEFAULT '',
	parent_document_id BIGINT NOT NULL DEFAULT 0,
	summary TEXT NOT NULL DEFAULT '',
	author TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS policy_sections (
	id BIGSERIAL PRIMARY KEY,
	document_id BIGINT NOT NULL,
	ordinal INTEGER NOT NULL DEFAULT 0,
	heading TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT '',
	section_kind TEXT NOT NULL DEFAULT 'statements',
	provenance TEXT NOT NULL DEFAULT 'human',
	provenance_detail TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT '',
	FOREIGN KEY(document_id) REFERENCES policy_documents(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS policy_versions (
	id BIGSERIAL PRIMARY KEY,
	document_id BIGINT NOT NULL,
	version_label TEXT NOT NULL DEFAULT '',
	approved_by TEXT NOT NULL DEFAULT '',
	approved_at TEXT NOT NULL DEFAULT '',
	change_summary TEXT NOT NULL DEFAULT '',
	snapshot TEXT NOT NULL DEFAULT '',
	FOREIGN KEY(document_id) REFERENCES policy_documents(id) ON DELETE CASCADE
);

-- No foreign key to rcsa_controls on purpose. The catalog is reseeded from
-- JSON and controls can be deleted from the Control Editor; a cascade there
-- would silently erase coverage claims, which are evidence. Dangling refs are
-- surfaced as orphans by the coverage report instead.
CREATE TABLE IF NOT EXISTS policy_section_controls (
	id BIGSERIAL PRIMARY KEY,
	section_id BIGINT NOT NULL,
	control_id TEXT NOT NULL,
	framework TEXT NOT NULL DEFAULT '',
	coverage TEXT NOT NULL DEFAULT 'full',
	note TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT '',
	FOREIGN KEY(section_id) REFERENCES policy_sections(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_policy_section_controls_unique
	ON policy_section_controls(section_id, control_id);
CREATE INDEX IF NOT EXISTS idx_policy_section_controls_control
	ON policy_section_controls(control_id);

-- Added after policy_documents shipped. CREATE TABLE IF NOT EXISTS does not add
-- a column to an existing table, so an upgraded database needs this explicitly;
-- ADD COLUMN IF NOT EXISTS makes it idempotent.
ALTER TABLE policy_documents ADD COLUMN IF NOT EXISTS author TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_policy_documents_client ON policy_documents(client_id);
CREATE INDEX IF NOT EXISTS idx_policy_documents_status ON policy_documents(status);
CREATE INDEX IF NOT EXISTS idx_policy_sections_document ON policy_sections(document_id, ordinal);
CREATE INDEX IF NOT EXISTS idx_policy_versions_document ON policy_versions(document_id);

CREATE TABLE IF NOT EXISTS app_state (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);

-- Credentials set in the Settings page (AI provider keys today). Values are
-- AES-256-GCM ciphertext, base64-encoded so both backends store them as TEXT
-- rather than diverging over BLOB/BYTEA. Deliberately separate from app_state
-- so plaintext state and secret material never share a table, and so who set a
-- credential and when is recorded for the audit trail.
CREATE TABLE IF NOT EXISTS app_secrets (
	name TEXT PRIMARY KEY,
	ciphertext TEXT NOT NULL,
	updated_at TEXT NOT NULL DEFAULT '',
	updated_by TEXT NOT NULL DEFAULT ''
);



-- Document-template brand settings: one row, because the brand is a property of
-- the install rather than of a client. The settings are a JSON blob rather than
-- a column per knob -- the row is never queried by any of them, and a column
-- each would mean a migration every time the templates gain a setting.
CREATE TABLE IF NOT EXISTS doc_template_brand (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	settings_json TEXT NOT NULL DEFAULT '{}',
	updated_at TEXT NOT NULL DEFAULT '',
	updated_by TEXT NOT NULL DEFAULT ''
);

-- Regulation Coverage: an uploaded regulation, its sections, the model's
-- findings and mappings, the immutable report versions, and the conversation
-- held about them. See internal/regcoverage.
CREATE TABLE IF NOT EXISTS reg_coverage_regulations (
	id BIGSERIAL PRIMARY KEY,
	title TEXT NOT NULL DEFAULT '',
	framework TEXT NOT NULL DEFAULT '',
	framework_name TEXT NOT NULL DEFAULT '',
	source_ref TEXT NOT NULL DEFAULT '',
	detected INTEGER NOT NULL DEFAULT 0,
	filename TEXT NOT NULL DEFAULT '',
	media_type TEXT NOT NULL DEFAULT '',
	sha256 TEXT NOT NULL DEFAULT '',
	byte_size BIGINT NOT NULL DEFAULT 0,
	extract_method TEXT NOT NULL DEFAULT '',
	extract_notes TEXT NOT NULL DEFAULT '',
	body_text TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'ingested',
	status_detail TEXT NOT NULL DEFAULT '',
	uploaded_by TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT '',
	analyzed_at TEXT NOT NULL DEFAULT ''
);

-- The upload itself, kept so the report can be read against the original
-- document rather than against this module's extraction of it. Separate from
-- the regulation row because every listing would otherwise carry a
-- multi-megabyte blob it never reads.
CREATE TABLE IF NOT EXISTS reg_coverage_sources (
	regulation_id BIGINT PRIMARY KEY REFERENCES reg_coverage_regulations(id) ON DELETE CASCADE,
	media_type TEXT NOT NULL DEFAULT '',
	filename TEXT NOT NULL DEFAULT '',
	byte_size BIGINT NOT NULL DEFAULT 0,
	content BYTEA NOT NULL
);

CREATE TABLE IF NOT EXISTS reg_coverage_sections (
	id BIGSERIAL PRIMARY KEY,
	regulation_id BIGINT NOT NULL REFERENCES reg_coverage_regulations(id) ON DELETE CASCADE,
	ref TEXT NOT NULL DEFAULT '',
	label TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT '',
	position INTEGER NOT NULL DEFAULT 0,
	confidence TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_reg_coverage_sections_regulation
	ON reg_coverage_sections (regulation_id, position);

CREATE TABLE IF NOT EXISTS reg_coverage_findings (
	id BIGSERIAL PRIMARY KEY,
	regulation_id BIGINT NOT NULL REFERENCES reg_coverage_regulations(id) ON DELETE CASCADE,
	section_id BIGINT NOT NULL REFERENCES reg_coverage_sections(id) ON DELETE CASCADE,
	relevant INTEGER NOT NULL DEFAULT 0,
	requirement TEXT NOT NULL DEFAULT '',
	commentary TEXT NOT NULL DEFAULT '',
	gaps TEXT NOT NULL DEFAULT '',
	quote TEXT NOT NULL DEFAULT '',
	grounded INTEGER NOT NULL DEFAULT 0,
	confidence TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	prompt_hash TEXT NOT NULL DEFAULT '',
	revision INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_reg_coverage_findings_section
	ON reg_coverage_findings (section_id, revision);

CREATE TABLE IF NOT EXISTS reg_coverage_mappings (
	id BIGSERIAL PRIMARY KEY,
	finding_id BIGINT NOT NULL REFERENCES reg_coverage_findings(id) ON DELETE CASCADE,
	kind TEXT NOT NULL DEFAULT '',
	ref TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	rationale TEXT NOT NULL DEFAULT '',
	confidence TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT '',
	known INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_reg_coverage_mappings_finding
	ON reg_coverage_mappings (finding_id);

CREATE TABLE IF NOT EXISTS reg_coverage_versions (
	id BIGSERIAL PRIMARY KEY,
	regulation_id BIGINT NOT NULL REFERENCES reg_coverage_regulations(id) ON DELETE CASCADE,
	number INTEGER NOT NULL DEFAULT 1,
	summary TEXT NOT NULL DEFAULT '',
	note TEXT NOT NULL DEFAULT '',
	snapshot TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT '',
	created_by TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_reg_coverage_versions_number
	ON reg_coverage_versions (regulation_id, number);

CREATE TABLE IF NOT EXISTS reg_coverage_chat (
	id BIGSERIAL PRIMARY KEY,
	regulation_id BIGINT NOT NULL REFERENCES reg_coverage_regulations(id) ON DELETE CASCADE,
	version INTEGER NOT NULL DEFAULT 0,
	role TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	actor TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_reg_coverage_chat_regulation
	ON reg_coverage_chat (regulation_id, id);

CREATE INDEX IF NOT EXISTS idx_rcsa_controls_control_id ON rcsa_controls(control_id);
CREATE INDEX IF NOT EXISTS idx_rcsa_controls_family_type ON rcsa_controls(family, control_type);
CREATE INDEX IF NOT EXISTS idx_security_nfrs_domain ON security_nfrs(domain);
CREATE INDEX IF NOT EXISTS idx_security_nfr_links_control_match ON security_nfr_control_links(control_id, matched);
CREATE INDEX IF NOT EXISTS idx_security_nfr_links_nfr_key ON security_nfr_control_links(nfr_key, mapping_control_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_users_username ON auth_users(username);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_id ON auth_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_stored_json_documents_created_at ON stored_json_documents(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_security_risk_register_status ON security_risk_register(status);
CREATE INDEX IF NOT EXISTS idx_security_risk_register_owner ON security_risk_register(owner);
CREATE INDEX IF NOT EXISTS idx_security_risk_register_scores ON security_risk_register(residual_score, inherent_score);
CREATE UNIQUE INDEX IF NOT EXISTS idx_security_risk_register_risk_id ON security_risk_register(risk_id);`

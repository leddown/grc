package db

import (
	"database/sql"
	"fmt"
)

// modernc.org/sqlite is SQLite translated to Go rather than bound to it through
// cgo. It is the driver here because the C one made every cold build pay a
// single-threaded compile of the SQLite amalgamation — minutes on a modest
// machine, and once per cross-compilation target — and required a C toolchain
// for each of those targets. This one compiles like any other Go package and
// cross-compiles with no toolchain at all. It also ships FTS5, which the C
// driver only included behind a build tag.
import _ "modernc.org/sqlite"

func OpenSQLite(path string) (*Conn, error) {
	// The driver name is "sqlite", not the C driver's "sqlite3".
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("failed to set %s: %w", pragma, err)
		}
	}

	const schema = `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		email TEXT NOT NULL UNIQUE
	);

	CREATE TABLE IF NOT EXISTS nist_stride_mappings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		control_id TEXT NOT NULL UNIQUE,
		baselines_json TEXT NOT NULL,
		threats_json TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS rcsa_controls (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
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
		id INTEGER PRIMARY KEY AUTOINCREMENT,
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
		id INTEGER PRIMARY KEY AUTOINCREMENT,
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
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL DEFAULT '',
		origin TEXT NOT NULL DEFAULT '',
		url TEXT NOT NULL DEFAULT '',
		filename TEXT NOT NULL DEFAULT '',
		media_type TEXT NOT NULL DEFAULT '',
		sha256 TEXT NOT NULL DEFAULT '',
		byte_size INTEGER NOT NULL DEFAULT 0,
		library_doc_id INTEGER NOT NULL DEFAULT 0,
		extract_via TEXT NOT NULL DEFAULT '',
		uploaded_by TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS nfr_source_chunks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		document_id INTEGER NOT NULL,
		ordinal INTEGER NOT NULL DEFAULT 0,
		heading TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_nfr_source_chunks_document
		ON nfr_source_chunks (document_id, ordinal);

	CREATE TABLE IF NOT EXISTS nfr_enrichment_proposals (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		nfr_key TEXT NOT NULL DEFAULT '',
		document_id INTEGER NOT NULL,
		field TEXT NOT NULL DEFAULT '',
		suggested_text TEXT NOT NULL DEFAULT '',
		rationale TEXT NOT NULL DEFAULT '',
		confidence REAL NOT NULL DEFAULT 0,
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
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		provider TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS stored_json_documents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		content_json TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS auth_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		is_admin INTEGER NOT NULL DEFAULT 0,
		allowed_pages_json TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS auth_sessions (
		session_token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		expires_at TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(user_id) REFERENCES auth_users(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS security_risk_register (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
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
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_id INTEGER NOT NULL DEFAULT 0,
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
		parent_document_id INTEGER NOT NULL DEFAULT 0,
		summary TEXT NOT NULL DEFAULT '',
		author TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS policy_sections (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		document_id INTEGER NOT NULL,
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
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		document_id INTEGER NOT NULL,
		version_label TEXT NOT NULL DEFAULT '',
		approved_by TEXT NOT NULL DEFAULT '',
		approved_at TEXT NOT NULL DEFAULT '',
		change_summary TEXT NOT NULL DEFAULT '',
		snapshot TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(document_id) REFERENCES policy_documents(id) ON DELETE CASCADE
	);

	-- No foreign key to rcsa_controls on purpose. The catalog is reseeded from
	-- JSON and controls can be deleted from the Control Editor; a cascade there
	-- would silently erase coverage claims, which are evidence. Dangling refs
	-- are surfaced as orphans by the coverage report instead.
	CREATE TABLE IF NOT EXISTS policy_section_controls (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		section_id INTEGER NOT NULL,
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

	CREATE INDEX IF NOT EXISTS idx_policy_documents_client ON policy_documents(client_id);
	CREATE INDEX IF NOT EXISTS idx_policy_documents_status ON policy_documents(status);
	CREATE INDEX IF NOT EXISTS idx_policy_sections_document ON policy_sections(document_id, ordinal);
	CREATE INDEX IF NOT EXISTS idx_policy_versions_document ON policy_versions(document_id);

	-- "What is due this month" is the calendar view's only query; without this
	-- it is a scan of every task the install has ever held.


	CREATE TABLE IF NOT EXISTS doc_template_brand (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		settings_json TEXT NOT NULL DEFAULT '{}',
		updated_at TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS reg_coverage_regulations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL DEFAULT '',
		framework TEXT NOT NULL DEFAULT '',
		framework_name TEXT NOT NULL DEFAULT '',
		source_ref TEXT NOT NULL DEFAULT '',
		detected INTEGER NOT NULL DEFAULT 0,
		library_doc_id INTEGER NOT NULL DEFAULT 0,
		filename TEXT NOT NULL DEFAULT '',
		media_type TEXT NOT NULL DEFAULT '',
		sha256 TEXT NOT NULL DEFAULT '',
		byte_size INTEGER NOT NULL DEFAULT 0,
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
	-- document rather than against this module's extraction of it. Separate
	-- from the regulation row because every listing would otherwise carry a
	-- multi-megabyte blob it never reads.
	CREATE TABLE IF NOT EXISTS reg_coverage_sources (
		regulation_id INTEGER PRIMARY KEY,
		media_type TEXT NOT NULL DEFAULT '',
		filename TEXT NOT NULL DEFAULT '',
		byte_size INTEGER NOT NULL DEFAULT 0,
		content BLOB NOT NULL,
		FOREIGN KEY(regulation_id) REFERENCES reg_coverage_regulations(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS reg_coverage_sections (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		regulation_id INTEGER NOT NULL,
		ref TEXT NOT NULL DEFAULT '',
		label TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		position INTEGER NOT NULL DEFAULT 0,
		confidence TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(regulation_id) REFERENCES reg_coverage_regulations(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_reg_coverage_sections_regulation
		ON reg_coverage_sections (regulation_id, position);

	-- One row per section per revision. The current finding for a section is
	-- the highest revision; the earlier ones are kept because a version
	-- snapshot has to stay reproducible after a revision replaces it.
	CREATE TABLE IF NOT EXISTS reg_coverage_findings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		regulation_id INTEGER NOT NULL,
		section_id INTEGER NOT NULL,
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
		created_at TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(regulation_id) REFERENCES reg_coverage_regulations(id) ON DELETE CASCADE,
		FOREIGN KEY(section_id) REFERENCES reg_coverage_sections(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_reg_coverage_findings_section
		ON reg_coverage_findings (section_id, revision);

	CREATE TABLE IF NOT EXISTS reg_coverage_mappings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		finding_id INTEGER NOT NULL,
		kind TEXT NOT NULL DEFAULT '',
		ref TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		rationale TEXT NOT NULL DEFAULT '',
		confidence TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		known INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY(finding_id) REFERENCES reg_coverage_findings(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_reg_coverage_mappings_finding
		ON reg_coverage_mappings (finding_id);

	CREATE TABLE IF NOT EXISTS reg_coverage_versions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		regulation_id INTEGER NOT NULL,
		number INTEGER NOT NULL DEFAULT 1,
		summary TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		snapshot TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(regulation_id) REFERENCES reg_coverage_regulations(id) ON DELETE CASCADE
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_reg_coverage_versions_number
		ON reg_coverage_versions (regulation_id, number);

	CREATE TABLE IF NOT EXISTS reg_coverage_chat (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		regulation_id INTEGER NOT NULL,
		version INTEGER NOT NULL DEFAULT 0,
		role TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		actor TEXT NOT NULL DEFAULT '',
		session_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(regulation_id) REFERENCES reg_coverage_regulations(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_reg_coverage_chat_regulation
		ON reg_coverage_chat (regulation_id, id);

	-- ---- Risk & Crisis Exercise (internal/crisisexercise) ----
	--
	-- One exercise is one delivered instance: designed, run, and reported on.
	-- A re-run is a clone rather than a second pass over the same rows, so an
	-- exercise that has been reported on cannot silently acquire a different
	-- set of observations.
	CREATE TABLE IF NOT EXISTS crisis_ex_exercises (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		reference TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		format TEXT NOT NULL DEFAULT 'tabletop',
		kind TEXT NOT NULL DEFAULT 'discussion',
		audience TEXT NOT NULL DEFAULT 'management',
		entity_name TEXT NOT NULL DEFAULT '',
		entity_type TEXT NOT NULL DEFAULT '',
		jurisdiction TEXT NOT NULL DEFAULT '',
		supervision TEXT NOT NULL DEFAULT '',
		critical_functions TEXT NOT NULL DEFAULT '',
		threat_actor TEXT NOT NULL DEFAULT '',
		threat_narrative TEXT NOT NULL DEFAULT '',
		initial_vector TEXT NOT NULL DEFAULT '',
		scenario_key TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'draft',
		tlp TEXT NOT NULL DEFAULT 'TLP:AMBER',
		scheduled_for TEXT NOT NULL DEFAULT '',
		duration_minutes INTEGER NOT NULL DEFAULT 0,
		started_at TEXT NOT NULL DEFAULT '',
		ended_at TEXT NOT NULL DEFAULT '',
		facilitator TEXT NOT NULL DEFAULT '',
		control_team TEXT NOT NULL DEFAULT '',
		evaluators TEXT NOT NULL DEFAULT '',
		ai_generated INTEGER NOT NULL DEFAULT 0,
		model TEXT NOT NULL DEFAULT '',
		prompt_hash TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT ''
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_crisis_ex_exercises_reference
		ON crisis_ex_exercises (reference);

	CREATE TABLE IF NOT EXISTS crisis_ex_objectives (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		ordinal INTEGER NOT NULL DEFAULT 0,
		code TEXT NOT NULL DEFAULT '',
		text TEXT NOT NULL DEFAULT '',
		capability TEXT NOT NULL DEFAULT '',
		success_criteria TEXT NOT NULL DEFAULT '',
		rating TEXT NOT NULL DEFAULT 'untested',
		notes TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_crisis_ex_objectives_exercise
		ON crisis_ex_objectives (exercise_id, ordinal);

	CREATE TABLE IF NOT EXISTS crisis_ex_phases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		ordinal INTEGER NOT NULL DEFAULT 0,
		phase_key TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL DEFAULT '',
		purpose TEXT NOT NULL DEFAULT '',
		entry_criteria TEXT NOT NULL DEFAULT '',
		exit_criteria TEXT NOT NULL DEFAULT '',
		lead_role TEXT NOT NULL DEFAULT '',
		offset_minutes INTEGER NOT NULL DEFAULT 0,
		duration_minutes INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'pending',
		notes TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_crisis_ex_phases_exercise
		ON crisis_ex_phases (exercise_id, ordinal);

	-- The Master Scenario Events List. Ordered by offset rather than by id
	-- because an inject inserted later in design still belongs at its own point
	-- on the clock.
	CREATE TABLE IF NOT EXISTS crisis_ex_injects (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		phase_id INTEGER NOT NULL DEFAULT 0,
		ordinal INTEGER NOT NULL DEFAULT 0,
		code TEXT NOT NULL DEFAULT '',
		offset_minutes INTEGER NOT NULL DEFAULT 0,
		title TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		channel TEXT NOT NULL DEFAULT '',
		from_actor TEXT NOT NULL DEFAULT '',
		to_actor TEXT NOT NULL DEFAULT '',
		inject_type TEXT NOT NULL DEFAULT 'event',
		expected_actions TEXT NOT NULL DEFAULT '',
		expected_decision TEXT NOT NULL DEFAULT '',
		decision_owner TEXT NOT NULL DEFAULT '',
		evaluation_notes TEXT NOT NULL DEFAULT '',
		difficulty TEXT NOT NULL DEFAULT 'challenge',
		ai_generated INTEGER NOT NULL DEFAULT 0,
		model TEXT NOT NULL DEFAULT '',
		prompt_hash TEXT NOT NULL DEFAULT '',
		confidence TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_crisis_ex_injects_exercise
		ON crisis_ex_injects (exercise_id, offset_minutes, ordinal);

	-- One response per inject: re-recording corrects the record rather than
	-- appending a second account of the same moment.
	CREATE TABLE IF NOT EXISTS crisis_ex_responses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		inject_id INTEGER NOT NULL,
		delivered_at TEXT NOT NULL DEFAULT '',
		responded_offset INTEGER NOT NULL DEFAULT -1,
		outcome TEXT NOT NULL DEFAULT 'not_played',
		actual_actions TEXT NOT NULL DEFAULT '',
		observations TEXT NOT NULL DEFAULT '',
		evaluator TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE,
		FOREIGN KEY(inject_id) REFERENCES crisis_ex_injects(id) ON DELETE CASCADE
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_crisis_ex_responses_inject
		ON crisis_ex_responses (inject_id);

	CREATE TABLE IF NOT EXISTS crisis_ex_decisions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		phase_id INTEGER NOT NULL DEFAULT 0,
		offset_minutes INTEGER NOT NULL DEFAULT 0,
		title TEXT NOT NULL DEFAULT '',
		options TEXT NOT NULL DEFAULT '',
		decision TEXT NOT NULL DEFAULT '',
		rationale TEXT NOT NULL DEFAULT '',
		made_by TEXT NOT NULL DEFAULT '',
		role TEXT NOT NULL DEFAULT '',
		authority TEXT NOT NULL DEFAULT '',
		reversible INTEGER NOT NULL DEFAULT 1,
		regulatory_implication TEXT NOT NULL DEFAULT '',
		customer_impact TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_crisis_ex_decisions_exercise
		ON crisis_ex_decisions (exercise_id, offset_minutes);

	-- The DORA materiality assessment, one row per exercise. Kept as its own
	-- table rather than as columns on the exercise because it is the artefact
	-- of one phase, it is re-entered as facts change, and it is what the clock
	-- arithmetic reads.
	CREATE TABLE IF NOT EXISTS crisis_ex_classification (
		exercise_id INTEGER PRIMARY KEY,
		aware_offset INTEGER NOT NULL DEFAULT 0,
		classified_offset INTEGER NOT NULL DEFAULT 0,
		critical_services_affected INTEGER NOT NULL DEFAULT 0,
		clients_affected TEXT NOT NULL DEFAULT '',
		clients_material INTEGER NOT NULL DEFAULT 0,
		transactions_affected TEXT NOT NULL DEFAULT '',
		transactions_material INTEGER NOT NULL DEFAULT 0,
		reputational_impact TEXT NOT NULL DEFAULT '',
		reputational_material INTEGER NOT NULL DEFAULT 0,
		downtime_minutes INTEGER NOT NULL DEFAULT 0,
		duration_material INTEGER NOT NULL DEFAULT 0,
		geographical_spread TEXT NOT NULL DEFAULT '',
		geographical_material INTEGER NOT NULL DEFAULT 0,
		data_losses TEXT NOT NULL DEFAULT '',
		data_losses_material INTEGER NOT NULL DEFAULT 0,
		economic_impact TEXT NOT NULL DEFAULT '',
		economic_material INTEGER NOT NULL DEFAULT 0,
		personal_data_breach INTEGER NOT NULL DEFAULT 0,
		nis2_significant INTEGER NOT NULL DEFAULT 0,
		major INTEGER NOT NULL DEFAULT 0,
		rationale TEXT NOT NULL DEFAULT '',
		team_verdict TEXT NOT NULL DEFAULT '',
		notes TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE
	);

	-- actual_offset is -1 until a notification is recorded, which is why it is
	-- not an unsigned count of minutes.
	CREATE TABLE IF NOT EXISTS crisis_ex_clocks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		ordinal INTEGER NOT NULL DEFAULT 0,
		regime TEXT NOT NULL DEFAULT '',
		authority TEXT NOT NULL DEFAULT '',
		label TEXT NOT NULL DEFAULT '',
		basis TEXT NOT NULL DEFAULT '',
		due_offset INTEGER NOT NULL DEFAULT 0,
		actual_offset INTEGER NOT NULL DEFAULT -1,
		status TEXT NOT NULL DEFAULT 'pending',
		evidence TEXT NOT NULL DEFAULT '',
		notes TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_crisis_ex_clocks_exercise
		ON crisis_ex_clocks (exercise_id, ordinal);

	CREATE TABLE IF NOT EXISTS crisis_ex_findings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		phase_id INTEGER NOT NULL DEFAULT 0,
		ordinal INTEGER NOT NULL DEFAULT 0,
		code TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT '',
		severity TEXT NOT NULL DEFAULT 'medium',
		description TEXT NOT NULL DEFAULT '',
		root_cause TEXT NOT NULL DEFAULT '',
		evidence TEXT NOT NULL DEFAULT '',
		recommendation TEXT NOT NULL DEFAULT '',
		owner TEXT NOT NULL DEFAULT '',
		due_date TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'open',
		risk_ref TEXT NOT NULL DEFAULT '',
		ai_generated INTEGER NOT NULL DEFAULT 0,
		model TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_crisis_ex_findings_exercise
		ON crisis_ex_findings (exercise_id, ordinal);

	CREATE TABLE IF NOT EXISTS crisis_ex_participants (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		role_key TEXT NOT NULL DEFAULT '',
		org TEXT NOT NULL DEFAULT '',
		player INTEGER NOT NULL DEFAULT 1,
		attended INTEGER NOT NULL DEFAULT 0,
		contact TEXT NOT NULL DEFAULT '',
		notes TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_crisis_ex_participants_exercise
		ON crisis_ex_participants (exercise_id);

	-- The link table that lets any part of an exercise cite any part of this
	-- installation's compliance vocabulary, plus the seeded authority catalog.
	-- Polymorphic on both ends because the alternative was thirty tables that
	-- would still have missed the pairing somebody wanted next.
	CREATE TABLE IF NOT EXISTS crisis_ex_references (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		owner_kind TEXT NOT NULL DEFAULT '',
		owner_id INTEGER NOT NULL DEFAULT 0,
		ref_kind TEXT NOT NULL DEFAULT '',
		ref TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT 'manual',
		known INTEGER NOT NULL DEFAULT 0,
		url TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_crisis_ex_references_owner
		ON crisis_ex_references (exercise_id, owner_kind, owner_id);
	CREATE INDEX IF NOT EXISTS idx_crisis_ex_references_target
		ON crisis_ex_references (ref_kind, ref);

	CREATE TABLE IF NOT EXISTS crisis_ex_versions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		number INTEGER NOT NULL DEFAULT 1,
		kind TEXT NOT NULL DEFAULT 'design',
		summary TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		snapshot TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_crisis_ex_versions_number
		ON crisis_ex_versions (exercise_id, number);

	CREATE TABLE IF NOT EXISTS crisis_ex_chat (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		persona TEXT NOT NULL DEFAULT '',
		role TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		actor TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL DEFAULT '',
		session_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(exercise_id) REFERENCES crisis_ex_exercises(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_crisis_ex_chat_exercise
		ON crisis_ex_chat (exercise_id, id);`

	const stateSchema = `
	CREATE TABLE IF NOT EXISTS app_state (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	);

	-- Credentials set in the Settings page (AI provider keys today). Values are
	-- AES-256-GCM ciphertext, base64-encoded so both backends store them as
	-- TEXT rather than diverging over BLOB/BYTEA. Deliberately separate from
	-- app_state so plaintext state and secret material never share a table, and
	-- so who set a credential and when is recorded for the audit trail.
	CREATE TABLE IF NOT EXISTS app_secrets (
		name TEXT PRIMARY KEY,
		ciphertext TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT ''
	);`

	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	if _, err := db.Exec(stateSchema); err != nil {
		return nil, err
	}

	if err := ensureColumn(db, "rcsa_controls", "name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "rcsa_controls", "source_key", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "rcsa_controls", "control_type", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "rcsa_controls", "requirements", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "rcsa_controls", "discussion", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "rcsa_controls", "related_controls_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "rcsa_controls", "appendix_json", "TEXT NOT NULL DEFAULT 'null'"); err != nil {
		return nil, err
	}
	// Added after policy_documents shipped, so existing databases need the
	// column back-filled -- CREATE TABLE IF NOT EXISTS will not add it and the
	// repository would fail on every read.
	if err := ensureColumn(db, "policy_documents", "author", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	// Source documents are no longer uploaded here: they are uploaded to an
	// agent's library on the Wintermute server, which extracts them, and this
	// records which document a copy came from and how its text was read. The
	// `origin` and `url` columns stay for rows imported before that — they are
	// no longer written, and a row with neither reads as one of those.
	// Regulations are no longer uploaded here either — see internal/regcoverage.
	// reg_coverage_sources kept the original bytes and is no longer written;
	// existing rows are left alone rather than dropped, so an installation that
	// still holds an original does not lose it to an upgrade.
	if err := ensureColumn(db, "reg_coverage_regulations", "library_doc_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "nfr_source_documents", "library_doc_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "nfr_source_documents", "extract_via", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfrs", "nfr_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfrs", "summary", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfrs", "issue_type", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfrs", "description", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfrs", "nist_mapping", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfrs", "additional_details", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfrs", "implementation", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfrs", "domain", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfr_control_links", "nfr_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfr_control_links", "nfr_summary", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfr_control_links", "nfr_domain", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfr_control_links", "nist_mapping_raw", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfr_control_links", "mapping_control_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfr_control_links", "control_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfr_control_links", "control_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfr_control_links", "control_family", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_nfr_control_links", "matched", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "nfr_control_link_overrides", "override_control_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "nfr_control_link_overrides", "matched", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return nil, err
	}
	// The client a policy was issued to used to be resolved through the CRM's
	// client table. The CRM has moved to wintermute, and the name is now stored
	// on the document — which is also the more correct record: an approved
	// deliverable's cover page must not change because somebody renamed a client
	// a year later.
	if err := ensureColumn(db, "policy_documents", "client_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "stored_json_documents", "name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "stored_json_documents", "source", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "stored_json_documents", "content_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "stored_json_documents", "created_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "auth_users", "username", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "auth_users", "password_hash", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "auth_users", "is_admin", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "auth_users", "allowed_pages_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "auth_users", "created_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "auth_users", "updated_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "auth_sessions", "user_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "auth_sessions", "expires_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "risk_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "title", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "business_unit", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "asset", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "threat_source", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "vulnerability", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "likelihood", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "impact", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "inherent_score", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "current_controls", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "residual_likelihood", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "residual_impact", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "residual_score", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "response_strategy", "TEXT NOT NULL DEFAULT 'Mitigate'"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "response_action", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "owner", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "status", "TEXT NOT NULL DEFAULT 'Open'"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "target_date", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "last_review_date", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "next_review_date", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "risk_appetite_aligned", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return nil, err
	}
	if err := ensureColumn(db, "security_risk_register", "notes", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_rcsa_controls_control_id", "CREATE INDEX IF NOT EXISTS idx_rcsa_controls_control_id ON rcsa_controls(control_id)"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_rcsa_controls_family_type", "CREATE INDEX IF NOT EXISTS idx_rcsa_controls_family_type ON rcsa_controls(family, control_type)"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_security_nfrs_domain", "CREATE INDEX IF NOT EXISTS idx_security_nfrs_domain ON security_nfrs(domain)"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_security_nfr_links_control_match", "CREATE INDEX IF NOT EXISTS idx_security_nfr_links_control_match ON security_nfr_control_links(control_id, matched)"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_security_nfr_links_nfr_key", "CREATE INDEX IF NOT EXISTS idx_security_nfr_links_nfr_key ON security_nfr_control_links(nfr_key, mapping_control_id)"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_auth_users_username", "CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_users_username ON auth_users(username)"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_auth_sessions_user_id", "CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_id ON auth_sessions(user_id)"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_stored_json_documents_created_at", "CREATE INDEX IF NOT EXISTS idx_stored_json_documents_created_at ON stored_json_documents(created_at DESC)"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_security_risk_register_status", "CREATE INDEX IF NOT EXISTS idx_security_risk_register_status ON security_risk_register(status)"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_security_risk_register_owner", "CREATE INDEX IF NOT EXISTS idx_security_risk_register_owner ON security_risk_register(owner)"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_security_risk_register_scores", "CREATE INDEX IF NOT EXISTS idx_security_risk_register_scores ON security_risk_register(residual_score, inherent_score)"); err != nil {
		return nil, err
	}
	if err := ensureIndex(db, "idx_security_risk_register_risk_id", "CREATE UNIQUE INDEX IF NOT EXISTS idx_security_risk_register_risk_id ON security_risk_register(risk_id)"); err != nil {
		return nil, err
	}

	return &Conn{DB: db, dialect: DialectSQLite}, nil
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var colType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func ensureIndex(db *sql.DB, _ string, statement string) error {
	_, err := db.Exec(statement)
	return err
}

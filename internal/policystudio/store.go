package policystudio

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reearth/ygo/crdt"

	"grc/internal/db"
	"grc/internal/policydocs"
)

// RoomPrefix names a policy's collaboration room: "policy:<document id>".
const RoomPrefix = "policy:"

// RoomName is the room for a document.
func RoomName(documentID int64) string { return RoomPrefix + strconv.FormatInt(documentID, 10) }

// DocumentIDOf parses a room name.
func DocumentIDOf(room string) (int64, bool) {
	rest, ok := strings.CutPrefix(room, RoomPrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	return id, err == nil && id > 0
}

// Snapshot reasons.
const (
	SnapshotInterval      = "interval"
	SnapshotStatus        = "status"
	SnapshotPreMigration  = "pre_migration"
	SnapshotSuperseded    = "superseded"
	SnapshotFormatYjs     = "yjs"
	SnapshotFormatLegacy  = "legacy_json"
	defaultSnapshotRetain = 50
)

// Store keeps each Studio document's Yjs state in the policy_doc_* tables. It
// is the same code on SQLite and PostgreSQL: internal/db rebinds the
// placeholders, and BLOB and BYTEA both scan into []byte.
type Store struct {
	db *db.Conn
	// mu serialises writers, which ygo's CompactableAdapter contract asks for:
	// a compaction must not interleave with an append for the same document.
	mu sync.Mutex
	// Retain is how many snapshots per document are kept; older ones are
	// pruned when a new one is written.
	Retain int
	// onStored is told after each durable append, to schedule a projection.
	onStored func(documentID int64)
	now      func() time.Time
}

func NewStore(conn *db.Conn) *Store {
	return &Store{db: conn, Retain: defaultSnapshotRetain, now: time.Now}
}

func (s *Store) stamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// stateOf returns the compacted state and its projection token.
func stateOf(q querier, documentID int64) (state []byte, token string, exists bool, err error) {
	err = q.QueryRow(`SELECT state, projection_token FROM policy_doc_state WHERE document_id = ?`, documentID).Scan(&state, &token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", false, nil
	}
	return state, token, err == nil, err
}

// merged returns the state plus every update appended since, as one update.
func merged(q querier, documentID int64) ([]byte, int64, error) {
	state, _, _, err := stateOf(q, documentID)
	if err != nil {
		return nil, 0, fmt.Errorf("load state: %w", err)
	}
	parts := [][]byte{}
	if len(state) > 0 {
		parts = append(parts, state)
	}
	rows, err := q.Query(`SELECT id, upd FROM policy_doc_updates WHERE document_id = ? ORDER BY id`, documentID)
	if err != nil {
		return nil, 0, fmt.Errorf("load updates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var last int64
	for rows.Next() {
		var u []byte
		if err := rows.Scan(&last, &u); err != nil {
			return nil, 0, err
		}
		parts = append(parts, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	switch len(parts) {
	case 0:
		return nil, 0, nil
	case 1:
		return parts[0], last, nil
	}
	out, err := crdt.MergeUpdatesV1(parts...)
	if err != nil {
		return nil, 0, fmt.Errorf("merge updates: %w", err)
	}
	return out, last, nil
}

// Load returns a document's durable state as one update (nil when it has
// none).
func (s *Store) Load(documentID int64) ([]byte, error) {
	out, _, err := merged(s.db, documentID)
	return out, err
}

// Append stores one (coalesced) update.
func (s *Store) Append(documentID int64, update []byte) error {
	s.mu.Lock()
	_, err := s.db.Exec(`INSERT INTO policy_doc_updates (document_id, upd, created_at) VALUES (?, ?, ?)`,
		documentID, update, s.stamp())
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("append update: %w", err)
	}
	if s.onStored != nil {
		s.onStored(documentID)
	}
	return nil
}

// Compact folds the update log into the state row in one transaction.
func (s *Store) Compact(documentID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	state, last, err := merged(tx, documentID)
	if err != nil || last == 0 {
		return err
	}
	res, err := tx.Exec(`UPDATE policy_doc_state SET state = ?, updated_at = ? WHERE document_id = ?`, state, s.stamp(), documentID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// An update log with no state row can only exist if the seed was
		// lost; keep the log rather than compacting it into nothing.
		return fmt.Errorf("compact document %d: no state row", documentID)
	}
	if _, err := tx.Exec(`DELETE FROM policy_doc_updates WHERE document_id = ? AND id <= ?`, documentID, last); err != nil {
		return err
	}
	return tx.Commit()
}

// SeedIfAbsent writes a document's first state, exactly once: the state row
// is the guard, so of two concurrent seeds one inserts and the other finds the
// row already there and reports false. The document's projection token is set
// in the same transaction so the two start paired.
func (s *Store) SeedIfAbsent(documentID int64, update []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	token := policydocs.NewUID()
	res, err := tx.Exec(`INSERT INTO policy_doc_state (document_id, state, projection_token, updated_at)
		VALUES (?, ?, ?, ?) ON CONFLICT (document_id) DO NOTHING`, documentID, update, token, s.stamp())
	if err != nil {
		return false, fmt.Errorf("seed: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	if _, err := tx.Exec(`UPDATE policy_documents SET projection_token = ? WHERE id = ?`, token, documentID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// Replace discards a document's state for a new one, keeping the old state as
// a snapshot. It is used when the section rows changed outside the Studio.
func (s *Store) Replace(documentID int64, update []byte, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	old, _, err := merged(tx, documentID)
	if err != nil {
		return err
	}
	now := s.stamp()
	if len(old) > 0 {
		if _, err := tx.Exec(`INSERT INTO policy_doc_snapshots (document_id, reason, format, state, created_by, created_at)
			VALUES (?, ?, ?, ?, '', ?)`, documentID, reason, SnapshotFormatYjs, old, now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM policy_doc_updates WHERE document_id = ?`, documentID); err != nil {
		return err
	}
	token := policydocs.NewUID()
	if _, err := tx.Exec(`INSERT INTO policy_doc_state (document_id, state, projection_token, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (document_id) DO UPDATE SET state = excluded.state, projection_token = excluded.projection_token,
		updated_at = excluded.updated_at`, documentID, update, token, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE policy_documents SET projection_token = ? WHERE id = ?`, token, documentID); err != nil {
		return err
	}
	return tx.Commit()
}

// Snapshot records a point-in-time copy and prunes beyond Retain.
func (s *Store) Snapshot(documentID int64, reason, format, by string, data []byte) (int64, error) {
	if len(data) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.db.Insert(`INSERT INTO policy_doc_snapshots (document_id, reason, format, state, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, documentID, reason, format, data, by, s.stamp())
	if err != nil {
		return 0, fmt.Errorf("snapshot: %w", err)
	}
	if s.Retain > 0 {
		// The pre-migration copy is the only record of the legacy text and is
		// never pruned.
		if _, err := s.db.Exec(`DELETE FROM policy_doc_snapshots WHERE document_id = ? AND reason <> ? AND id NOT IN (
			SELECT id FROM policy_doc_snapshots WHERE document_id = ? AND reason <> ? ORDER BY id DESC LIMIT ?)`,
			documentID, SnapshotPreMigration, documentID, SnapshotPreMigration, s.Retain); err != nil {
			return id, fmt.Errorf("prune snapshots: %w", err)
		}
	}
	return id, nil
}

// SnapshotInfo describes one snapshot, without its data.
type SnapshotInfo struct {
	ID        int64  `json:"id"`
	Reason    string `json:"reason"`
	Format    string `json:"format"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) ListSnapshots(documentID int64) ([]SnapshotInfo, error) {
	rows, err := s.db.Query(`SELECT id, reason, format, created_by, created_at FROM policy_doc_snapshots
		WHERE document_id = ? ORDER BY id DESC`, documentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []SnapshotInfo{}
	for rows.Next() {
		var si SnapshotInfo
		if err := rows.Scan(&si.ID, &si.Reason, &si.Format, &si.CreatedBy, &si.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

// stateToken returns the state row's projection token and whether the row
// exists.
func (s *Store) stateToken(documentID int64) (string, bool, error) {
	_, token, exists, err := stateOf(s.db, documentID)
	return token, exists, err
}

// ---- ygo persistence adapter ----

// adapter is what ygo calls. LoadDoc is where a document's first state is
// seeded and where rows changed outside the Studio are reconciled: ygo calls
// it once per room load, before any peer can see the document, and runs
// OnLoadDocument before attaching its own persistence -- so seeding anywhere
// later would never be stored.
type adapter struct {
	store *Store
	// prepare seeds or reconciles a document's state before it is loaded.
	prepare func(documentID int64) error
}

func (a adapter) LoadDoc(room string) ([]byte, error) {
	id, ok := DocumentIDOf(room)
	if !ok {
		return nil, fmt.Errorf("room %q is not a policy document", room)
	}
	if err := a.prepare(id); err != nil {
		return nil, err
	}
	return a.store.Load(id)
}

func (a adapter) StoreUpdate(room string, update []byte) error {
	id, ok := DocumentIDOf(room)
	if !ok {
		return fmt.Errorf("room %q is not a policy document", room)
	}
	return a.store.Append(id, update)
}

func (a adapter) Compact(_ context.Context, room string) error {
	id, ok := DocumentIDOf(room)
	if !ok {
		return nil
	}
	return a.store.Compact(id)
}

// SaveVersion is ygo's interval snapshot (Server.AutoVersionEvery): at most one
// per interval per room, and only when the room changed.
func (a adapter) SaveVersion(_ context.Context, room, _ string) (int64, error) {
	id, ok := DocumentIDOf(room)
	if !ok {
		return 0, nil
	}
	data, err := a.store.Load(id)
	if err != nil {
		return 0, err
	}
	return a.store.Snapshot(id, SnapshotInterval, SnapshotFormatYjs, "", data)
}

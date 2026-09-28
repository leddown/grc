// Package clientprofile holds the clients policies are written for and the
// facts about each: the anti-hallucination store POLICY_MODULE_FRAMEWORK.md
// §2.2 calls for. Names, roles, systems, retention periods, review triggers --
// anything a policy states about a client -- come from here, and a policy that
// refers to a fact nobody has recorded carries an unresolved token that blocks
// approval rather than a plausible guess.
//
// The consulting CRM lives on wintermute; a profile is this installation's own
// record of a client, with crm_ref noting the CRM client it corresponds to, so
// a document's facts never depend on another server being reachable.
package clientprofile

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"grc/internal/db"
)

// ErrNotFound is returned for an unknown profile or fact.
var ErrNotFound = errors.New("clientprofile: not found")

// ValidationError is a refusal the caller can fix.
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// Profile is one client.
type Profile struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CRMRef    string `json:"crm_ref"`
	Notes     string `json:"notes"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	FactCount int    `json:"fact_count"`
}

// Fact is one recorded fact about a client.
type Fact struct {
	ClientID  int64  `json:"client_id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	ValueType string `json:"value_type"`
	Source    string `json:"source"`
	UpdatedBy string `json:"updated_by"`
	UpdatedAt string `json:"updated_at"`
}

// KeyPattern is the shape of a fact key, shared with the Policy Studio's fact
// tokens: snake_case, starting with a letter.
var KeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ValueTypes are the accepted value types.
var ValueTypes = []string{"text", "number", "date", "duration", "list"}

const maxValue = 2000

// Service manages profiles and facts.
type Service struct {
	db  *db.Conn
	now func() time.Time
	// onFactsChanged is told when a client's facts change, so documents that
	// state them can be recomputed.
	onFactsChanged func(clientID int64)
}

func NewService(conn *db.Conn) *Service { return &Service{db: conn, now: time.Now} }

// OnFactsChanged registers the facts-changed callback. Call at wiring time.
func (s *Service) OnFactsChanged(fn func(clientID int64)) { s.onFactsChanged = fn }

func (s *Service) stamp() string { return s.now().UTC().Format(time.RFC3339) }

func (s *Service) List() ([]Profile, error) {
	rows, err := s.db.Query(`SELECT p.id, p.name, p.crm_ref, p.notes, p.created_at, p.updated_at,
		(SELECT COUNT(*) FROM client_profile_facts f WHERE f.client_id = p.id)
		FROM client_profiles p ORDER BY p.name, p.id`)
	if err != nil {
		return nil, fmt.Errorf("list client profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Profile{}
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.ID, &p.Name, &p.CRMRef, &p.Notes, &p.CreatedAt, &p.UpdatedAt, &p.FactCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) Get(id int64) (Profile, error) {
	var p Profile
	err := s.db.QueryRow(`SELECT p.id, p.name, p.crm_ref, p.notes, p.created_at, p.updated_at,
		(SELECT COUNT(*) FROM client_profile_facts f WHERE f.client_id = p.id)
		FROM client_profiles p WHERE p.id = ?`, id).Scan(&p.ID, &p.Name, &p.CRMRef, &p.Notes, &p.CreatedAt, &p.UpdatedAt, &p.FactCount)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func normalizeProfile(p *Profile) error {
	p.Name = strings.TrimSpace(p.Name)
	p.CRMRef = strings.TrimSpace(p.CRMRef)
	p.Notes = strings.TrimSpace(p.Notes)
	if p.Name == "" {
		return invalid("a client name is required")
	}
	if utf8.RuneCountInString(p.Name) > 200 || utf8.RuneCountInString(p.CRMRef) > 100 || utf8.RuneCountInString(p.Notes) > maxValue {
		return invalid("a field is too long")
	}
	return nil
}

func (s *Service) Create(p Profile) (Profile, error) {
	if err := normalizeProfile(&p); err != nil {
		return Profile{}, err
	}
	now := s.stamp()
	id, err := s.db.Insert(`INSERT INTO client_profiles (name, crm_ref, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		p.Name, p.CRMRef, p.Notes, now, now)
	if err != nil {
		return Profile{}, fmt.Errorf("create client profile: %w", err)
	}
	return s.Get(id)
}

func (s *Service) Update(id int64, p Profile) (Profile, error) {
	if err := normalizeProfile(&p); err != nil {
		return Profile{}, err
	}
	res, err := s.db.Exec(`UPDATE client_profiles SET name = ?, crm_ref = ?, notes = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.CRMRef, p.Notes, s.stamp(), id)
	if err != nil {
		return Profile{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Profile{}, ErrNotFound
	}
	return s.Get(id)
}

// Delete removes a profile and its facts. A profile a policy document still
// names is kept: its facts are what that document's tokens resolve to.
func (s *Service) Delete(id int64) error {
	var used int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM policy_documents WHERE client_profile_id = ?`, id).Scan(&used); err != nil {
		return err
	}
	if used > 0 {
		return invalid("%d policy document(s) use this client; choose another client for them first", used)
	}
	res, err := s.db.Exec(`DELETE FROM client_profiles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Facts returns a client's facts, keyed.
func (s *Service) Facts(clientID int64) (map[string]Fact, error) {
	out := map[string]Fact{}
	if clientID <= 0 {
		return out, nil
	}
	rows, err := s.db.Query(`SELECT client_id, key, value, value_type, source, updated_by, updated_at
		FROM client_profile_facts WHERE client_id = ? ORDER BY key`, clientID)
	if err != nil {
		return nil, fmt.Errorf("read client facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var f Fact
		if err := rows.Scan(&f.ClientID, &f.Key, &f.Value, &f.ValueType, &f.Source, &f.UpdatedBy, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out[f.Key] = f
	}
	return out, rows.Err()
}

// Values is Facts reduced to key -> value, for resolving tokens. An empty
// value counts as unresolved.
func (s *Service) Values(clientID int64) (map[string]string, error) {
	facts, err := s.Facts(clientID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(facts))
	for k, f := range facts {
		if strings.TrimSpace(f.Value) != "" {
			out[k] = f.Value
		}
	}
	return out, nil
}

// SetFact records one fact. actor comes from the session, never the body.
func (s *Service) SetFact(clientID int64, key, value, valueType, actor string) (Fact, error) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	valueType = strings.TrimSpace(valueType)
	if valueType == "" {
		valueType = "text"
	}
	if !KeyPattern.MatchString(key) {
		return Fact{}, invalid("a fact key is lower-case letters, digits and underscores, starting with a letter")
	}
	known := false
	for _, t := range ValueTypes {
		known = known || t == valueType
	}
	if !known {
		return Fact{}, invalid("unknown value type %q", valueType)
	}
	if utf8.RuneCountInString(value) > maxValue {
		return Fact{}, invalid("a fact value is at most %d characters", maxValue)
	}
	if _, err := s.Get(clientID); err != nil {
		return Fact{}, err
	}
	now := s.stamp()
	if _, err := s.db.Exec(`INSERT INTO client_profile_facts (client_id, key, value, value_type, source, updated_by, updated_at)
		VALUES (?, ?, ?, ?, 'manual', ?, ?)
		ON CONFLICT (client_id, key) DO UPDATE SET value = excluded.value, value_type = excluded.value_type,
		source = excluded.source, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		clientID, key, value, valueType, actor, now); err != nil {
		return Fact{}, fmt.Errorf("record client fact: %w", err)
	}
	s.changed(clientID)
	return Fact{ClientID: clientID, Key: key, Value: value, ValueType: valueType, Source: "manual", UpdatedBy: actor, UpdatedAt: now}, nil
}

func (s *Service) DeleteFact(clientID int64, key string) error {
	res, err := s.db.Exec(`DELETE FROM client_profile_facts WHERE client_id = ? AND key = ?`, clientID, key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.changed(clientID)
	return nil
}

func (s *Service) changed(clientID int64) {
	if s.onFactsChanged != nil {
		s.onFactsChanged(clientID)
	}
}

// ---- HTTP ----

// Handler serves the profile API under /policies/clients, inside the policy
// module's page gate: reads for anyone who can read policies (the values are
// on the documents already), writes for admins.
type Handler struct {
	service *Service
	actor   func(*gin.Context) string
}

func NewHandler(service *Service, actor func(*gin.Context) string) *Handler {
	return &Handler{service: service, actor: actor}
}

func (h *Handler) RegisterReadRoutes(r gin.IRouter) {
	r.GET("/policies/clients", h.list)
	r.GET("/policies/clients/:clientID/facts", h.facts)
}

func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.POST("/policies/clients", h.create)
	r.PUT("/policies/clients/:clientID", h.update)
	r.DELETE("/policies/clients/:clientID", h.delete)
	r.PUT("/policies/clients/:clientID/facts/:key", h.setFact)
	r.DELETE("/policies/clients/:clientID/facts/:key", h.deleteFact)
}

func (h *Handler) fail(c *gin.Context, err error) {
	var v ValidationError
	switch {
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "client or fact not found"})
	case errors.As(err, &v):
		c.JSON(http.StatusBadRequest, gin.H{"error": v.Msg})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "the client profile could not be saved; try again"})
	}
}

func clientID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("clientID"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid client id"})
		return 0, false
	}
	return id, true
}

func (h *Handler) list(c *gin.Context) {
	out, err := h.service.List()
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) facts(c *gin.Context) {
	id, ok := clientID(c)
	if !ok {
		return
	}
	if _, err := h.service.Get(id); err != nil {
		h.fail(c, err)
		return
	}
	facts, err := h.service.Facts(id)
	if err != nil {
		h.fail(c, err)
		return
	}
	out := make([]Fact, 0, len(facts))
	for _, f := range facts {
		out = append(out, f)
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) create(c *gin.Context) {
	var p Profile
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid client payload"})
		return
	}
	out, err := h.service.Create(p)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h *Handler) update(c *gin.Context) {
	id, ok := clientID(c)
	if !ok {
		return
	}
	var p Profile
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid client payload"})
		return
	}
	out, err := h.service.Update(id, p)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) delete(c *gin.Context) {
	id, ok := clientID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) setFact(c *gin.Context) {
	id, ok := clientID(c)
	if !ok {
		return
	}
	var in struct {
		Value     string `json:"value"`
		ValueType string `json:"value_type"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid fact payload"})
		return
	}
	f, err := h.service.SetFact(id, c.Param("key"), in.Value, in.ValueType, h.actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, f)
}

func (h *Handler) deleteFact(c *gin.Context) {
	id, ok := clientID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteFact(id, c.Param("key")); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

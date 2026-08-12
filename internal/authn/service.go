package authn

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"carelockconsulting/internal/db"
)

var (
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrNotFound            = errors.New("auth user not found")
	ErrAlreadyBootstrapped = errors.New("admin is already bootstrapped")
	ErrInvalidInput        = errors.New("invalid input")
	ErrLastAdmin           = errors.New("cannot remove the last admin user")
	ErrTooManyAttempts     = errors.New("too many failed login attempts")
	ErrSingleUserMode      = errors.New("single-user mode: an account already exists")
)

const (
	// minPasswordLength is deliberately low for internal single-user
	// development use; raise it before any multi-user or exposed deployment.
	minPasswordLength      = 5
	maxPasswordLength      = 256
	maxFailedLoginAttempts = 5
	loginLockoutWindow     = 15 * time.Minute
)

type User struct {
	ID           int64    `json:"id"`
	Username     string   `json:"username"`
	IsAdmin      bool     `json:"is_admin"`
	AllowedPages []string `json:"allowed_pages"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

type Session struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
}

type Service struct {
	db *db.Conn

	// singleUser refuses creation of a second account. On by default for
	// internal development; turn it off to restore normal multi-user
	// behaviour, which the rest of the package still fully supports.
	singleUser bool

	loginAttemptsMu sync.Mutex
	loginAttempts   map[string]*loginAttemptState
}

// loginAttemptState tracks recent failed login attempts for a single
// username, in-memory only. It resets on process restart, which is
// acceptable for an internal, single-process tool.
type loginAttemptState struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
}

func NewService(conn *db.Conn) *Service {
	return &Service{db: conn, singleUser: true, loginAttempts: make(map[string]*loginAttemptState)}
}

// SingleUserMode reports whether the one-account-only restriction is active.
// Callers use it to decide whether controls that need two distinct people —
// policy approval separation, for one — can be enforced at all.
func (s *Service) SingleUserMode() bool {
	return s.singleUser
}

// SetSingleUserMode toggles the one-account-only restriction.
func (s *Service) SetSingleUserMode(enabled bool) {
	s.singleUser = enabled
}

func (s *Service) HasUsers() (bool, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM auth_users`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Service) BootstrapAdmin(username, password string) (User, error) {
	hasUsers, err := s.HasUsers()
	if err != nil {
		return User{}, err
	}
	if hasUsers {
		return User{}, ErrAlreadyBootstrapped
	}
	return s.createUser(username, password, true, nil)
}

func (s *Service) CreateUser(username, password string, isAdmin bool) (User, error) {
	return s.createUser(username, password, isAdmin, nil)
}

func (s *Service) CreateUserWithAccess(username, password string, isAdmin bool, allowedPages []string) (User, error) {
	return s.createUser(username, password, isAdmin, allowedPages)
}

func (s *Service) createUser(username, password string, isAdmin bool, allowedPages []string) (User, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	password = strings.TrimSpace(password)
	if username == "" {
		return User{}, fmt.Errorf("%w: username is required", ErrInvalidInput)
	}
	// Single-user mode: the first account may be created (bootstrap), any
	// further account is refused. Checked before bcrypt to fail cheaply.
	if s.singleUser {
		hasUsers, err := s.HasUsers()
		if err != nil {
			return User{}, err
		}
		if hasUsers {
			return User{}, ErrSingleUserMode
		}
	}
	if len(password) < minPasswordLength {
		return User{}, fmt.Errorf("%w: password must be at least %d characters", ErrInvalidInput, minPasswordLength)
	}
	if len(password) > maxPasswordLength {
		return User{}, fmt.Errorf("%w: password must be at most %d characters", ErrInvalidInput, maxPasswordLength)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	normalizedPages, err := normalizeAllowedPages(allowedPages)
	if err != nil {
		return User{}, err
	}
	allowedPagesJSON, err := json.Marshal(normalizedPages)
	if err != nil {
		return User{}, err
	}
	id, err := s.db.Insert(
		`INSERT INTO auth_users (username, password_hash, is_admin, allowed_pages_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		username, string(hash), boolToInt(isAdmin), string(allowedPagesJSON), now, now,
	)
	if err != nil {
		return User{}, err
	}
	return User{ID: id, Username: username, IsAdmin: isAdmin, AllowedPages: normalizedPages, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Service) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, is_admin, allowed_pages_json, created_at, updated_at FROM auth_users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]User, 0)
	for rows.Next() {
		var item User
		var isAdmin int
		var allowedPagesJSON string
		if err := rows.Scan(&item.ID, &item.Username, &isAdmin, &allowedPagesJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.IsAdmin = isAdmin == 1
		item.AllowedPages = parseAllowedPagesJSON(allowedPagesJSON)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) UpdateUser(username string, newPassword *string, isAdmin *bool, allowedPages *[]string) (User, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	if username == "" {
		return User{}, fmt.Errorf("%w: username is required", ErrInvalidInput)
	}
	user, hash, err := s.getUserWithHash(username)
	if err != nil {
		return User{}, err
	}
	if newPassword != nil {
		password := strings.TrimSpace(*newPassword)
		if len(password) < minPasswordLength {
			return User{}, fmt.Errorf("%w: password must be at least %d characters", ErrInvalidInput, minPasswordLength)
		}
		if len(password) > maxPasswordLength {
			return User{}, fmt.Errorf("%w: password must be at most %d characters", ErrInvalidInput, maxPasswordLength)
		}
		encoded, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return User{}, err
		}
		hash = string(encoded)
	}
	if isAdmin != nil {
		if user.IsAdmin && !*isAdmin {
			count, err := s.adminCountExcluding(user.Username)
			if err != nil {
				return User{}, err
			}
			if count == 0 {
				return User{}, ErrLastAdmin
			}
		}
		user.IsAdmin = *isAdmin
	}
	if allowedPages != nil {
		normalizedPages, err := normalizeAllowedPages(*allowedPages)
		if err != nil {
			return User{}, err
		}
		user.AllowedPages = normalizedPages
	}
	allowedPagesJSON, err := json.Marshal(user.AllowedPages)
	if err != nil {
		return User{}, err
	}
	user.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(
		`UPDATE auth_users SET password_hash = ?, is_admin = ?, allowed_pages_json = ?, updated_at = ? WHERE username = ?`,
		hash, boolToInt(user.IsAdmin), string(allowedPagesJSON), user.UpdatedAt, user.Username,
	); err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Service) DeleteUser(username string) error {
	username = strings.TrimSpace(strings.ToLower(username))
	user, _, err := s.getUserWithHash(username)
	if err != nil {
		return err
	}
	if user.IsAdmin {
		soleAccount, err := s.isSoleAccount()
		if err != nil {
			return err
		}
		// In single-user mode the last-admin guard only strands the operator:
		// deleting the one account empties auth_users, and bootstrap can then
		// create a fresh admin. Deleting the last *admin* while other
		// non-admin accounts remain is still refused, because bootstrap
		// refuses to run on a non-empty table and single-user mode blocks
		// CreateUser — that combination is unrecoverable without raw SQL.
		if !(s.singleUser && soleAccount) {
			count, err := s.adminCountExcluding(user.Username)
			if err != nil {
				return err
			}
			if count == 0 {
				return ErrLastAdmin
			}
		}
	}

	result, err := s.db.Exec(`DELETE FROM auth_users WHERE username = ?`, username)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// dummyPasswordHash is compared against on unknown usernames so that
// Authenticate takes roughly the same time whether or not the account
// exists, preventing username enumeration via response timing.
var dummyPasswordHash = mustGenerateDummyHash()

func mustGenerateDummyHash() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-constant-time-auth"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return hash
}

func (s *Service) Authenticate(username, password string) (User, error) {
	key := strings.TrimSpace(strings.ToLower(username))
	if retryAfter, locked := s.lockoutRemaining(key); locked {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		return User{}, fmt.Errorf("%w: try again in %s", ErrTooManyAttempts, retryAfter.Round(time.Second))
	}

	user, hash, err := s.getUserWithHash(key)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		s.recordFailedLogin(key)
		return User{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		s.recordFailedLogin(key)
		return User{}, ErrInvalidCredentials
	}
	s.clearFailedLogins(key)
	return user, nil
}

// lockoutRemaining reports whether key is currently locked out after too
// many failed attempts, and if so how much longer the lockout lasts.
func (s *Service) lockoutRemaining(key string) (time.Duration, bool) {
	s.loginAttemptsMu.Lock()
	defer s.loginAttemptsMu.Unlock()
	state := s.loginAttempts[key]
	if state == nil {
		return 0, false
	}
	if remaining := time.Until(state.lockedUntil); remaining > 0 {
		return remaining, true
	}
	return 0, false
}

func (s *Service) recordFailedLogin(key string) {
	s.loginAttemptsMu.Lock()
	defer s.loginAttemptsMu.Unlock()
	now := time.Now()
	// Evict fully-expired entries for other keys to prevent unbounded growth
	// under credential-stuffing with many unique usernames.
	for k, st := range s.loginAttempts {
		if k != key && now.Sub(st.windowStart) > loginLockoutWindow && time.Until(st.lockedUntil) <= 0 {
			delete(s.loginAttempts, k)
		}
	}
	state := s.loginAttempts[key]
	if state == nil || now.Sub(state.windowStart) > loginLockoutWindow {
		state = &loginAttemptState{windowStart: now}
		s.loginAttempts[key] = state
	}
	state.count++
	if state.count >= maxFailedLoginAttempts {
		state.lockedUntil = now.Add(loginLockoutWindow)
	}
}

func (s *Service) clearFailedLogins(key string) {
	s.loginAttemptsMu.Lock()
	defer s.loginAttemptsMu.Unlock()
	delete(s.loginAttempts, key)
}

func (s *Service) CreateSession(userID int64, ttl time.Duration) (Session, error) {
	token, err := randomToken(32)
	if err != nil {
		return Session{}, err
	}
	expiresAt := time.Now().UTC().Add(ttl)
	if _, err := s.db.Exec(
		`INSERT INTO auth_sessions (session_token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, expiresAt.Format(time.RFC3339Nano),
	); err != nil {
		return Session{}, err
	}
	return Session{Token: token, UserID: userID, ExpiresAt: expiresAt}, nil
}

func (s *Service) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE session_token = ?`, strings.TrimSpace(token))
	return err
}

func (s *Service) GetSessionUser(token string) (User, error) {
	var user User
	var isAdmin int
	var allowedPagesJSON string
	var expiresRaw string
	err := s.db.QueryRow(
		`SELECT u.id, u.username, u.is_admin, u.allowed_pages_json, u.created_at, u.updated_at, s.expires_at
		FROM auth_sessions s
		INNER JOIN auth_users u ON u.id = s.user_id
		WHERE s.session_token = ?`,
		strings.TrimSpace(token),
	).Scan(&user.ID, &user.Username, &isAdmin, &allowedPagesJSON, &user.CreatedAt, &user.UpdatedAt, &expiresRaw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrInvalidCredentials
		}
		return User{}, err
	}
	user.IsAdmin = isAdmin == 1
	user.AllowedPages = parseAllowedPagesJSON(allowedPagesJSON)
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresRaw)
	if err != nil {
		return User{}, err
	}
	if time.Now().UTC().After(expiresAt) {
		_ = s.DeleteSession(token)
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (s *Service) getUserWithHash(username string) (User, string, error) {
	var user User
	var hash string
	var isAdmin int
	var allowedPagesJSON string
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, is_admin, allowed_pages_json, created_at, updated_at FROM auth_users WHERE username = ?`,
		username,
	).Scan(&user.ID, &user.Username, &hash, &isAdmin, &allowedPagesJSON, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, "", ErrNotFound
		}
		return User{}, "", err
	}
	user.IsAdmin = isAdmin == 1
	user.AllowedPages = parseAllowedPagesJSON(allowedPagesJSON)
	return user, hash, nil
}

func randomToken(numBytes int) (string, error) {
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// isSoleAccount reports whether exactly one auth user exists.
func (s *Service) isSoleAccount() (bool, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM auth_users`).Scan(&count); err != nil {
		return false, fmt.Errorf("count auth users: %w", err)
	}
	return count == 1, nil
}

func (s *Service) adminCountExcluding(username string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM auth_users WHERE is_admin = 1 AND username <> ?`, strings.TrimSpace(strings.ToLower(username))).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func parseAllowedPagesJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var pages []string
	if err := json.Unmarshal([]byte(raw), &pages); err != nil {
		return []string{}
	}
	normalized, _ := normalizeAllowedPages(pages)
	return normalized
}

func normalizeAllowedPages(pages []string) ([]string, error) {
	if pages == nil {
		return []string{}, nil
	}
	seen := make(map[string]struct{}, len(pages))
	out := make([]string, 0, len(pages))
	for _, page := range pages {
		item := strings.TrimSpace(page)
		if item == "" {
			continue
		}
		if !strings.HasPrefix(item, "/") {
			return nil, fmt.Errorf("%w: page path must start with '/'", ErrInvalidInput)
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out, nil
}

package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

const (
	pbkdf2Iterations = 100000
	pbkdf2KeyLen     = 32
	pbkdf2SaltLen    = 16
)

// HashPassword hashes a plain-text password using the specified method ("bcrypt" or "pbkdf2").
func HashPassword(password, method string) (string, error) {
	switch strings.ToLower(method) {
	case "bcrypt":
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return "", fmt.Errorf("bcrypt hash failed: %w", err)
		}
		return string(hash), nil
	case "pbkdf2", "":
		salt := make([]byte, pbkdf2SaltLen)
		if _, err := rand.Read(salt); err != nil {
			return "", fmt.Errorf("failed to generate random salt: %w", err)
		}
		key := pbkdf2.Key([]byte(password), salt, pbkdf2Iterations, pbkdf2KeyLen, sha256.New)
		return fmt.Sprintf("pbkdf2:sha256:%d:%s:%s", pbkdf2Iterations, hex.EncodeToString(salt), hex.EncodeToString(key)), nil
	default:
		return "", fmt.Errorf("unsupported hashing method: %s", method)
	}
}

// CheckPassword verifies a plain-text password against a bcrypt or PBKDF2 hash.
func CheckPassword(password, hash string) bool {
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}

	if strings.HasPrefix(hash, "pbkdf2:") {
		parts := strings.Split(hash, ":")
		if len(parts) != 5 {
			return false
		}
		iter, err := strconv.Atoi(parts[2])
		if err != nil || iter <= 0 {
			return false
		}
		salt, err := hex.DecodeString(parts[3])
		if err != nil {
			return false
		}
		expectedKey, err := hex.DecodeString(parts[4])
		if err != nil {
			return false
		}

		key := pbkdf2.Key([]byte(password), salt, iter, len(expectedKey), sha256.New)
		return subtle.ConstantTimeCompare(key, expectedKey) == 1
	}

	return false
}

// IsLocalAddress checks if the given host or host:port string originates from a local or private network.
func IsLocalAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	// Clean IPv6 brackets if present
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	return false
}

// session holds authenticated session data.
type session struct {
	username  string
	csrfToken string
	expiresAt time.Time
}

// SessionStore maintains in-memory authenticated user sessions.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]session
	ttl      time.Duration
}

// NewSessionStore creates a thread-safe SessionStore.
func NewSessionStore(ttl time.Duration) *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]session),
		ttl:      ttl,
	}
	return store
}

// Create generates a cryptographically random session token and stores the session.
func (s *SessionStore) Create(username string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)

	cb := make([]byte, 32)
	_, _ = rand.Read(cb)
	csrfToken := hex.EncodeToString(cb)

	s.sessions[token] = session{
		username:  username,
		csrfToken: csrfToken,
		expiresAt: time.Now().Add(s.ttl),
	}
	return token
}

// CSRFToken returns the CSRF token associated with an active session.
func (s *SessionStore) CSRFToken(token string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[token]
	if !ok || time.Now().After(sess.expiresAt) {
		return ""
	}
	return sess.csrfToken
}

// ValidateCSRF checks if the provided CSRF token matches the session.
func (s *SessionStore) ValidateCSRF(token, csrfToken string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[token]
	if !ok || time.Now().After(sess.expiresAt) {
		return false
	}
	if csrfToken == "" || sess.csrfToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(csrfToken), []byte(sess.csrfToken)) == 1
}

// Validate checks if a session token is active and returns the username.
func (s *SessionStore) Validate(token string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[token]
	if !ok {
		return "", false
	}
	if time.Now().After(sess.expiresAt) {
		return "", false
	}
	return sess.username, true
}

// Delete removes a session token.
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// Authenticator provides user authentication operations against the database.
type Authenticator struct {
	sessions *SessionStore
}

// NewAuthenticator creates a new Authenticator.
func NewAuthenticator(ttl time.Duration) *Authenticator {
	return &Authenticator{
		sessions: NewSessionStore(ttl),
	}
}

// ErrInvalidCredentials indicates username or password mismatch.
var ErrInvalidCredentials = errors.New("invalid username or password")

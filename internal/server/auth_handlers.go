package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleSetup creates the initial administrator user when no users exist.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		s.writeJSONError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	hash, err := HashPassword(req.Password, "pbkdf2")
	if err != nil {
		log.Printf("failed to hash password: %v", err)
		s.writeJSONError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	err = s.db.Write(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		var userCount int
		if queryErr := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users;").Scan(&userCount); queryErr != nil {
			return queryErr
		}
		if userCount > 0 {
			return errors.New("setup already completed")
		}

		_, execErr := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", req.Username, hash)
		return execErr
	})
	if err != nil {
		if err.Error() == "setup already completed" {
			s.writeJSONError(w, http.StatusBadRequest, "setup already completed")
			return
		}
		log.Printf("failed to create user: %v", err)
		s.writeJSONError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	token := s.sessions.Create(req.Username)
	csrfToken := s.sessions.CSRFToken(token)
	s.setSessionCookie(w, r, token, csrfToken)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"username":  req.Username,
		"csrfToken": csrfToken,
	})
}

// handleLogin validates credentials and creates an authenticated session.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		s.writeJSONError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	var storedHash string
	err := s.db.Read(r.Context(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username = ?;", req.Username).Scan(&storedHash)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeJSONError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		log.Printf("database query failed during login: %v", err)
		s.writeJSONError(w, http.StatusInternalServerError, "database error")
		return
	}

	if !CheckPassword(req.Password, storedHash) {
		s.writeJSONError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	token := s.sessions.Create(req.Username)
	csrfToken := s.sessions.CSRFToken(token)
	s.setSessionCookie(w, r, token, csrfToken)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"username":  req.Username,
		"csrfToken": csrfToken,
	})
}

// handleLogout clears the authenticated session token and invalidates cookies.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		s.sessions.Delete(cookie.Value)
	}

	s.clearSessionCookie(w, r)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// handleCurrentUser returns authentication status and session details for the requesting user.
func (s *Server) handleCurrentUser(w http.ResponseWriter, r *http.Request) {
	username, _ := r.Context().Value(userContextKey).(string)

	var csrfToken string
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		csrfToken = s.sessions.CSRFToken(cookie.Value)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"username":      username,
		"authenticated": username != "",
		"csrfToken":     csrfToken,
	})
}

// setSessionCookie configures HTTP-only session and CSRF cookies, honoring TLS and reverse proxy HTTPS.
func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token, csrfToken string) {
	path := "/"
	if s.cleanURLBase != "" {
		path = s.cleanURLBase + "/"
	}

	ssl := s.isSslEnabled() || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     path,
		HttpOnly: true,
		Secure:   ssl,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})

	if csrfToken != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     csrfCookieName,
			Value:    csrfToken,
			Path:     path,
			HttpOnly: false,
			Secure:   ssl,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(24 * time.Hour),
		})
	}
}

// clearSessionCookie removes active session and CSRF cookies.
func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	path := "/"
	if s.cleanURLBase != "" {
		path = s.cleanURLBase + "/"
	}

	ssl := s.isSslEnabled() || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     path,
		HttpOnly: true,
		Secure:   ssl,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})

	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     path,
		HttpOnly: false,
		Secure:   ssl,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

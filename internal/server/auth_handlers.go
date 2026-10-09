package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

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

	var userCount int
	err := s.db.Read(r.Context(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM users;").Scan(&userCount)
	})
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "failed to query users: "+err.Error())
		return
	}
	if userCount > 0 {
		s.writeJSONError(w, http.StatusBadRequest, "setup already completed")
		return
	}

	hash, err := HashPassword(req.Password, "pbkdf2")
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "failed to hash password: "+err.Error())
		return
	}

	err = s.db.Write(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, execErr := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", req.Username, hash)
		return execErr
	})
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "failed to create user: "+err.Error())
		return
	}

	token := s.sessions.Create(req.Username)
	s.setSessionCookie(w, token)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"username": req.Username,
	})
}

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
		s.writeJSONError(w, http.StatusInternalServerError, "database error: "+err.Error())
		return
	}

	if !CheckPassword(req.Password, storedHash) {
		s.writeJSONError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	token := s.sessions.Create(req.Username)
	s.setSessionCookie(w, token)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"username": req.Username,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		s.sessions.Delete(cookie.Value)
	}

	s.clearSessionCookie(w)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func (s *Server) handleCurrentUser(w http.ResponseWriter, r *http.Request) {
	username, _ := r.Context().Value(userContextKey).(string)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"username":      username,
		"authenticated": username != "",
	})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	path := "/"
	if s.cleanURLBase != "" {
		path = s.cleanURLBase + "/"
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     path,
		HttpOnly: true,
		Secure:   s.cfg.EnableSsl,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	path := "/"
	if s.cleanURLBase != "" {
		path = s.cleanURLBase + "/"
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     path,
		HttpOnly: true,
		Secure:   s.cfg.EnableSsl,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

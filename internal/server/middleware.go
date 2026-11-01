package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

const sessionCookieName = "anilist_sync_session"
const csrfCookieName = "anilist_sync_csrf"

type contextKey string

const userContextKey = contextKey("user")

// authMiddleware handles user authentication across Forms, Basic, and Local LAN bypass.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if s.cleanURLBase != "" {
			path = strings.TrimPrefix(path, s.cleanURLBase)
		}

		// Public endpoints that do not require authentication (static assets, client-side SPA routes, health, and auth login/setup)
		if !strings.HasPrefix(path, "/api/") || path == "/api/v1/health" || path == "/api/v1/auth/login" || path == "/api/v1/auth/setup" {
			next.ServeHTTP(w, r)
			return
		}

		// 1. API Key authentication via X-Api-Key header
		reqAPIKey := r.Header.Get("X-Api-Key")
		apiKey := s.getAPIKey()
		if reqAPIKey != "" && apiKey != "" {
			if subtle.ConstantTimeCompare([]byte(reqAPIKey), []byte(apiKey)) == 1 {
				ctx := context.WithValue(r.Context(), userContextKey, "apikey_user")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 2. Local LAN address bypass
		if s.getAuthRequired() == "DisabledForLocalAddresses" {
			clientIP := s.getClientIP(r)
			if clientIP != nil && IsLocalIP(clientIP) {
				ctx := context.WithValue(r.Context(), userContextKey, "local_user")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 3. Forms session cookie authentication
		if s.getAuthMethod() == "Forms" {
			cookie, err := r.Cookie(sessionCookieName)
			if err == nil && cookie.Value != "" {
				if username, ok := s.sessions.Validate(cookie.Value); ok {
					ctx := context.WithValue(r.Context(), userContextKey, username)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		}

		// 5. Basic HTTP popup authentication
		if s.getAuthMethod() == "Basic" {
			user, pass, ok := r.BasicAuth()
			if ok && s.db != nil {
				var storedHash string
				err := s.db.Read(r.Context(), func(ctx context.Context, q storage.Querier) error {
					return q.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username = ?;", user).Scan(&storedHash)
				})
				if err == nil && CheckPassword(pass, storedHash) {
					ctx := context.WithValue(r.Context(), userContextKey, user)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="AniList-Arr-Sync"`)
			s.writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.writeJSONError(w, http.StatusUnauthorized, "unauthorized")
	})
}

// csrfMiddleware verifies X-Api-Key or session CSRF token on all state-mutating requests to protect against CSRF attacks.
func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
			next.ServeHTTP(w, r)
			return
		}

		path := r.URL.Path
		if s.cleanURLBase != "" {
			path = strings.TrimPrefix(path, s.cleanURLBase)
		}

		// Login and initial setup endpoints are exempt from CSRF validation
		if path == "/api/v1/auth/login" || path == "/api/v1/auth/setup" {
			next.ServeHTTP(w, r)
			return
		}

		// Basic auth mode is exempt from session CSRF validation
		if s.getAuthMethod() == "Basic" {
			next.ServeHTTP(w, r)
			return
		}

		// 1. Check API Key header
		reqAPIKey := r.Header.Get("X-Api-Key")
		serverAPIKey := s.getAPIKey()
		if reqAPIKey != "" && serverAPIKey != "" && subtle.ConstantTimeCompare([]byte(reqAPIKey), []byte(serverAPIKey)) == 1 {
			next.ServeHTTP(w, r)
			return
		}

		// 2. Check session CSRF token (for Forms browser users)
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil && cookie.Value != "" {
			csrfToken := r.Header.Get("X-CSRF-Token")
			if csrfToken != "" && s.sessions.ValidateCSRF(cookie.Value, csrfToken) {
				next.ServeHTTP(w, r)
				return
			}
		}

		s.writeJSONError(w, http.StatusUnauthorized, "missing or invalid X-Api-Key or X-CSRF-Token header")
	})
}

func (s *Server) writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// allowedHostsMiddleware validates incoming HTTP Host headers against AllowedHosts to protect against DNS rebinding.
func (s *Server) allowedHostsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isHostAllowed(r.Host, s.getAllowedHosts()) {
			s.writeJSONError(w, http.StatusBadRequest, "invalid host header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isHostAllowed checks if requestHost matches the comma-separated allowedHosts whitelist.
// If allowedHosts is empty or "*", all hosts are permitted.
func isHostAllowed(requestHost, allowedHosts string) bool {
	allowedHosts = strings.TrimSpace(allowedHosts)
	if allowedHosts == "" || allowedHosts == "*" {
		return true
	}

	host, _, err := net.SplitHostPort(requestHost)
	if err != nil {
		host = requestHost
	}
	host = strings.ToLower(strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(host), "]"), "["))

	for _, entry := range strings.Split(allowedHosts, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if entry == "*" {
			return true
		}
		entryHost, _, err := net.SplitHostPort(entry)
		if err == nil {
			entry = entryHost
		}
		entry = strings.TrimPrefix(strings.TrimSuffix(entry, "]"), "[")

		if entry == host {
			return true
		}
		if strings.HasPrefix(entry, "*.") {
			suffix := entry[1:] // e.g. ".example.com"
			if strings.HasSuffix(host, suffix) && host != suffix {
				return true
			}
		}
	}

	return false
}

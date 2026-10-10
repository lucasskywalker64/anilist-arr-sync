package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

const sessionCookieName = "anilist_sync_session"
const csrfCookieName = "anilist_sync_csrf"

type contextKey string

const userContextKey = contextKey("user")

// authMiddleware handles user authentication across Forms, External headers, and Local LAN bypass.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if s.cleanURLBase != "" {
			path = strings.TrimPrefix(path, s.cleanURLBase)
		}

		// Public endpoints that do not require authentication
		if path == "" || path == "/" || path == "/api/v1/health" || path == "/api/v1/auth/login" || path == "/api/v1/auth/setup" {
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

		// 2. External reverse proxy headers (Authelia / Authentik)
		if s.getAuthMethod() == "External" {
			if !IsLocalAddress(r.RemoteAddr) {
				s.writeJSONError(w, http.StatusUnauthorized, "untrusted client address for external authentication")
				return
			}
			remoteUser := r.Header.Get("Remote-User")
			if remoteUser == "" {
				remoteUser = r.Header.Get("X-authentik-username")
			}
			if remoteUser != "" {
				ctx := context.WithValue(r.Context(), userContextKey, remoteUser)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			s.writeJSONError(w, http.StatusUnauthorized, "external authentication header missing or invalid")
			return
		}

		// 3. Local LAN address bypass (applies to Forms authentication)
		if s.getAuthRequired() == "DisabledForLocalAddresses" {
			if IsLocalAddress(r.RemoteAddr) {
				ctx := context.WithValue(r.Context(), userContextKey, "local_user")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 4. Forms session cookie authentication
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
			if csrfToken == "" {
				csrfToken = reqAPIKey
			}
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

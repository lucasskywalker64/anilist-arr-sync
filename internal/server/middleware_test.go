package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestAuthMiddleware(t *testing.T) {
	cfg := config.NewDefault()
	cfg.AuthenticationMethod = "Forms"
	cfg.AuthenticationRequired = "Enabled"
	cfg.APIKey = "secret-api-key-12345"

	sessions := NewSessionStore(1 * time.Hour)
	token := sessions.Create("admin")

	srv := &Server{
		cfg:      cfg,
		sessions: sessions,
	}

	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	// 1. Health endpoint bypasses auth
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected health check to return 200, got %d", rec.Code)
		}
	}

	// 2. Unauthenticated GET on protected route returns 401
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected unauthenticated request to return 401, got %d", rec.Code)
		}
	}

	// 3. Authenticated via valid session cookie
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected authenticated session request to return 200, got %d", rec.Code)
		}
	}

	// 4. Authenticated via X-Api-Key header
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.Header.Set("X-Api-Key", "secret-api-key-12345")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected valid api key to return 200, got %d", rec.Code)
		}
	}
}

func TestLocalLanBypass(t *testing.T) {
	cfg := config.NewDefault()
	cfg.AuthenticationMethod = "Forms"
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "secret-api-key"

	srv := &Server{
		cfg:      cfg,
		sessions: NewSessionStore(1 * time.Hour),
	}

	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Local IP bypasses auth
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "192.168.1.100:45678"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected local address to bypass auth, got %d", rec.Code)
		}
	}

	// Public IP requires auth
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "198.51.100.1:45678"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected public address to require auth, got %d", rec.Code)
		}
	}
}

func TestExternalAuthHeaders(t *testing.T) {
	cfg := config.NewDefault()
	cfg.AuthenticationMethod = "External"
	cfg.AuthenticationRequired = "Enabled"
	cfg.APIKey = "secret-api-key"

	srv := &Server{
		cfg:      cfg,
		sessions: NewSessionStore(1 * time.Hour),
	}

	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Authelia Remote-User header from trusted local proxy
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Header.Set("Remote-User", "authelia_user")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected Authelia Remote-User to authenticate, got %d", rec.Code)
		}
	}

	// Authentik X-authentik-username header from trusted local proxy
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Header.Set("X-authentik-username", "authentik_user")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected Authentik header to authenticate, got %d", rec.Code)
		}
	}

	// External header from untrusted public IP is rejected
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "198.51.100.1:1234"
		req.Header.Set("Remote-User", "hacker")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected spoofed external header from untrusted IP to return 401, got %d", rec.Code)
		}
	}

	// Missing external headers
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected missing external header to return 401, got %d", rec.Code)
		}
	}
}

func TestCSRFProtection_ApiKeyRequiredForMutatingRequests(t *testing.T) {
	cfg := config.NewDefault()
	cfg.AuthenticationMethod = "Forms"
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "valid-api-key-xyz"

	srv := &Server{
		cfg:      cfg,
		sessions: NewSessionStore(1 * time.Hour),
	}

	handler := srv.csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mutated"))
	}))

	// Safe GET request passes without API key
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected GET to pass without X-Api-Key, got %d", rec.Code)
		}
	}

	// State-mutating POST request fails without X-Api-Key
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/review/1/resolve", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected POST without X-Api-Key to return 401, got %d", rec.Code)
		}
	}

	// State-mutating POST request fails with wrong X-Api-Key
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/review/1/resolve", nil)
		req.Header.Set("X-Api-Key", "wrong-key")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected POST with invalid X-Api-Key to return 401, got %d", rec.Code)
		}
	}

	// State-mutating POST request succeeds with valid X-Api-Key
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/review/1/resolve", nil)
		req.Header.Set("X-Api-Key", "valid-api-key-xyz")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected POST with valid X-Api-Key to return 200, got %d", rec.Code)
		}
	}

	// Login POST request exempt from X-Api-Key
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected login POST to bypass X-Api-Key check, got %d", rec.Code)
		}
	}
}

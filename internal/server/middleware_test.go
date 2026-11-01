package server

import (
	"context"
	"database/sql"
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

func TestBasicAuth(t *testing.T) {
	db := newTestStorage(t)
	hash, err := HashPassword("SecretPass123!", "pbkdf2")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	_ = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO users (id, username, password_hash) VALUES (1, 'basicuser', ?);", hash)
		return err
	})

	cfg := config.NewDefault()
	cfg.AuthenticationMethod = "Basic"
	cfg.AuthenticationRequired = "Enabled"
	cfg.APIKey = "secret-api-key"

	srv := &Server{
		cfg:      cfg,
		db:       db,
		sessions: NewSessionStore(1 * time.Hour),
	}

	var capturedUser string
	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := r.Context().Value(userContextKey).(string); ok {
			capturedUser = u
		}
		w.WriteHeader(http.StatusOK)
	}))

	// 1. Missing Basic auth header returns 401 with WWW-Authenticate
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 without basic auth, got %d", rec.Code)
		}
		if authHeader := rec.Header().Get("WWW-Authenticate"); authHeader == "" {
			t.Errorf("expected WWW-Authenticate header, got empty")
		}
	}

	// 2. Incorrect password returns 401
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.SetBasicAuth("basicuser", "WrongPassword")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 with wrong password, got %d", rec.Code)
		}
	}

	// 3. Correct credentials succeeds and sets user context
	{
		capturedUser = ""
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.SetBasicAuth("basicuser", "SecretPass123!")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 with valid basic auth, got %d", rec.Code)
		}
		if capturedUser != "basicuser" {
			t.Errorf("expected user 'basicuser', got %q", capturedUser)
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

func TestLocalLanBypass_ReverseProxyXForwardedFor(t *testing.T) {
	cfg := config.NewDefault()
	cfg.AuthenticationMethod = "Forms"
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "secret-api-key"

	trustedNets, err := ParseCIDRs(cfg.TrustedNetworks)
	if err != nil {
		t.Fatalf("failed to parse default trusted networks: %v", err)
	}

	srv := &Server{
		cfg:         cfg,
		trustedNets: trustedNets,
		sessions:    NewSessionStore(1 * time.Hour),
	}

	var capturedUser string
	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := r.Context().Value(userContextKey).(string); ok {
			capturedUser = u
		}
		w.WriteHeader(http.StatusOK)
	}))

	// 1. Untrusted remote RemoteAddr (203.0.113.195) with local X-Forwarded-For (192.168.1.50)
	// Spoofed XFF must be ignored, client IP is 203.0.113.195 -> rejected with HTTP 401.
	{
		capturedUser = ""
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "203.0.113.195:54321"
		req.Header.Set("X-Forwarded-For", "192.168.1.50")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected untrusted remote with spoofed XFF to return 401, got %d", rec.Code)
		}
	}

	// 2. Trusted remote RemoteAddr (127.0.0.1) with external X-Forwarded-For (203.0.113.195)
	// External client IP is resolved -> rejected with HTTP 401 (triggers login challenge, does not bypass).
	{
		capturedUser = ""
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Header.Set("X-Forwarded-For", "203.0.113.195")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected trusted remote with external XFF to return 401, got %d", rec.Code)
		}
	}

	// 3. Trusted remote RemoteAddr (127.0.0.1) with local LAN X-Forwarded-For (192.168.1.50)
	// Local client IP is resolved -> successfully bypasses auth with HTTP 200 as local_user.
	{
		capturedUser = ""
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Header.Set("X-Forwarded-For", "192.168.1.50")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected trusted remote with local XFF to return 200, got %d", rec.Code)
		}
		if capturedUser != "local_user" {
			t.Errorf("expected context user 'local_user', got %q", capturedUser)
		}
	}
}

func TestAllowedHostsMiddleware(t *testing.T) {
	cfg := config.NewDefault()
	srv := &Server{
		cfg: cfg,
	}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := srv.allowedHostsMiddleware(nextHandler)

	// 1. Wildcard "*" allows any host
	{
		cfg.AllowedHosts = "*"
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.Host = "arbitrary.attacker.com:7171"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected wildcard AllowedHosts to return 200, got %d", rec.Code)
		}
	}

	// 2. Specific host whitelist allows matching host (with and without port)
	{
		cfg.AllowedHosts = "localhost, myserver.lan, *.internal.net"

		// Matching host with port
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.Host = "localhost:7171"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected matching localhost to return 200, got %d", rec.Code)
		}

		// Matching secondary host
		req = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.Host = "myserver.lan"
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected matching myserver.lan to return 200, got %d", rec.Code)
		}

		// Matching wildcard subdomain
		req = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.Host = "sub.internal.net"
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected matching sub.internal.net to return 200, got %d", rec.Code)
		}

		// Disallowed host rejected with 400 Bad Request
		req = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.Host = "evil-rebinding.com"
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected disallowed host to return 400 Bad Request, got %d", rec.Code)
		}
	}
}

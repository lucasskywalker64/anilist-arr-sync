package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestSavingSSLSettingsPreservesSessionUntilRestart(t *testing.T) {
	for _, enableSSL := range []bool{false, true} {
		name := "enable"
		if enableSSL {
			name = "disable"
		}
		t.Run(name, func(t *testing.T) {
			cfg := config.NewDefault()
			cfg.AuthenticationRequired = "Enabled"
			cfg.EnableSsl = enableSSL
			cfg.SslCertPath, cfg.SslKeyPath = "localhost.crt", "localhost.key"
			db := newTestStorage(t)
			srv := NewServer(cfg, filepath.Join(t.TempDir(), "config.xml"), db, nil, nil)
			handler := srv.Handler()
			scheme := "http"
			if enableSSL {
				scheme = "https"
			}
			setup := httptest.NewRequest("POST", scheme+"://localhost/api/v1/auth/setup", bytes.NewBufferString(`{"username":"admin","password":"Settings-Test-Only-2026"}`))
			setupResult := httptest.NewRecorder()
			handler.ServeHTTP(setupResult, setup)
			if setupResult.Code != http.StatusCreated {
				t.Fatalf("setup: %d", setupResult.Code)
			}
			var session *http.Cookie
			var csrf string
			for _, cookie := range setupResult.Result().Cookies() {
				switch cookie.Name {
				case sessionCookieName:
					session = cookie
				case csrfCookieName:
					csrf = cookie.Value
				}
			}
			if session == nil || csrf == "" {
				t.Fatal("setup did not establish session and CSRF cookies")
			}
			request := func(method, path string, body []byte) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequest(method, scheme+"://localhost"+path, bytes.NewReader(body))
				req.AddCookie(session)
				req.Header.Set("X-CSRF-Token", csrf)
				result := httptest.NewRecorder()
				handler.ServeHTTP(result, req)
				return result
			}
			loaded := request("GET", "/api/v1/config", nil)
			if loaded.Code != http.StatusOK {
				t.Fatalf("load settings: %d", loaded.Code)
			}
			var settings map[string]any
			if err := json.Unmarshal(loaded.Body.Bytes(), &settings); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(map[string]any{"enableSsl": !enableSSL})
			if err != nil {
				t.Fatal(err)
			}
			withoutUsername := request("PUT", "/api/v1/config", body)
			if withoutUsername.Code != http.StatusOK || request("GET", "/api/v1/auth/user", nil).Code != http.StatusOK {
				t.Fatal("SSL-only update invalidated session")
			}
			settings["enableSsl"] = !enableSSL
			body, err = json.Marshal(settings)
			if err != nil {
				t.Fatal(err)
			}
			saved := request("PUT", "/api/v1/config", body)
			if saved.Code != http.StatusOK {
				t.Fatalf("save settings: %d %s", saved.Code, saved.Body.String())
			}
			current := request("GET", "/api/v1/auth/user", nil)
			if current.Code != http.StatusOK {
				t.Fatalf("saving SSL settings logged out the session: status %d", current.Code)
			}
			restart := request("POST", "/api/v1/system/restart", nil)
			if restart.Code != http.StatusOK {
				t.Fatalf("existing session cannot request restart: %d", restart.Code)
			}
			credentialChange := []byte(`{"username":"renamed-admin"}`)
			if enableSSL {
				credentialChange = []byte(`{"password":"Changed-Test-Only-2026"}`)
			}
			changed := request("PUT", "/api/v1/config", credentialChange)
			if changed.Code != http.StatusOK {
				t.Fatalf("change credentials: %d", changed.Code)
			}
			if current := request("GET", "/api/v1/auth/user", nil); current.Code != http.StatusUnauthorized {
				t.Fatalf("actual credential change did not revoke session: %d", current.Code)
			}
		})
	}
}

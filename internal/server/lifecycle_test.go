package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestLifecyclePermissionsAndPropagation(t *testing.T) {
	for _, endpoint := range []string{"restart", "shutdown"} {
		for _, auth := range []string{"none", "wrong key", "key", "session", "session csrf"} {
			t.Run(endpoint+"/"+auth, func(t *testing.T) {
				cfg := config.NewDefault()
				cfg.APIKey = "test-key"
				cfg.AuthenticationRequired = "Enabled"
				srv := NewServer(cfg, "", nil, nil, nil)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/system/"+endpoint, nil)
				switch auth {
				case "key":
					req.Header.Set("X-Api-Key", "test-key")
				case "wrong key":
					req.Header.Set("X-Api-Key", "wrong")
				case "session", "session csrf":
					token := srv.sessions.Create("admin")
					csrf := srv.sessions.CSRFToken(token)
					req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
					if auth == "session csrf" {
						req.Header.Set("X-CSRF-Token", csrf)
					}
				}
				rec := httptest.NewRecorder()
				srv.Handler().ServeHTTP(rec, req)
				expected := http.StatusUnauthorized
				if auth == "key" || auth == "session csrf" {
					expected = http.StatusOK
					if endpoint == "shutdown" && IsContainerized(cfg.UpdateMechanism) {
						expected = http.StatusBadRequest
					}
				}
				if rec.Code != expected {
					t.Fatalf("status %d, want %d: %s", rec.Code, expected, rec.Body.String())
				}
				if expected == http.StatusOK {
					done := make(chan LifecycleAction, 1)
					go func() { done <- srv.WaitLifecycle() }()
					want := LifecycleRestart
					if endpoint == "shutdown" {
						want = LifecycleShutdown
					}
					select {
					case got := <-done:
						if got != want {
							t.Fatalf("action %v, want %v", got, want)
						}
					case <-time.After(time.Second):
						t.Fatal("lifecycle event missing")
					}
				}
			})
		}
	}
}

func TestContainerStatusAndShutdownGuard(t *testing.T) {
	for _, indicator := range []string{"IN_DOCKER", "CONTAINER", "UpdateMechanism"} {
		t.Run(indicator, func(t *testing.T) {
			cfg := config.NewDefault()
			cfg.APIKey = "key"
			if indicator == "UpdateMechanism" {
				cfg.UpdateMechanism = "Docker"
			} else {
				t.Setenv(indicator, "1")
			}
			srv := NewServer(cfg, "", nil, nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
			req.Header.Set("X-Api-Key", "key")
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			var status struct {
				IsContainerized bool `json:"isContainerized"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil || !status.IsContainerized || rec.Code != 200 {
				t.Fatalf("container status: %s", rec.Body.String())
			}
			req = httptest.NewRequest(http.MethodPost, "/api/v1/system/shutdown", nil)
			req.Header.Set("X-Api-Key", "key")
			rec = httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != 400 || !bytes.Contains(rec.Body.Bytes(), []byte("shutdown is disabled in containerized environments")) {
				t.Fatalf("shutdown: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRestartReturnsBootIdentity(t *testing.T) {
	cfg := config.NewDefault()
	cfg.APIKey = "key"
	srv := NewServer(cfg, "", nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/restart", nil)
	req.Header.Set("X-Api-Key", "key")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var result struct {
		InstanceID string `json:"instanceId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.InstanceID == "" {
		t.Fatalf("restart must identify the old boot: %s", rec.Body.String())
	}
}

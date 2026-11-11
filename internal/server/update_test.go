package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/updater"
)

func TestUpdateAPIRestrictions(t *testing.T) {
	for _, owner := range []string{"scoop", "aur", "winget", "chocolatey", "Docker", "missing verification"} {
		t.Run(owner, func(t *testing.T) {
			cfg := config.NewDefault()
			cfg.APIKey = "fixture-api-key"
			s := NewServer(cfg, "", nil, nil, nil)
			exe := filepath.Join(t.TempDir(), "app")
			if owner != "Docker" && owner != "missing verification" {
				if err := os.WriteFile(filepath.Join(filepath.Dir(exe), "update-owner"), []byte(owner), 0600); err != nil {
					t.Fatal(err)
				}
			}
			e := &updater.Engine{Executable: exe, Current: updater.Identity{Version: "v1.0.0"},
				Config: s.Config, Container: func(string) bool { return owner == "Docker" }}
			s.SetUpdater(e)
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				path := "/api/v1/update/status"
				if method == http.MethodPost {
					path = "/api/v1/update/apply"
				}
				req := httptest.NewRequest(method, path, nil)
				req.Header.Set("X-Api-Key", cfg.APIKey)
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, req)
				if method == http.MethodGet {
					var status updater.Status
					if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &status) != nil || status.CanUpdate || status.Reason == "" {
						t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
					}
					if owner != "missing verification" && status.Owner != owner {
						t.Fatalf("update owner=%s", status.Owner)
					}
				} else if rec.Code != http.StatusConflict {
					t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
				}
				if owner == "missing verification" && !strings.Contains(rec.Body.String(), "trusted Minisign public key") {
					t.Fatal("missing trust configuration was not explained")
				}
				select {
				case <-s.lifecycle:
					t.Fatal("rejected update stopped the application")
				default:
				}
			}
		})
	}
}

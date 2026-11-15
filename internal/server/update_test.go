package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/updater"
)

func TestSettingsCannotRaceCommittedUpdate(t *testing.T) {
	public, private, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("fixture key generation failed")
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	f, err := zw.Create("anilist-arr-sync.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("fixture executable")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"version":"v2.0.0","commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","channel":"main"}`)
	name := "anilist-arr-sync_v2.0.0_windows_amd64.zip"
	sums := fmt.Appendf(nil, "%x  release.json\n%x  %s\n", sha256.Sum256(manifest), sha256.Sum256(archive.Bytes()), name)
	assets := map[string][]byte{"release.json": manifest, name: archive.Bytes(), "checksums.txt": sums, "checksums.txt.minisig": minisign.Sign(private, sums)}
	var release *httptest.Server
	release = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/latest" {
			var files []map[string]string
			for name := range assets {
				files = append(files, map[string]string{"name": name, "browser_download_url": release.URL + "/" + name})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v2.0.0", "assets": files})
			return
		}
		_, _ = w.Write(assets[strings.TrimPrefix(r.URL.Path, "/")])
	}))
	defer release.Close()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	cfg := config.NewDefault()
	cfg.APIKey = "fixture-api-key"
	s := NewServer(cfg, "", db, nil, nil)
	entered, relinquish := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-relinquish:
		default:
			close(relinquish)
		}
	}()
	e := &updater.Engine{DB: db, Client: release.Client(), APIBase: release.URL, PublicKey: public.String(), Current: updater.Identity{Version: "v1.0.0"},
		Platform: "windows", Arch: "amd64", Executable: filepath.Join(dir, "app.exe"), Staging: filepath.Join(dir, "staging"), Config: s.Config, Container: func(string) bool { return false },
		Launch: func(context.Context, string) error { _ = s.Config(); close(entered); <-relinquish; return nil }}
	s.SetUpdater(e)
	settingsEntered := make(chan struct{})
	handler := s.Handler()
	observedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/api/v1/config" {
			close(settingsEntered)
		}
		handler.ServeHTTP(w, r)
	})
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-Api-Key", cfg.APIKey)
		rec := httptest.NewRecorder()
		observedHandler.ServeHTTP(rec, req)
		return rec
	}
	applied := make(chan *httptest.ResponseRecorder, 1)
	go func() { applied <- request("POST", "/api/v1/update/apply", "") }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("update did not reach handoff")
	}
	saved := make(chan *httptest.ResponseRecorder, 1)
	go func() { saved <- request("PUT", "/api/v1/config", `{"port":8181}`) }()
	select {
	case <-settingsEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("configuration request did not enter the handler")
	}
	select {
	case rec := <-saved:
		t.Fatalf("settings changed during handoff: %d", rec.Code)
	case <-time.After(50 * time.Millisecond):
	}
	close(relinquish)
	if rec := <-applied; rec.Code != 202 {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	if rec := <-saved; rec.Code != 409 {
		t.Fatalf("settings accepted after handoff: %d", rec.Code)
	}
}

func TestUpdatesRequireRestart(t *testing.T) {
	cfg := config.NewDefault()
	cfg.APIKey = "fixture-api-key"
	cfg.UpdateAutomatically = true
	s := NewServer(cfg, "", nil, nil, nil)
	s.SetUpdater(&updater.Engine{Executable: filepath.Join(t.TempDir(), "app"), Config: s.Config, Container: func(string) bool { return false }})
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-Api-Key", cfg.APIKey)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := request("PUT", "/api/v1/config", `{"port":8181}`); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if rec := request("POST", "/api/v1/update/apply", ""); rec.Code != 409 || !strings.Contains(rec.Body.String(), "restart") {
		t.Fatalf("manual update: %d %s", rec.Code, rec.Body.String())
	}
	if err := s.AutomaticUpdate(context.Background()); err == nil || !strings.Contains(err.Error(), "restart") {
		t.Fatalf("automatic update: %v", err)
	}
	var status updater.Status
	rec := request("GET", "/api/v1/update/status", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.CanUpdate || !strings.Contains(status.Reason, "restart") {
		t.Fatal("update status omitted pending restart")
	}
}

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

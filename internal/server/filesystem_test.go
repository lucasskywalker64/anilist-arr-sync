package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestFilesystemHandler(t *testing.T) {
	tmpDir := t.TempDir()
	subA := filepath.Join(tmpDir, "folderA")
	subB := filepath.Join(tmpDir, "folderB")
	if err := os.Mkdir(subA, 0o755); err != nil {
		t.Fatalf("failed to create subA: %v", err)
	}
	if err := os.Mkdir(subB, 0o755); err != nil {
		t.Fatalf("failed to create subB: %v", err)
	}
	// Add a regular file; it should not appear in directory listings
	fileC := filepath.Join(tmpDir, "fileC.txt")
	if err := os.WriteFile(fileC, []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to create fileC: %v", err)
	}
	// Hidden and system-style directories must be omitted from listings
	for _, name := range []string{".hidden", "$RECYCLE.BIN"} {
		if err := os.Mkdir(filepath.Join(tmpDir, name), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", name, err)
		}
	}

	cfg := &config.Config{
		AuthenticationMethod:   "Disabled",
		AuthenticationRequired: "Disabled",
		BackupFolder:           tmpDir,
	}
	srv := NewServer(cfg, "", nil, nil, nil)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	t.Run("DefaultPath", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/filesystem", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp FilesystemResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if len(resp.Roots) == 0 {
			t.Errorf("expected at least 1 root drive/directory")
		}
		if len(resp.Directories) != 2 {
			t.Errorf("expected 2 subdirectories, got %d", len(resp.Directories))
		}
		if resp.Directories[0].Name != "folderA" || resp.Directories[1].Name != "folderB" {
			t.Errorf("unexpected directory names: %+v", resp.Directories)
		}
	})

	t.Run("ExplicitPath", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/filesystem?path="+url.QueryEscape(subA), nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp FilesystemResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp.Current != subA {
			t.Errorf("expected current %s, got %s", subA, resp.Current)
		}
		if resp.Parent != tmpDir {
			t.Errorf("expected parent %s, got %s", tmpDir, resp.Parent)
		}
		if len(resp.Directories) != 0 {
			t.Errorf("expected 0 subdirectories in folderA, got %d", len(resp.Directories))
		}
	})

	t.Run("NonExistentPathFallback", func(t *testing.T) {
		nonExistent := filepath.Join(tmpDir, "missing", "path")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/filesystem?path="+url.QueryEscape(nonExistent), nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK after fallback, got %d: %s", w.Code, w.Body.String())
		}

		var resp FilesystemResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if len(resp.Roots) == 0 {
			t.Fatalf("expected at least 1 root")
		}
		if resp.Current != resp.Roots[0] {
			t.Errorf("expected fallback to %s, got %s", resp.Roots[0], resp.Current)
		}
	})
}

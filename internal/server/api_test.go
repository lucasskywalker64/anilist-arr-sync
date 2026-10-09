package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func TestOverridesEndpoints(t *testing.T) {
	db := newTestStorage(t)
	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "test-key"

	srv := NewServer(cfg, "", db, nil, nil)
	handler := srv.Handler()

	// 1. Create mapping override via POST /api/v1/overrides
	createPayload, _ := json.Marshal(map[string]any{
		"anilistId":     500,
		"mediaType":     "SERIES",
		"tvdbId":        123456,
		"tmdbId":        0,
		"seasons":       "1-2",
		"titleOverride": "Custom Title",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/overrides", bytes.NewReader(createPayload))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "test-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("expected create override to succeed, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. GET /api/v1/overrides
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/overrides", nil)
	getReq.RemoteAddr = "127.0.0.1:1234"
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected GET /api/v1/overrides to return 200, got %d", getRec.Code)
	}
	var overrides []map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &overrides); err != nil {
		t.Fatalf("failed to parse overrides JSON: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(overrides))
	}
	if int(overrides[0]["anilistId"].(float64)) != 500 {
		t.Errorf("expected anilistId 500, got %v", overrides[0]["anilistId"])
	}

	// 3. DELETE /api/v1/overrides/500
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/overrides/500", nil)
	delReq.RemoteAddr = "127.0.0.1:1234"
	delReq.Header.Set("X-Api-Key", "test-key")
	delRec := httptest.NewRecorder()
	handler.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("expected DELETE /api/v1/overrides/500 to return 200, got %d: %s", delRec.Code, delRec.Body.String())
	}

	// 4. GET /api/v1/overrides is now empty
	getReq2 := httptest.NewRequest(http.MethodGet, "/api/v1/overrides", nil)
	getReq2.RemoteAddr = "127.0.0.1:1234"
	getRec2 := httptest.NewRecorder()
	handler.ServeHTTP(getRec2, getReq2)

	var overrides2 []map[string]any
	_ = json.Unmarshal(getRec2.Body.Bytes(), &overrides2)
	if len(overrides2) != 0 {
		t.Errorf("expected 0 overrides after delete, got %d", len(overrides2))
	}
}

func TestConfigEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.xml")

	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "secret-key-to-mask"
	cfg.RadarrAPIKey = "radarr-key-to-mask"
	cfg.SonarrAPIKey = "sonarr-key-to-mask"
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	db := newTestStorage(t)
	srv := NewServer(cfg, configPath, db, nil, nil)
	handler := srv.Handler()

	// 1. GET /api/v1/config returns sanitized config (API keys masked)
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	getReq.RemoteAddr = "127.0.0.1:1234"
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected GET /api/v1/config to return 200, got %d", getRec.Code)
	}
	var returnedCfg map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &returnedCfg); err != nil {
		t.Fatalf("failed to parse config JSON: %v", err)
	}
	if returnedCfg["apiKey"] == "secret-key-to-mask" {
		t.Errorf("expected apiKey to be sanitized or masked, got raw key")
	}

	// 2. PUT /api/v1/config updates settings
	updatePayload, _ := json.Marshal(map[string]any{
		"instanceName":      "My Anime Sync",
		"syncIntervalHours": 6,
	})
	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/config", bytes.NewReader(updatePayload))
	putReq.RemoteAddr = "127.0.0.1:1234"
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("X-Api-Key", "secret-key-to-mask")
	putRec := httptest.NewRecorder()
	handler.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("expected PUT /api/v1/config to return 200, got %d: %s", putRec.Code, putRec.Body.String())
	}
	if srv.cfg.InstanceName != "My Anime Sync" {
		t.Errorf("expected instanceName to be updated to 'My Anime Sync', got %s", srv.cfg.InstanceName)
	}
	if srv.cfg.SyncIntervalHours != 6 {
		t.Errorf("expected syncIntervalHours to be 6, got %d", srv.cfg.SyncIntervalHours)
	}
}

func TestSyncAndStagedEndpoints(t *testing.T) {
	db := newTestStorage(t)
	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "sync-key"

	// Seed staged_sync_actions and sync_history
	var stagedID int64
	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO staged_sync_actions (action_type, media_type, title, target_service, payload_json, status)
			VALUES ('ADD_SERIES', 'SERIES', 'Sousou no Frieren', 'Sonarr', '{"tvdbId": 414833}', 'PENDING');
		`)
		if err != nil {
			return err
		}
		stagedID, _ = res.LastInsertId()

		_, err = tx.ExecContext(ctx, `
			INSERT INTO sync_history (duration_ms, status, items_scanned, trigger_type)
			VALUES (1250, 'SUCCESS', 42, 'MANUAL_WEB');
		`)
		return err
	})
	if err != nil {
		t.Fatalf("failed to seed tables: %v", err)
	}

	srv := NewServer(cfg, "", db, nil, nil)
	handler := srv.Handler()

	// 1. GET /api/v1/sync/status
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/status", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected GET /api/v1/sync/status to return 200, got %d", rec.Code)
		}
	}

	// 2. GET /api/v1/sync/history
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/history", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected GET /api/v1/sync/history to return 200, got %d", rec.Code)
		}
		var history []map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &history)
		if len(history) != 1 {
			t.Fatalf("expected 1 history entry, got %d", len(history))
		}
	}

	// 3. GET /api/v1/staged returns pending actions
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/staged", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected GET /api/v1/staged to return 200, got %d", rec.Code)
		}
		var staged []map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &staged)
		if len(staged) != 1 {
			t.Fatalf("expected 1 staged action, got %d", len(staged))
		}
	}

	// 4. POST /api/v1/staged/reject marks action as REJECTED
	{
		rejectPayload, _ := json.Marshal(map[string]any{
			"ids": []int64{stagedID},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/staged/reject", bytes.NewReader(rejectPayload))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", "sync-key")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected POST /api/v1/staged/reject to return 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify status changed to REJECTED in database
		err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
			var status string
			err := q.QueryRowContext(ctx, "SELECT status FROM staged_sync_actions WHERE id = ?;", stagedID).Scan(&status)
			if err != nil {
				return err
			}
			if status != "REJECTED" {
				t.Errorf("expected staged action status REJECTED, got %s", status)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("failed to query staged_sync_actions: %v", err)
		}
	}
}

func TestBackupAndRestoreEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.xml")
	backupDir := filepath.Join(tempDir, "backups")

	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "backup-key"
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	db := newTestStorage(t)
	backupEngine := backup.NewEngine(db, configPath, backupDir)
	srv := NewServer(cfg, configPath, db, nil, backupEngine)
	handler := srv.Handler()

	// 1. GET /api/v1/backups returns empty initially
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected GET /api/v1/backups to return 200, got %d", rec.Code)
		}
		var backups []map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &backups)
		if len(backups) != 0 {
			t.Errorf("expected 0 backups initially, got %d", len(backups))
		}
	}

	// 2. POST /api/v1/backups creates a new backup
	var createdFilename string
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Api-Key", "backup-key")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
			t.Fatalf("expected POST /api/v1/backups to return 200/201, got %d: %s", rec.Code, rec.Body.String())
		}
		var backupInfo map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &backupInfo)
		createdFilename = backupInfo["filename"].(string)
		if createdFilename == "" {
			t.Fatalf("expected non-empty backup filename")
		}
	}

	// 3. GET /api/v1/backups now lists 1 backup
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var backups []map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &backups)
		if len(backups) != 1 {
			t.Fatalf("expected 1 backup, got %d", len(backups))
		}
	}

	// 4. POST /api/v1/backups/restore restores backup
	{
		restorePayload, _ := json.Marshal(map[string]string{
			"filename": createdFilename,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/restore", bytes.NewReader(restorePayload))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", "backup-key")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected restore to return 200, got %d: %s", rec.Code, rec.Body.String())
		}
	}
}

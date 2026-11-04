package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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

	// Seed synced_media row to verify arr_title synchronization
	_ = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO synced_media (
				anilist_id, media_type, format, title_romaji, title_english,
				release_year, poster_url, anilist_status, target_service,
				tvdb_id, tmdb_id, arr_title, monitored_status, seasons
			) VALUES (
				500, 'SERIES', 'TV', 'Original Romaji', 'Original English',
				2020, '', 'CURRENT', 'SONARR',
				NULL, NULL, NULL, 'MONITORED', '1'
			);
		`)
		return err
	})

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

	var seededArrTitle sql.NullString
	if readErr := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT arr_title FROM synced_media WHERE anilist_id = 500;").Scan(&seededArrTitle)
	}); readErr != nil {
		t.Fatalf("failed to query synced_media: %v", readErr)
	}
	if !seededArrTitle.Valid || seededArrTitle.String != "Custom Title" {
		t.Errorf("expected synced_media arr_title to be updated to 'Custom Title', got %v", seededArrTitle)
	}

	// 1b. Update mapping override without titleOverride clears arr_title to NULL
	noTitlePayload, _ := json.Marshal(map[string]any{
		"anilistId":     500,
		"mediaType":     "SERIES",
		"tvdbId":        123456,
		"tmdbId":        0,
		"seasons":       "1-2",
		"titleOverride": "",
	})
	noTitleReq := httptest.NewRequest(http.MethodPost, "/api/v1/overrides", bytes.NewReader(noTitlePayload))
	noTitleReq.RemoteAddr = "127.0.0.1:1234"
	noTitleReq.Header.Set("Content-Type", "application/json")
	noTitleReq.Header.Set("X-Api-Key", "test-key")
	noTitleRec := httptest.NewRecorder()
	handler.ServeHTTP(noTitleRec, noTitleReq)
	if noTitleRec.Code != http.StatusCreated && noTitleRec.Code != http.StatusOK {
		t.Fatalf("expected override update without titleOverride to succeed, got %d: %s", noTitleRec.Code, noTitleRec.Body.String())
	}

	var blankArrTitle sql.NullString
	if readErr := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT arr_title FROM synced_media WHERE anilist_id = 500;").Scan(&blankArrTitle)
	}); readErr != nil {
		t.Fatalf("failed to query synced_media after blank titleOverride: %v", readErr)
	}
	if blankArrTitle.Valid {
		t.Errorf("expected synced_media arr_title to be NULL when titleOverride is omitted/empty, got %v", blankArrTitle.String)
	}

	// Re-apply Custom Title before testing DELETE
	reqReapply := httptest.NewRequest(http.MethodPost, "/api/v1/overrides", bytes.NewReader(createPayload))
	reqReapply.RemoteAddr = "127.0.0.1:1234"
	reqReapply.Header.Set("Content-Type", "application/json")
	reqReapply.Header.Set("X-Api-Key", "test-key")
	reapplyRec := httptest.NewRecorder()
	handler.ServeHTTP(reapplyRec, reqReapply)
	if reapplyRec.Code != http.StatusCreated && reapplyRec.Code != http.StatusOK {
		t.Fatalf("expected override reapply to succeed, got %d: %s", reapplyRec.Code, reapplyRec.Body.String())
	}

	var reappliedArrTitle sql.NullString
	if readErr := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT arr_title FROM synced_media WHERE anilist_id = 500;").Scan(&reappliedArrTitle)
	}); readErr != nil {
		t.Fatalf("failed to query synced_media after reapply: %v", readErr)
	}
	if !reappliedArrTitle.Valid || reappliedArrTitle.String != "Custom Title" {
		t.Fatalf("expected synced_media arr_title to be 'Custom Title' before delete, got %v", reappliedArrTitle)
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

	var clearedArrTitle sql.NullString
	if readErr := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT arr_title FROM synced_media WHERE anilist_id = 500;").Scan(&clearedArrTitle)
	}); readErr != nil {
		t.Fatalf("failed to query synced_media after delete: %v", readErr)
	}
	if clearedArrTitle.Valid {
		t.Errorf("expected synced_media arr_title to be cleared (NULL) after delete, got %v", clearedArrTitle.String)
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
	if returnedCfg["apiKey"] != "secret-key-to-mask" {
		t.Errorf("expected apiKey to be returned for viewing and copying, got %v", returnedCfg["apiKey"])
	}
	for _, field := range []string{"unmonitorCompleted", "deleteDropped", "deleteCompleted", "unmonitorWatched", "deleteWatched"} {
		if _, ok := returnedCfg[field]; !ok {
			t.Errorf("expected GET /api/v1/config to include field %s", field)
		}
	}

	// 2. PUT /api/v1/config updates settings (non-network fields do not require restart)
	updatePayload, _ := json.Marshal(map[string]any{
		"instanceName":       "My Anime Sync",
		"syncIntervalHours":  6,
		"unmonitorCompleted": true,
		"deleteDropped":      true,
		"deleteCompleted":    true,
		"unmonitorWatched":   true,
		"deleteWatched":      true,
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
	var putResp map[string]any
	_ = json.Unmarshal(putRec.Body.Bytes(), &putResp)
	if restart, ok := putResp["restartRequired"].(bool); !ok || restart {
		t.Errorf("expected restartRequired to be false when updating non-network fields, got %v", putResp["restartRequired"])
	}
	if srv.cfg.InstanceName != "My Anime Sync" {
		t.Errorf("expected instanceName to be updated to 'My Anime Sync', got %s", srv.cfg.InstanceName)
	}
	if srv.cfg.SyncIntervalHours != 6 {
		t.Errorf("expected syncIntervalHours to be 6, got %d", srv.cfg.SyncIntervalHours)
	}
	if !srv.cfg.UnmonitorCompleted || !srv.cfg.DeleteDropped || !srv.cfg.DeleteCompleted || !srv.cfg.UnmonitorWatched || !srv.cfg.DeleteWatched {
		t.Errorf("expected cleanup and pruning settings to be true, got %+v", srv.cfg)
	}

	// 3. PUT /api/v1/config with Port change requires restart
	portPayload, _ := json.Marshal(map[string]any{
		"port": 7172,
	})
	putReq2 := httptest.NewRequest(http.MethodPut, "/api/v1/config", bytes.NewReader(portPayload))
	putReq2.RemoteAddr = "127.0.0.1:1234"
	putReq2.Header.Set("Content-Type", "application/json")
	putReq2.Header.Set("X-Api-Key", "secret-key-to-mask")
	putRec2 := httptest.NewRecorder()
	handler.ServeHTTP(putRec2, putReq2)
	if putRec2.Code != http.StatusOK {
		t.Fatalf("expected PUT /api/v1/config to return 200, got %d: %s", putRec2.Code, putRec2.Body.String())
	}
	var putResp2 map[string]any
	_ = json.Unmarshal(putRec2.Body.Bytes(), &putResp2)
	if restart, ok := putResp2["restartRequired"].(bool); !ok || !restart {
		t.Errorf("expected restartRequired to be true when updating port, got %v", putResp2["restartRequired"])
	}

	// 4. Subsequent PUT /api/v1/config keeping the modified port still reports restartRequired: true
	putReq3 := httptest.NewRequest(http.MethodPut, "/api/v1/config", bytes.NewReader(updatePayload))
	putReq3.RemoteAddr = "127.0.0.1:1234"
	putReq3.Header.Set("Content-Type", "application/json")
	putReq3.Header.Set("X-Api-Key", "secret-key-to-mask")
	putRec3 := httptest.NewRecorder()
	handler.ServeHTTP(putRec3, putReq3)
	if putRec3.Code != http.StatusOK {
		t.Fatalf("expected PUT /api/v1/config to return 200, got %d: %s", putRec3.Code, putRec3.Body.String())
	}
	var putResp3 map[string]any
	_ = json.Unmarshal(putRec3.Body.Bytes(), &putResp3)
	if restart, ok := putResp3["restartRequired"].(bool); !ok || !restart {
		t.Errorf("expected restartRequired to remain true while bound port differs from saved port, got %v", putResp3["restartRequired"])
	}
}

func TestConfigEndpoints_RestartBaselineRefreshedOnStart(t *testing.T) {
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on port: %v", err)
	}
	port1 := l1.Addr().(*net.TCPAddr).Port
	_ = l1.Close()

	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on second port: %v", err)
	}
	port2 := l2.Addr().(*net.TCPAddr).Port
	_ = l2.Close()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.xml")

	cfg := config.NewDefault()
	cfg.BindAddress = "127.0.0.1"
	cfg.Port = port1
	cfg.EnableSsl = false
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "secret-key"
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	db := newTestStorage(t)
	srv := NewServer(cfg, configPath, db, nil, nil)
	handler := srv.Handler()

	// 1. PUT port change to port2 before Start() returns restartRequired: true
	portPayload, _ := json.Marshal(map[string]any{
		"port": port2,
	})
	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/config", bytes.NewReader(portPayload))
	putReq.RemoteAddr = "127.0.0.1:1234"
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("X-Api-Key", "secret-key")
	putRec := httptest.NewRecorder()
	handler.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected PUT /api/v1/config to return 200, got %d: %s", putRec.Code, putRec.Body.String())
	}
	var putResp map[string]any
	_ = json.Unmarshal(putRec.Body.Bytes(), &putResp)
	if restart, ok := putResp["restartRequired"].(bool); !ok || !restart {
		t.Fatalf("expected restartRequired true before start, got %v", putResp["restartRequired"])
	}

	// 2. Start() binds port2 and refreshes the baseline
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// 3. Subsequent PUT keeping port2 now returns restartRequired: false
	updatePayload, _ := json.Marshal(map[string]any{
		"instanceName": "Updated Name",
		"port":         port2,
	})
	putReq2 := httptest.NewRequest(http.MethodPut, "/api/v1/config", bytes.NewReader(updatePayload))
	putReq2.RemoteAddr = "127.0.0.1:1234"
	putReq2.Header.Set("Content-Type", "application/json")
	putReq2.Header.Set("X-Api-Key", "secret-key")
	putRec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(putRec2, putReq2)
	if putRec2.Code != http.StatusOK {
		t.Fatalf("expected PUT /api/v1/config to return 200, got %d: %s", putRec2.Code, putRec2.Body.String())
	}
	var putResp2 map[string]any
	_ = json.Unmarshal(putRec2.Body.Bytes(), &putResp2)
	if restart, ok := putResp2["restartRequired"].(bool); !ok || restart {
		t.Errorf("expected restartRequired false after Start() refreshed baseline, got %v", putResp2["restartRequired"])
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

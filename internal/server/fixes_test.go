package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestConfigUpdates_PersistenceAndValidation(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.xml")

	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "test-api-key"
	cfg.InstanceName = "Initial Name"
	cfg.SyncIntervalHours = 4
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	db := newTestStorage(t)
	srv := NewServer(cfg, configPath, db, nil, nil)
	handler := srv.Handler()

	// 1. Invalid update is rejected and does not mutate in-memory state
	invalidBody, _ := json.Marshal(map[string]any{
		"syncIntervalHours": -10, // Invalid: must be > 0
		"instanceName":      "Corrupted Name",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", bytes.NewReader(invalidBody))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "test-api-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid config, got %d", rec.Code)
	}
	if srv.Config().InstanceName != "Initial Name" {
		t.Errorf("instance name was mutated despite validation failure: got %s", srv.Config().InstanceName)
	}

	// 2. Valid update persists to file on disk
	validBody, _ := json.Marshal(map[string]any{
		"instanceName":      "Updated Name",
		"syncIntervalHours": 8,
	})
	req2 := httptest.NewRequest(http.MethodPut, "/api/v1/config", bytes.NewReader(validBody))
	req2.RemoteAddr = "127.0.0.1:1234"
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Api-Key", "test-api-key")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 OK for valid config, got %d", rec2.Code)
	}
	if srv.Config().InstanceName != "Updated Name" {
		t.Errorf("expected in-memory instance name 'Updated Name', got %s", srv.Config().InstanceName)
	}

	// Verify disk file
	loaded, err := config.Load(configPath, nil)
	if err != nil {
		t.Fatalf("failed to reload config from disk: %v", err)
	}
	if loaded.InstanceName != "Updated Name" || loaded.SyncIntervalHours != 8 {
		t.Errorf("disk config was not persisted: name=%s, hours=%d", loaded.InstanceName, loaded.SyncIntervalHours)
	}
}

func TestRestoreBackup_ArbitraryPathBlocked(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.xml")
	backupDir := filepath.Join(tempDir, "backups")

	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "test-api-key"
	_ = config.Save(configPath, cfg)

	db := newTestStorage(t)
	backupEngine := backup.NewEngine(db, configPath, backupDir)
	srv := NewServer(cfg, configPath, db, nil, backupEngine)
	handler := srv.Handler()

	// Create arbitrary file outside backup directory
	evilPath := filepath.Join(tempDir, "evil.zip")
	_ = os.WriteFile(evilPath, []byte("fake-zip"), 0644)

	restoreBody, _ := json.Marshal(map[string]string{
		"archivePath": evilPath,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/restore", bytes.NewReader(restoreBody))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "test-api-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for arbitrary external path, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStagedActions_Validation(t *testing.T) {
	db := newTestStorage(t)
	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "test-api-key"

	srv := NewServer(cfg, "", db, nil, nil) // orch is nil
	handler := srv.Handler()

	// 1. Apply staged without orchestrator returns 400
	applyBody, _ := json.Marshal(map[string]any{"ids": []int{1}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/staged/apply", bytes.NewReader(applyBody))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "test-api-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request when orchestrator is nil on apply, got %d", rec.Code)
	}

	// 2. Rejecting nonexistent ID reports empty rejected list
	rejectBody, _ := json.Marshal(map[string]any{"ids": []int{9999}})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/staged/reject", bytes.NewReader(rejectBody))
	req2.RemoteAddr = "127.0.0.1:1234"
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Api-Key", "test-api-key")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 OK for reject request, got %d", rec2.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp)
	rejected := resp["rejected"].([]any)
	if len(rejected) != 0 {
		t.Errorf("expected 0 rejected for nonexistent ID, got %d", len(rejected))
	}
}

func TestConcurrentSetup_SingleAdmin(t *testing.T) {
	db := newTestStorage(t)
	cfg := config.NewDefault()

	srv := NewServer(cfg, "", db, nil, nil)
	handler := srv.Handler()

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]string{
				"username": "admin",
				"password": "Password123!",
			})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code == http.StatusCreated {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 setup request to succeed, got %d", successCount)
	}
}

func TestFormsSession_CSRFTokenHeader(t *testing.T) {
	db := newTestStorage(t)
	cfg := config.NewDefault()
	cfg.AuthenticationMethod = "Forms"
	cfg.AuthenticationRequired = "Enabled"
	cfg.APIKey = "server-master-key"

	srv := NewServer(cfg, "", db, nil, nil)
	handler := srv.Handler()

	// 1. Initial setup
	setupBody, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "Password123!",
	})
	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(setupBody))
	setupReq.Header.Set("Content-Type", "application/json")
	setupRec := httptest.NewRecorder()
	handler.ServeHTTP(setupRec, setupReq)

	var setupResp map[string]any
	_ = json.Unmarshal(setupRec.Body.Bytes(), &setupResp)
	csrfToken := setupResp["csrfToken"].(string)

	cookies := setupRec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			sessionCookie = c
			break
		}
	}

	// 2. Mutating request with valid session and X-CSRF-Token header succeeds without master API key
	overrideBody, _ := json.Marshal(map[string]any{
		"anilistId": 1001,
		"mediaType": "SERIES",
		"tvdbId":    5555,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/overrides", bytes.NewReader(overrideBody))
	req.AddCookie(sessionCookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected session with X-CSRF-Token to succeed, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. Mutating request with invalid CSRF token fails
	reqBad := httptest.NewRequest(http.MethodPost, "/api/v1/overrides", bytes.NewReader(overrideBody))
	reqBad.AddCookie(sessionCookie)
	reqBad.Header.Set("Content-Type", "application/json")
	reqBad.Header.Set("X-CSRF-Token", "invalid-csrf-token")
	recBad := httptest.NewRecorder()
	handler.ServeHTTP(recBad, reqBad)

	if recBad.Code != http.StatusUnauthorized {
		t.Errorf("expected invalid CSRF token to return 401, got %d", recBad.Code)
	}
}

func TestReviewResolve_Validation(t *testing.T) {
	db := newTestStorage(t)
	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "key"

	srv := NewServer(cfg, "", db, nil, nil)
	handler := srv.Handler()

	// Seed queue item
	_ = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO review_queue (id, anilist_id, media_type, title_romaji, candidates_json, reason, status)
			VALUES (1, 99, 'MOVIE', 'Test Movie', '[]', 'UNMAPPED', 'PENDING');
		`)
		return err
	})

	// 1. Missing both tvdbId and tmdbId returns 400
	badBody1, _ := json.Marshal(map[string]any{
		"mediaType": "MOVIE",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/review/1/resolve", bytes.NewReader(badBody1))
	req1.RemoteAddr = "127.0.0.1:1234"
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("X-Api-Key", "key")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when IDs are missing, got %d", rec1.Code)
	}

	// 2. Invalid mediaType returns 400
	badBody2, _ := json.Marshal(map[string]any{
		"mediaType": "INVALID_TYPE",
		"tvdbId":    123,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/review/1/resolve", bytes.NewReader(badBody2))
	req2.RemoteAddr = "127.0.0.1:1234"
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Api-Key", "key")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when mediaType is invalid, got %d", rec2.Code)
	}

	// 3. Valid resolve succeeds
	goodBody, _ := json.Marshal(map[string]any{
		"tvdbId": 12345,
	})
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/review/1/resolve", bytes.NewReader(goodBody))
	req3.RemoteAddr = "127.0.0.1:1234"
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("X-Api-Key", "key")
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid resolve, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// 4. Resolving again returns 404 (already resolved)
	rec4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodPost, "/api/v1/review/1/resolve", bytes.NewReader(goodBody))
	req4.RemoteAddr = "127.0.0.1:1234"
	req4.Header.Set("Content-Type", "application/json")
	req4.Header.Set("X-Api-Key", "key")
	handler.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusNotFound {
		t.Errorf("expected 404 for already resolved item, got %d", rec4.Code)
	}
}

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
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
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

func TestExternalAuth_NoBypassWhenDisabledForLocalAddresses(t *testing.T) {
	cfg := config.NewDefault()
	cfg.AuthenticationMethod = "External"
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "secret-key"

	srv := &Server{
		cfg:      cfg,
		sessions: NewSessionStore(1 * time.Hour),
	}

	var capturedUser string
	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := r.Context().Value(userContextKey).(string); ok {
			capturedUser = u
		}
		w.WriteHeader(http.StatusOK)
	}))

	// 1. Proxied request from local IP without external user header receives 401 (not bypassed as local_user)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 when external header is missing despite local IP, got %d", rec.Code)
		}
	}

	// 2. Proxied request from local IP with Remote-User receives 200 and captures username
	{
		capturedUser = ""
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Header.Set("Remote-User", "authelia_admin")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 with valid Remote-User header, got %d", rec.Code)
		}
		if capturedUser != "authelia_admin" {
			t.Errorf("expected user 'authelia_admin', got %q", capturedUser)
		}
	}
}

func TestReviewResolve_NullableZeroIDs(t *testing.T) {
	db := newTestStorage(t)
	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "key"

	srv := NewServer(cfg, "", db, nil, nil)
	handler := srv.Handler()

	// Seed two pending review queue items
	_ = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, `
			INSERT INTO review_queue (id, anilist_id, media_type, title_romaji, candidates_json, reason, status)
			VALUES (10, 1010, 'SERIES', 'Series Title', '[]', 'UNMAPPED', 'PENDING'),
			       (20, 2020, 'MOVIE', 'Movie Title', '[]', 'UNMAPPED', 'PENDING');
		`)
		return nil
	})

	// 1. Resolve series with TVDB ID only: TMDB ID in mapping_overrides must be SQL NULL
	seriesPayload, _ := json.Marshal(map[string]any{
		"mediaType": "SERIES",
		"tvdbId":    55555,
		"tmdbId":    0,
	})
	reqSeries := httptest.NewRequest(http.MethodPost, "/api/v1/review/10/resolve", bytes.NewReader(seriesPayload))
	reqSeries.RemoteAddr = "127.0.0.1:1234"
	reqSeries.Header.Set("Content-Type", "application/json")
	reqSeries.Header.Set("X-Api-Key", "key")
	recSeries := httptest.NewRecorder()
	handler.ServeHTTP(recSeries, reqSeries)
	if recSeries.Code != http.StatusOK {
		t.Fatalf("expected 200 OK resolving series, got %d: %s", recSeries.Code, recSeries.Body.String())
	}

	_ = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		var tvdb sql.NullInt64
		var tmdb sql.NullInt64
		err := q.QueryRowContext(ctx, "SELECT tvdb_id, tmdb_id FROM mapping_overrides WHERE anilist_id = 1010;").
			Scan(&tvdb, &tmdb)
		if err != nil {
			t.Fatalf("query mapping_overrides series: %v", err)
		}
		if !tvdb.Valid || tvdb.Int64 != 55555 {
			t.Errorf("expected tvdb_id=55555, got %+v", tvdb)
		}
		if tmdb.Valid {
			t.Errorf("expected tmdb_id to be NULL, but got value %d", tmdb.Int64)
		}
		return nil
	})

	// 2. Resolve movie with TMDB ID only: TVDB ID in mapping_overrides must be SQL NULL
	moviePayload, _ := json.Marshal(map[string]any{
		"mediaType": "MOVIE",
		"tmdbId":    77777,
	})
	reqMovie := httptest.NewRequest(http.MethodPost, "/api/v1/review/20/resolve", bytes.NewReader(moviePayload))
	reqMovie.RemoteAddr = "127.0.0.1:1234"
	reqMovie.Header.Set("Content-Type", "application/json")
	reqMovie.Header.Set("X-Api-Key", "key")
	recMovie := httptest.NewRecorder()
	handler.ServeHTTP(recMovie, reqMovie)
	if recMovie.Code != http.StatusOK {
		t.Fatalf("expected 200 OK resolving movie, got %d: %s", recMovie.Code, recMovie.Body.String())
	}

	_ = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		var tvdb sql.NullInt64
		var tmdb sql.NullInt64
		err := q.QueryRowContext(ctx, "SELECT tvdb_id, tmdb_id FROM mapping_overrides WHERE anilist_id = 2020;").
			Scan(&tvdb, &tmdb)
		if err != nil {
			t.Fatalf("query mapping_overrides movie: %v", err)
		}
		if !tmdb.Valid || tmdb.Int64 != 77777 {
			t.Errorf("expected tmdb_id=77777, got %+v", tmdb)
		}
		if tvdb.Valid {
			t.Errorf("expected tvdb_id to be NULL, but got value %d", tvdb.Int64)
		}
		return nil
	})
}

func TestSessionStore_EvictsExpired(t *testing.T) {
	ttl := 20 * time.Millisecond
	store := NewSessionStore(ttl)

	tok1 := store.Create("user1")
	if store.Len() != 1 {
		t.Fatalf("expected 1 session, got %d", store.Len())
	}

	time.Sleep(30 * time.Millisecond)

	// Validate lazily purges expired session
	u, ok := store.Validate(tok1)
	if ok || u != "" {
		t.Errorf("expected expired session validation to fail")
	}
	if store.Len() != 0 {
		t.Errorf("expected session to be evicted upon expired Validate, got len=%d", store.Len())
	}

	// Create eagerly purges expired sessions
	tok2 := store.Create("user2")
	time.Sleep(30 * time.Millisecond)
	tok3 := store.Create("user3")

	if store.Len() != 1 {
		t.Errorf("expected tok2 to be evicted on Create(user3), got len=%d", store.Len())
	}
	if _, ok2 := store.Validate(tok2); ok2 {
		t.Errorf("expected tok2 to be expired")
	}
	if u3, ok3 := store.Validate(tok3); !ok3 || u3 != "user3" {
		t.Errorf("expected user3 session to be valid, got %q, %v", u3, ok3)
	}

	// CleanExpired explicitly purges
	time.Sleep(30 * time.Millisecond)
	store.CleanExpired()
	if store.Len() != 0 {
		t.Errorf("expected 0 sessions after CleanExpired, got %d", store.Len())
	}
}

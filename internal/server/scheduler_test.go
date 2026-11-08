package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/scheduler"
)

type syncTransportFunc func(*http.Request) (*http.Response, error)

func (f syncTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSyncEndpointsRecognizeOtherTriggers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.NewDefault()
		cfg.APIKey = "test-key"
		cfg.AniListUsername = "testuser"
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		client := anilist.NewClient("", anilist.WithHTTPClient(&http.Client{Transport: syncTransportFunc(
			func(r *http.Request) (*http.Response, error) {
				select {
				case entered <- struct{}{}:
				default:
				}
				select {
				case <-r.Context().Done():
				case <-release:
				}
				return nil, context.Canceled
			})}))
		db := newTestStorage(t)
		o := orchestrator.New(db, cfg, client, nil, nil)
		srv := NewServer(cfg, "", db, o, nil)
		handler := srv.Handler()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _, _ = o.Sync(ctx, orchestrator.TriggerCLI) }()
		<-entered
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/start", nil)
		req.Header.Set("X-Api-Key", cfg.APIKey)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Errorf("manual request during CLI sync: %d, %s", rec.Code, rec.Body.String())
		}
		req = httptest.NewRequest(http.MethodGet, "/api/v1/sync/status", nil)
		req.Header.Set("X-Api-Key", cfg.APIKey)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var status struct{ Running bool }
		if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil || !status.Running {
			t.Errorf("sync status must include CLI runs: %s, err=%v", rec.Body.String(), err)
		}
		cancel()
		time.Sleep(time.Second)
		synctest.Wait()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/sync/start", nil)
		req.Header.Set("X-Api-Key", cfg.APIKey)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("manual start after CLI completion: %d, %s", rec.Code, rec.Body.String())
		}
		if _, err := o.Sync(ctx, orchestrator.TriggerCLI); !errors.Is(err, orchestrator.ErrSyncInProgress) {
			t.Errorf("accepted HTTP start did not reserve sync: %v", err)
		}
		close(release)
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if o.IsSyncing() {
			t.Fatal("completed HTTP sync did not release its reservation")
		}
	})
}

func TestSyncStatusKeepsNewestReportWhenHistoryWriteFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.NewDefault()
		cfg.APIKey = "test-key"
		cfg.AniListUsername = "testuser"
		db := newTestStorage(t)
		client := anilist.NewClient("", anilist.WithHTTPClient(&http.Client{Transport: syncTransportFunc(
			func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{"data":{"MediaListCollection":{"lists":[],"hasNextChunk":false}}}`))}, nil
			})}))
		o := orchestrator.New(db, cfg, client, nil, nil)
		old, err := o.Sync(t.Context(), orchestrator.TriggerCLI)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TRIGGER reject_history BEFORE INSERT ON sync_history
				BEGIN SELECT RAISE(FAIL, 'history unavailable'); END;`)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		cfg.AniListUsername = ""
		o.UpdateConfig(*cfg)
		srv := NewServer(cfg, "", db, o, nil)
		handler := srv.Handler()
		time.Sleep(time.Second)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/start", nil)
		req.Header.Set("X-Api-Key", cfg.APIKey)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("sync start: %d, %s", rec.Code, rec.Body.String())
		}
		synctest.Wait()
		readStatus := func() *orchestrator.SyncReport {
			t.Helper()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/status", nil)
			req.Header.Set("X-Api-Key", cfg.APIKey)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			var status struct {
				LastReport *orchestrator.SyncReport `json:"lastReport"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil || status.LastReport == nil {
				t.Fatalf("sync status: %s, err=%v", rec.Body.String(), err)
			}
			return status.LastReport
		}
		latest := readStatus()
		if latest.Status != orchestrator.SyncStatusFailed || !latest.StartTime.After(old.StartTime) {
			t.Fatalf("old success masked the newer failure: %+v", latest)
		}
		if err := db.Write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "DROP TRIGGER reject_history")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		cfg.AniListUsername = "testuser"
		o.UpdateConfig(*cfg)
		newest, err := o.Sync(t.Context(), orchestrator.TriggerCLI)
		if err != nil {
			t.Fatal(err)
		}
		if got := readStatus(); got.RunID != newest.RunID || got.Status != orchestrator.SyncStatusSuccess {
			t.Fatalf("newer CLI history was hidden by cached failure: %+v", got)
		}
	})
}

func TestConfigEndpointResetsSchedulerIntervals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.NewDefault()
		cfg.APIKey = "test-key"
		cfg.SonarrURL, cfg.SonarrAPIKey = "http://sonarr.example", "key"
		db := newTestStorage(t)
		var usernames []string
		client := anilist.NewClient("", anilist.WithHTTPClient(&http.Client{Transport: syncTransportFunc(
			func(r *http.Request) (*http.Response, error) {
				var query struct {
					Variables struct {
						UserName string `json:"userName"`
					} `json:"variables"`
				}
				if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
					return nil, err
				}
				usernames = append(usernames, query.Variables.UserName)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{"data":{"MediaListCollection":{"lists":[],"hasNextChunk":false}}}`))}, nil
			})}))
		o := orchestrator.New(db, cfg, client, nil, nil)
		engine := backup.NewEngine(db, "", t.TempDir())
		scheduled := scheduler.New(*cfg, o, engine, nil)
		scheduled.Start(t.Context())
		defer scheduled.Stop()
		srv := NewServer(cfg, "", db, o, engine)
		srv.SetScheduler(scheduled)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(
			`{"syncIntervalHours":8,"backupInterval":2,"aniListUsername":"updated-user"}`))
		req.Header.Set("X-Api-Key", cfg.APIKey)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("config update: %d, %s", rec.Code, rec.Body.String())
		}
		status := scheduled.Status()
		if !status.Sync.Next.Equal(time.Now().Add(8*time.Hour)) || !status.Backup.Next.Equal(time.Now().Add(48*time.Hour)) {
			t.Fatalf("HTTP update did not reschedule tasks: %+v", status)
		}
		if _, err := o.Sync(t.Context(), orchestrator.TriggerManualWeb); err != nil {
			t.Fatal(err)
		}
		if len(usernames) == 0 {
			t.Fatal("sync made no AniList requests after saving the username")
		}
		for _, username := range usernames {
			if username != "updated-user" {
				t.Errorf("AniList request used stale username %q", username)
			}
		}
		// Rejected settings leave both the config and ticker deadlines untouched.
		req = httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(`{"backupInterval":0}`))
		req.Header.Set("X-Api-Key", cfg.APIKey)
		rec = httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || scheduled.Status() != status || srv.Config().BackupInterval != 2 {
			t.Fatalf("invalid interval: status=%d, schedule=%+v, backupInterval=%d", rec.Code, scheduled.Status(), srv.Config().BackupInterval)
		}
	})
}

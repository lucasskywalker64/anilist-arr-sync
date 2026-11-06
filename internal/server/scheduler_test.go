package server

import (
	"context"
	"encoding/json"
	"errors"
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

func TestConfigEndpointResetsSchedulerIntervals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.NewDefault()
		cfg.APIKey = "test-key"
		db := newTestStorage(t)
		o := orchestrator.New(db, cfg, anilist.NewClient(""), nil, nil)
		engine := backup.NewEngine(db, "", t.TempDir())
		scheduled := scheduler.New(*cfg, o, engine, nil)
		scheduled.Start(t.Context())
		defer scheduled.Stop()
		srv := NewServer(cfg, "", db, o, engine)
		srv.SetScheduler(scheduled)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(
			`{"syncIntervalHours":8,"backupInterval":2}`))
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
		// Rejected settings leave both the config and ticker deadlines untouched.
		req = httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(`{"backupInterval":0}`))
		req.Header.Set("X-Api-Key", cfg.APIKey)
		rec = httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || scheduled.Status() != status {
			t.Fatalf("invalid interval changed the schedule: %d, %+v", rec.Code, scheduled.Status())
		}
	})
}

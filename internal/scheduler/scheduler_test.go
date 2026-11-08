package scheduler_test

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/scheduler"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newOrchestrator(t *testing.T, cfg *config.Config, transport transportFunc) *orchestrator.Orchestrator {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if transport == nil {
		transport = func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"data":{"MediaListCollection":{"lists":[],"hasNextChunk":false}}}`)),
			}, nil
		}
	}
	client := anilist.NewClient("", anilist.WithHTTPClient(&http.Client{Transport: transport}))
	return orchestrator.New(db, cfg, client, nil, nil)
}

func TestUpdateChecksRespectSettingAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.NewDefault()
		started := make(chan struct{}, 1)
		cancelled := make(chan struct{})
		s := scheduler.New(*cfg, nil, nil, func(ctx context.Context) error {
			started <- struct{}{}
			<-ctx.Done()
			close(cancelled)
			return ctx.Err()
		})
		s.Start(t.Context())
		defer s.Stop()
		time.Sleep(24 * time.Hour)
		synctest.Wait()
		if !s.Status().Update.Next.IsZero() || len(started) != 0 {
			t.Fatal("automatic updates ran while disabled")
		}
		cfg.UpdateAutomatically = true
		s.UpdateConfig(*cfg)
		if got := s.Status().Update.Next; !got.Equal(time.Now().Add(24 * time.Hour)) {
			t.Fatalf("next update check: %v", got)
		}
		time.Sleep(24 * time.Hour)
		synctest.Wait()
		if len(started) != 1 {
			t.Fatal("enabled update check never ran")
		}
		if !s.Status().Update.Last.Equal(time.Now()) {
			t.Fatalf("active update check has no start timestamp: %+v", s.Status().Update)
		}
		s.Stop()
		select {
		case <-cancelled:
		default:
			t.Fatal("Stop did not cancel the active update check")
		}
		if !s.Status().Update.Next.IsZero() {
			t.Fatal("stopped scheduler still reports a next update check")
		}
		// Restart after an off-peak time, then disable checks again.
		s.Start(t.Context())
		cfg.UpdateAutomatically = false
		s.UpdateConfig(*cfg)
		time.Sleep(48 * time.Hour)
		synctest.Wait()
		if len(started) != 1 || !s.Status().Update.Next.IsZero() {
			t.Fatal("disabling automatic updates left a live ticker")
		}
	})
}

func TestScheduledSyncCancellationAndStartStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.NewDefault()
		cfg.AniListUsername = "testuser"
		cfg.SonarrURL, cfg.SonarrAPIKey = "http://sonarr.example", "key"
		entered, cancelled := make(chan struct{}), make(chan struct{})
		o := newOrchestrator(t, cfg, func(r *http.Request) (*http.Response, error) {
			close(entered)
			<-r.Context().Done()
			close(cancelled)
			return nil, r.Context().Err()
		})
		s := scheduler.New(*cfg, o, nil, nil)
		s.Start(t.Context())
		defer s.Stop()
		time.Sleep(4 * time.Hour)
		<-entered
		synctest.Wait()
		if !s.Status().Sync.Last.Equal(time.Now()) {
			t.Errorf("active scheduled sync has no start timestamp: %+v", s.Status().Sync)
		}
		s.Stop()
		select {
		case <-cancelled:
		default:
			t.Fatal("Stop did not cancel the scheduled AniList request")
		}
		if o.IsSyncing() {
			t.Fatal("Stop returned before scheduled sync released its guard")
		}
	})
}

func TestConfigChangesDuringShutdownDoNotRestartTimers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.NewDefault()
		cfg.UpdateAutomatically = true
		started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		s := scheduler.New(*cfg, nil, nil, func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release
			return ctx.Err()
		})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		s.Start(ctx)
		time.Sleep(24 * time.Hour)
		<-started
		cancel()
		<-cancelled
		synctest.Wait()
		cfg.UpdateAutomatically = false
		s.UpdateConfig(*cfg)
		cfg.UpdateAutomatically = true
		s.UpdateConfig(*cfg)
		if !s.Status().Update.Next.IsZero() {
			t.Error("config update restarted a timer during shutdown")
		}
		close(release)
		s.Stop()
	})
}

func TestBusySyncTicksAreSkippedAndContextCancellationStopsWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		previous := log.Writer()
		log.SetOutput(&logs)
		defer log.SetOutput(previous)
		cfg := config.NewDefault()
		cfg.AniListUsername = "testuser"
		cfg.SonarrURL, cfg.SonarrAPIKey = "http://sonarr.example", "key"
		entered := make(chan struct{}, 1)
		o := newOrchestrator(t, cfg, func(r *http.Request) (*http.Response, error) {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan struct{})
		go func() { _, _ = o.Sync(ctx, orchestrator.TriggerCLI); close(done) }()
		<-entered
		s := scheduler.New(*cfg, o, nil, nil)
		s.Start(ctx)
		s.Start(ctx)
		time.Sleep(12 * time.Hour)
		synctest.Wait()
		if got := strings.Count(logs.String(), "skipping sync tick"); got != 3 {
			t.Fatalf("expected three skipped ticks, got %d: %s", got, logs.String())
		}
		if !s.Status().Sync.Last.IsZero() {
			t.Fatal("skipped ticks must not count as executions")
		}
		cancel()
		synctest.Wait()
		s.Stop()
		<-done
		if !s.Status().Sync.Next.IsZero() {
			t.Fatal("parent cancellation did not stop scheduling")
		}
	})
}

func TestBackupsStartOffPeakAndRepeat(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.NewDefault()
		cfg.BackupInterval = 2
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config.xml")
		if err := config.Save(configPath, cfg); err != nil {
			t.Fatal(err)
		}
		db, err := storage.Open(filepath.Join(dir, "sync.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		engine := backup.NewEngine(db, configPath, filepath.Join(dir, "backups"))
		s := scheduler.New(*cfg, nil, engine, nil)
		s.Start(t.Context())
		defer s.Stop()
		now := time.Now().Local()
		first := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, time.Local)
		if !first.After(now) {
			first = first.AddDate(0, 0, 1)
		}
		if got := s.Status().Backup.Next; !got.Equal(first) {
			t.Fatalf("initial backup: got %v, want %v", got, first)
		}
		time.Sleep(time.Until(first) + 48*time.Hour + time.Second)
		synctest.Wait()
		archives, err := engine.List()
		if err != nil || len(archives) != 2 {
			t.Fatalf("backup archives: %v, err=%v", archives, err)
		}
		for _, archive := range archives {
			if archive.Type != backup.TypeScheduled {
				t.Errorf("backup type: %s", archive.Type)
			}
		}
		status := s.Status().Backup
		if !status.Last.Equal(first.Add(48*time.Hour)) || !status.Next.Equal(first.Add(96*time.Hour)) {
			t.Fatalf("backup status: %+v", status)
		}
		cfg.BackupInterval = 1
		changed := time.Now()
		s.UpdateConfig(*cfg)
		if got := s.Status().Backup.Next; !got.Equal(changed.Add(24 * time.Hour)) {
			t.Fatalf("updated backup schedule: %v", got)
		}
		time.Sleep(24*time.Hour + time.Second)
		synctest.Wait()
		archives, err = engine.List()
		if err != nil || len(archives) != 3 {
			t.Fatalf("backups after interval change: %v, err=%v", archives, err)
		}
	})
}

func TestRuntimeSyncIntervalUpdates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.NewDefault()
		cfg.AniListUsername = "testuser"
		cfg.SonarrURL, cfg.SonarrAPIKey = "http://sonarr.example", "key"
		o := newOrchestrator(t, cfg, nil)
		s := scheduler.New(*cfg, o, nil, nil)
		s.Start(t.Context())
		defer s.Stop()
		time.Sleep(time.Hour)
		cfg.SyncIntervalHours = 8
		s.UpdateConfig(*cfg)
		next := time.Now().Add(8 * time.Hour)
		if got := s.Status().Sync.Next; !got.Equal(next) {
			t.Fatalf("updated sync schedule: %v", got)
		}
		time.Sleep(time.Hour)
		cfg.InstanceName = "Changed name"
		s.UpdateConfig(*cfg)
		if got := s.Status().Sync.Next; !got.Equal(next) {
			t.Fatalf("unrelated config change reset sync: %v", got)
		}
		time.Sleep(2 * time.Hour)
		synctest.Wait()
		if history, err := o.GetSyncHistory(t.Context(), 10); err != nil || len(history) != 0 {
			t.Fatalf("old ticker still fired: %v, err=%v", history, err)
		}
		time.Sleep(5*time.Hour + time.Second)
		synctest.Wait()
		if history, err := o.GetSyncHistory(t.Context(), 10); err != nil || len(history) != 1 {
			t.Fatalf("new ticker did not fire: %v, err=%v", history, err)
		}
	})
}

func TestScheduledSyncCycles(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.NewDefault()
		cfg.AniListUsername = "testuser"
		cfg.SonarrURL, cfg.SonarrAPIKey = "http://sonarr.example", "key"
		o := newOrchestrator(t, cfg, nil)
		s := scheduler.New(*cfg, o, nil, nil)
		start := time.Now()
		s.Start(t.Context())
		defer s.Stop()
		if got := s.Status().Sync.Next; !got.Equal(start.Add(4 * time.Hour)) {
			t.Fatalf("first sync: %v", got)
		}
		time.Sleep(16*time.Hour + time.Second)
		synctest.Wait()
		history, err := o.GetSyncHistory(context.Background(), 10)
		if err != nil || len(history) != 4 {
			t.Fatalf("scheduled runs: %v, err=%v", history, err)
		}
		for _, run := range history {
			if run.Trigger != orchestrator.TriggerScheduled {
				t.Errorf("trigger: %s", run.Trigger)
			}
		}
		status := s.Status().Sync
		if !status.Last.Equal(start.Add(16*time.Hour)) || !status.Next.Equal(start.Add(20*time.Hour)) {
			t.Fatalf("sync status after four cycles: %+v", status)
		}
	})
}

package orchestrator_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

func TestSyncRejectsConcurrentTriggers(t *testing.T) {
	for _, active := range []orchestrator.TriggerType{
		orchestrator.TriggerScheduled, orchestrator.TriggerManualWeb, orchestrator.TriggerCLI,
	} {
		t.Run(string(active), func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				once.Do(func() { close(entered) })
				<-release
				_, _ = w.Write([]byte(`{"data":{"MediaListCollection":{"lists":[],"hasNextChunk":false}}}`))
			}))
			defer remote.Close()
			defer close(release)
			cfg := config.NewDefault()
			cfg.AniListUsername = "testuser"
			o := orchestrator.New(setupTestDB(t), cfg, anilist.NewClient(remote.URL), nil, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := o.Sync(ctx, active); done <- err }()
			select {
			case <-entered:
			case err := <-done:
				t.Fatalf("sync ended before reaching AniList: %v", err)
			case <-ctx.Done():
				t.Fatal("sync never reached AniList")
			}
			for _, trigger := range []orchestrator.TriggerType{
				orchestrator.TriggerScheduled, orchestrator.TriggerManualWeb, orchestrator.TriggerCLI,
			} {
				cancelled, stop := context.WithCancel(ctx)
				stop()
				if report, err := o.Sync(cancelled, trigger); !errors.Is(err, orchestrator.ErrSyncInProgress) || report != nil {
					t.Errorf("concurrent %s: report=%v err=%v", trigger, report, err)
				}
			}
			live, stop := context.WithTimeout(ctx, time.Second)
			if report, err := o.Sync(live, orchestrator.TriggerCLI); !errors.Is(err, orchestrator.ErrSyncInProgress) || report != nil || live.Err() != nil {
				t.Errorf("live CLI contender: report=%v err=%v context=%v", report, err, live.Err())
			}
			stop()
			cancel()
			<-done
			cancelled, stop := context.WithCancel(t.Context())
			stop()
			if _, err := o.Sync(cancelled, active); errors.Is(err, orchestrator.ErrSyncInProgress) {
				t.Fatal("cancelled run did not release the sync guard")
			}
		})
	}
}

func TestApplyBatchReservesSyncUntilAllActionsFinish(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	db := setupTestDB(t)
	if err := db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO staged_sync_actions (id, action_type, media_type, title, target_service, payload_json, status)
			VALUES (1, 'ADD_MOVIE', 'MOVIE', 'First', 'RADARR', '{"tmdbId":11}', 'PENDING'),
			       (2, 'ADD_MOVIE', 'MOVIE', 'Second', 'RADARR', '{"tmdbId":22}', 'PENDING');
		`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}, 1), make(chan struct{}, 1)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
			_, _ = w.Write([]byte(`{"id":1}`))
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(remote.Close)
	radarr, err := servarr.NewRadarrClient(remote.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.NewDefault()
	cfg.TagName = ""
	o := orchestrator.New(db, cfg, nil, nil, radarr)
	type applyResult struct {
		ids []int
		err error
	}
	done := make(chan applyResult, 1)
	go func() {
		applied, err := o.ApplyStagedActions(ctx, []int{1, 2})
		done <- applyResult{ids: applied, err: err}
	}()
	for i := range 2 {
		select {
		case <-entered:
		case result := <-done:
			t.Fatalf("apply ended before action %d: %+v", i+1, result)
		case <-ctx.Done():
			t.Fatalf("apply never reached action %d", i+1)
		}
		if o.IsSyncing() {
			t.Error("staged apply was reported as a sync")
		}
		cancelled, stop := context.WithCancel(ctx)
		stop()
		if _, err := o.StartSync(cancelled, orchestrator.TriggerScheduled); !errors.Is(err, orchestrator.ErrSyncInProgress) {
			t.Errorf("scheduled sync during action %d: %v", i+1, err)
		}
		if ids, err := o.ApplyStagedActions(cancelled, []int{1, 2}); !errors.Is(err, orchestrator.ErrSyncInProgress) || len(ids) != 0 {
			t.Errorf("concurrent batch during action %d: applied=%v err=%v", i+1, ids, err)
		}
		if ids, err := o.RejectStagedActions(ctx, []int{2, 1}); !errors.Is(err, orchestrator.ErrSyncInProgress) || len(ids) != 0 {
			t.Errorf("rejection during action %d: rejected=%v err=%v", i+1, ids, err)
		}
		if err := o.RejectStagedAction(ctx, i+1); !errors.Is(err, orchestrator.ErrSyncInProgress) {
			t.Errorf("single rejection during action %d: %v", i+1, err)
		}
		release <- struct{}{}
	}
	if result := <-done; result.err != nil || !slices.Equal(result.ids, []int{1, 2}) {
		t.Fatalf("expected both IDs applied in order: %+v", result)
	}
}

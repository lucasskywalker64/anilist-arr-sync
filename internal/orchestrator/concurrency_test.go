package orchestrator_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
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

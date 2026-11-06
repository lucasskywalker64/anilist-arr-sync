package orchestrator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func setupTestDB(t *testing.T) *storage.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_sync.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func setupMockAniList(t *testing.T, entries []anilist.MediaListEntry) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"data": map[string]any{
				"MediaListCollection": map[string]any{
					"hasNextChunk": false,
					"lists": []map[string]any{
						{
							"name":    "Watching",
							"status":  "CURRENT",
							"entries": entries,
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func setupMockSonarr(t *testing.T) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{})
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func setupMockRadarr(t *testing.T) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			_ = json.NewEncoder(w).Encode([]servarr.Movie{})
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestSync_SkipsIgnoredTitlesAndDiscardsNonVideo(t *testing.T) {
	db := setupTestDB(t)

	// Insert ignored title (AniList ID 100)
	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO ignored_titles (anilist_id, title_romaji, reason)
			VALUES (100, 'Ignored Anime', 'User preference');
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("failed to insert ignored title: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     100, // Present in ignored_titles
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Ignored Anime"},
			},
		},
		{
			ID:     2,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     200,
				Format: "MUSIC", // Non-video format, must discard immediately
				Title:  anilist.MediaTitle{Romaji: "Anime Music Video"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)
	sonarrSrv := setupMockSonarr(t)
	radarrSrv := setupMockRadarr(t)

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = true

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, err := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	if err != nil {
		t.Fatalf("failed to create Sonarr client: %v", err)
	}
	radarrClient, err := servarr.NewRadarrClient(radarrSrv.URL, "key")
	if err != nil {
		t.Fatalf("failed to create Radarr client: %v", err)
	}

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if report.ItemsScanned != 2 {
		t.Fatalf("expected 2 items scanned, got %d", report.ItemsScanned)
	}
	if report.QueuedReview != 0 {
		t.Fatalf("expected 0 items queued for review, got %d", report.QueuedReview)
	}

	// Verify nothing was added to review_queue or staged_sync_actions
	var reviewCount, stagedCount int
	_ = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		_ = q.QueryRowContext(ctx, "SELECT COUNT(*) FROM review_queue").Scan(&reviewCount)
		_ = q.QueryRowContext(ctx, "SELECT COUNT(*) FROM staged_sync_actions").Scan(&stagedCount)
		return nil
	})

	if reviewCount != 0 {
		t.Fatalf("expected 0 review queue rows, got %d", reviewCount)
	}
	if stagedCount != 0 {
		t.Fatalf("expected 0 staged sync action rows, got %d", stagedCount)
	}
}

func TestSync_DivertsUnmappedEntriesToReviewQueueAndRecordsMetrics(t *testing.T) {
	db := setupTestDB(t)

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     301,
				Format: "SPECIAL",
				Title:  anilist.MediaTitle{Romaji: "Attack on Titan OVA Special"},
			},
		},
		{
			ID:     2,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     302,
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Unknown New Anime"},
			},
		},
		{
			ID:     3,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     303,
				Format: "MOVIE",
				Title:  anilist.MediaTitle{Romaji: "Unknown New Movie"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)
	sonarrSrv := setupMockSonarr(t)
	radarrSrv := setupMockRadarr(t)

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if report.ItemsScanned != 3 {
		t.Fatalf("expected 3 items scanned, got %d", report.ItemsScanned)
	}
	if report.QueuedReview != 3 {
		t.Fatalf("expected 3 items queued for review, got %d", report.QueuedReview)
	}
	if report.Status != orchestrator.SyncStatusSuccess {
		t.Fatalf("expected status SUCCESS, got %s", report.Status)
	}

	// Verify review_queue table entries
	type queueRow struct {
		anilistID int
		mediaType string
		reason    string
		status    string
	}
	var rows []queueRow

	err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		dbRows, qErr := q.QueryContext(ctx, "SELECT anilist_id, media_type, reason, status FROM review_queue ORDER BY anilist_id ASC;")
		if qErr != nil {
			return qErr
		}
		defer func() { _ = dbRows.Close() }()

		for dbRows.Next() {
			var r queueRow
			if err := dbRows.Scan(&r.anilistID, &r.mediaType, &r.reason, &r.status); err != nil {
				return err
			}
			rows = append(rows, r)
		}
		return dbRows.Err()
	})
	if err != nil {
		t.Fatalf("query review_queue failed: %v", err)
	}

	if len(rows) != 3 {
		t.Fatalf("expected 3 review_queue rows, got %d", len(rows))
	}

	// 301 (SPECIAL) -> SPECIAL_WITHOUT_MAPPING, PENDING
	if rows[0].anilistID != 301 || rows[0].reason != "SPECIAL_WITHOUT_MAPPING" || rows[0].status != "PENDING" {
		t.Fatalf("unexpected row 0: %+v", rows[0])
	}
	// 302 (TV) -> SERIES, UNMAPPED, PENDING
	if rows[1].anilistID != 302 || rows[1].mediaType != "SERIES" || rows[1].reason != "UNMAPPED" || rows[1].status != "PENDING" {
		t.Fatalf("unexpected row 1: %+v", rows[1])
	}
	// 303 (MOVIE) -> MOVIE, UNMAPPED, PENDING
	if rows[2].anilistID != 303 || rows[2].mediaType != "MOVIE" || rows[2].reason != "UNMAPPED" || rows[2].status != "PENDING" {
		t.Fatalf("unexpected row 2: %+v", rows[2])
	}

	// Verify sync_history table
	var histCount int
	var histStatus, histTrigger string
	var histScanned, histQueued int
	err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, `
			SELECT COUNT(*), status, trigger_type, items_scanned, queued_review
			FROM sync_history
			WHERE id = ?;
		`, report.RunID).Scan(&histCount, &histStatus, &histTrigger, &histScanned, &histQueued)
	})
	if err != nil {
		t.Fatalf("query sync_history failed: %v", err)
	}

	if histCount != 1 || histStatus != "SUCCESS" || histTrigger != "MANUAL_WEB" || histScanned != 3 || histQueued != 3 {
		t.Fatalf("unexpected sync_history row: count=%d, status=%s, trigger=%s, scanned=%d, queued=%d",
			histCount, histStatus, histTrigger, histScanned, histQueued)
	}
}

func TestSync_DryRunStagesActionsAndAppliesOnConfirmation(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err1 := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons)
			VALUES (401, 'SERIES', 11111, '1');
		`)
		if err1 != nil {
			return err1
		}
		_, err2 := tx.ExecContext(ctx, `
			INSERT INTO fribb_entries (anilist_id, tmdb_id, media_type, tvdb_season)
			VALUES (402, 22222, 'MOVIE', 1);
		`)
		return err2
	})
	if err != nil {
		t.Fatalf("failed to insert mappings: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     401,
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Series To Stage"},
			},
		},
		{
			ID:     2,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     402,
				Format: "MOVIE",
				Title:  anilist.MediaTitle{Romaji: "Movie To Stage"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)

	sonarrAddCount := 0
	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/series":
			sonarrAddCount++
			_ = json.NewEncoder(w).Encode(servarr.Series{ID: 501, TVDBID: 11111, Title: "Series To Stage"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	radarrAddCount := 0
	radarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			_ = json.NewEncoder(w).Encode([]servarr.Movie{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/movie":
			radarrAddCount++
			_ = json.NewEncoder(w).Encode(servarr.Movie{ID: 601, TMDBID: 22222, Title: "Movie To Stage"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer radarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = true

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	_, err = orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if sonarrAddCount != 0 {
		t.Fatalf("expected 0 calls to Sonarr AddSeries during dry run, got %d", sonarrAddCount)
	}
	if radarrAddCount != 0 {
		t.Fatalf("expected 0 calls to Radarr AddMovie during dry run, got %d", radarrAddCount)
	}

	staged, err := orc.GetStagedActions(context.Background(), orchestrator.StagedStatusPending)
	if err != nil {
		t.Fatalf("GetStagedActions failed: %v", err)
	}
	if len(staged) != 2 {
		t.Fatalf("expected 2 staged actions, got %d", len(staged))
	}

	var seriesActionID, movieActionID int
	for _, a := range staged {
		switch a.ActionType {
		case orchestrator.ActionAddSeries:
			seriesActionID = a.ID
		case orchestrator.ActionAddMovie:
			movieActionID = a.ID
		}
	}

	if err := orc.ApplyStagedAction(context.Background(), seriesActionID); err != nil {
		t.Fatalf("ApplyStagedAction series failed: %v", err)
	}
	if sonarrAddCount != 1 {
		t.Fatalf("expected 1 call to Sonarr AddSeries after apply, got %d", sonarrAddCount)
	}

	if err := orc.ApplyStagedAction(context.Background(), movieActionID); err != nil {
		t.Fatalf("ApplyStagedAction movie failed: %v", err)
	}
	if radarrAddCount != 1 {
		t.Fatalf("expected 1 call to Radarr AddMovie after apply, got %d", radarrAddCount)
	}

	var seriesMonStatus, movieMonStatus string
	if err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		if err := q.QueryRowContext(ctx, "SELECT monitored_status FROM synced_media WHERE anilist_id = 401").Scan(&seriesMonStatus); err != nil {
			return err
		}
		return q.QueryRowContext(ctx, "SELECT monitored_status FROM synced_media WHERE anilist_id = 402").Scan(&movieMonStatus)
	}); err != nil {
		t.Fatalf("failed to query synced_media after staged apply: %v", err)
	}
	if seriesMonStatus != "MONITORED" {
		t.Fatalf("expected series 401 monitored_status = MONITORED after staged apply, got %q", seriesMonStatus)
	}
	if movieMonStatus != "MONITORED" {
		t.Fatalf("expected movie 402 monitored_status = MONITORED after staged apply, got %q", movieMonStatus)
	}

	pendingAfter, _ := orc.GetStagedActions(context.Background(), orchestrator.StagedStatusPending)
	if len(pendingAfter) != 0 {
		t.Fatalf("expected 0 pending actions, got %d", len(pendingAfter))
	}
	appliedAfter, _ := orc.GetStagedActions(context.Background(), orchestrator.StagedStatusApplied)
	if len(appliedAfter) != 2 {
		t.Fatalf("expected 2 applied actions, got %d", len(appliedAfter))
	}
}

func TestPurgeExpiredStagedActions(t *testing.T) {
	db := setupTestDB(t)

	// Insert an expired staged action (>7 days old) and a recent staged action (<1 day old)
	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err1 := tx.ExecContext(ctx, `
			INSERT INTO staged_sync_actions (action_type, media_type, title, target_service, payload_json, status, created_at)
			VALUES ('ADD_MOVIE', 'MOVIE', 'Old Movie', 'RADARR', '{}', 'PENDING', datetime('now', '-8 days'));
		`)
		if err1 != nil {
			return err1
		}
		_, err2 := tx.ExecContext(ctx, `
			INSERT INTO staged_sync_actions (action_type, media_type, title, target_service, payload_json, status, created_at)
			VALUES ('ADD_MOVIE', 'MOVIE', 'New Movie', 'RADARR', '{}', 'PENDING', datetime('now', '-1 hour'));
		`)
		return err2
	})
	if err != nil {
		t.Fatalf("failed to insert staged actions: %v", err)
	}

	cfg := config.NewDefault()
	orc := orchestrator.New(db, cfg, nil, nil, nil)

	deleted, err := orc.PurgeExpiredStagedActions(context.Background(), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("PurgeExpiredStagedActions failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted row, got %d", deleted)
	}

	staged, err := orc.GetStagedActions(context.Background(), orchestrator.StagedStatusPending)
	if err != nil {
		t.Fatalf("GetStagedActions failed: %v", err)
	}
	if len(staged) != 1 || staged[0].Title != "New Movie" {
		t.Fatalf("expected only 'New Movie' remaining, got %+v", staged)
	}
}

func TestApplyStagedAction_MonitorSeason_ParentSeriesError(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO staged_sync_actions (id, action_type, media_type, title, target_service, payload_json, status, created_at)
			VALUES (901, 'MONITOR_SEASON', 'SERIES', 'Test Series', 'SONARR', '{"seriesId":99,"tvdbId":111,"seasonNumber":1,"episodeIds":[1,2],"anilistId":801}', 'PENDING', datetime('now'));
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert staged action failed: %v", err)
	}

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/series/99" {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer sonarrSrv.Close()

	cfg := config.NewDefault()
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	orc := orchestrator.New(db, cfg, nil, sonarrClient, nil)

	applyErr := orc.ApplyStagedAction(context.Background(), 901)
	if applyErr == nil {
		t.Fatalf("expected ApplyStagedAction to fail when parent series cannot be fetched, got nil")
	}

	staged, getErr := orc.GetStagedActions(context.Background(), orchestrator.StagedStatusPending)
	if getErr != nil {
		t.Fatalf("GetStagedActions failed: %v", getErr)
	}
	if len(staged) != 1 || staged[0].ID != 901 {
		t.Fatalf("expected action 901 to stay PENDING, got: %+v", staged)
	}
}

func TestApplyStagedAction_MonitorSeason_ScopedByAnilistID(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO synced_media (anilist_id, media_type, format, title_romaji, anilist_status, target_service, tvdb_id, seasons, monitored_status)
			VALUES (1001, 'SERIES', 'TV', 'Cour 1', 'CURRENT', 'SONARR', 8888, '1', 'UNMONITORED'),
			       (1002, 'SERIES', 'TV', 'Cour 2', 'PLANNING', 'SONARR', 8888, '1', 'UNMONITORED');

			INSERT INTO staged_sync_actions (id, action_type, media_type, title, target_service, payload_json, status, created_at)
			VALUES (902, 'MONITOR_SEASON', 'SERIES', 'Cour 1', 'SONARR', '{"seriesId":88,"tvdbId":8888,"seasonNumber":1,"episodeIds":[1,2],"anilistId":1001}', 'PENDING', datetime('now'));
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("seed database failed: %v", err)
	}

	updatedSeriesMonitored := false
	monitoredEpisodesCalled := false
	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/88":
			_ = json.NewEncoder(w).Encode(servarr.Series{ID: 88, Monitored: false, Seasons: []servarr.Season{{SeasonNumber: 1, Monitored: false}}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/series":
			var s servarr.Series
			_ = json.NewDecoder(r.Body).Decode(&s)
			updatedSeriesMonitored = s.Monitored
			_ = json.NewEncoder(w).Encode(s)
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
			monitoredEpisodesCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	cfg := config.NewDefault()
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	orc := orchestrator.New(db, cfg, nil, sonarrClient, nil)

	if err := orc.ApplyStagedAction(context.Background(), 902); err != nil {
		t.Fatalf("ApplyStagedAction failed: %v", err)
	}

	if !updatedSeriesMonitored {
		t.Errorf("expected parent series to be updated to monitored=true")
	}
	if !monitoredEpisodesCalled {
		t.Errorf("expected MonitorEpisodes to be called")
	}

	var status1001, status1002 string
	if readErr := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		if qErr := q.QueryRowContext(ctx, "SELECT monitored_status FROM synced_media WHERE anilist_id = 1001;").Scan(&status1001); qErr != nil {
			return qErr
		}
		return q.QueryRowContext(ctx, "SELECT monitored_status FROM synced_media WHERE anilist_id = 1002;").Scan(&status1002)
	}); readErr != nil {
		t.Fatalf("query synced_media failed: %v", readErr)
	}

	if status1001 != "MONITORED" {
		t.Errorf("expected anilist_id 1001 to be MONITORED, got %q", status1001)
	}
	if status1002 != "UNMONITORED" {
		t.Errorf("expected anilist_id 1002 to remain UNMONITORED, got %q", status1002)
	}
}

func TestSync_ActiveModeSonarr_AdditiveAdditionAndSearchDispatch(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons)
			VALUES (501, 'SERIES', 77777, '2');
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert mapping failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     501,
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Jujutsu Kaisen Season 2"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)

	addedSeries := false

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 42, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{
				{
					TVDBID: 77777,
					Title:  "Jujutsu Kaisen",
					Seasons: []servarr.Season{
						{SeasonNumber: 1, Monitored: true},
						{SeasonNumber: 2, Monitored: false},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/series":
			addedSeries = true
			var body servarr.AddSeriesRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Tags) == 0 || body.Tags[0] != 42 {
				t.Errorf("expected tag 42, got %v", body.Tags)
			}
			if len(body.Seasons) != 2 || body.Seasons[0].Monitored || !body.Seasons[1].Monitored {
				t.Errorf("expected season 2 monitored and season 1 unmonitored on add, got %v", body.Seasons)
			}
			if body.AddOptions == nil || !body.AddOptions.SearchForMissingEpisodes {
				t.Errorf("expected SearchForMissingEpisodes to be true in AddOptions")
			}
			_ = json.NewEncoder(w).Encode(servarr.Series{
				ID:     801,
				TVDBID: 77777,
				Title:  body.Title,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false // Active mode
	cfg.SonarrSearchOnAdd = true

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient("http://localhost:1234", "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !addedSeries {
		t.Fatal("expected Sonarr AddSeries to be called")
	}
	if report.MonitoredSonarr != 1 {
		t.Fatalf("expected MonitoredSonarr=1, got %d", report.MonitoredSonarr)
	}
}

func TestSync_ActiveModeSonarr_SelectiveMonitoringAndSplitCour(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons)
			VALUES (601, 'SERIES', 88888, '1');
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert mapping failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     601,
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Bleach: Thousand-Year Blood War Part 2"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)

	episodesMonitored := false
	var monitoredEpisodeIDs []int

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 42, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{
				{
					ID:        901, // Already exists in library
					TVDBID:    88888,
					Title:     "Bleach: Thousand-Year Blood War",
					Monitored: true,
					Tags:      []int{42},
					Seasons: []servarr.Season{
						{SeasonNumber: 1, Monitored: true},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			_ = json.NewEncoder(w).Encode([]servarr.Episode{
				{ID: 1001, SeriesID: 901, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true, AirDate: "2022-10-11"},
				{ID: 1013, SeriesID: 901, SeasonNumber: 1, EpisodeNumber: 13, Monitored: true, FinaleType: "midseason", AirDate: "2022-12-27"},
				{ID: 1014, SeriesID: 901, SeasonNumber: 1, EpisodeNumber: 14, Monitored: false, AirDate: "2023-07-08"},
				{ID: 1026, SeriesID: 901, SeasonNumber: 1, EpisodeNumber: 26, Monitored: false, FinaleType: "season", AirDate: "2023-09-30"},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
			episodesMonitored = true
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if ids, ok := body["episodeIds"].([]any); ok {
				for _, id := range ids {
					monitoredEpisodeIDs = append(monitoredEpisodeIDs, int(id.(float64)))
				}
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/command":
			_ = json.NewEncoder(w).Encode(servarr.Command{ID: 1, Name: "SeasonSearch", Status: "queued"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false // Active mode

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient("http://localhost:1234", "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !episodesMonitored {
		t.Fatal("expected MonitorEpisodes to be called for Part 2 episode slice")
	}
	if len(monitoredEpisodeIDs) != 2 || monitoredEpisodeIDs[0] != 1014 || monitoredEpisodeIDs[1] != 1026 {
		t.Fatalf("expected episode IDs [1014, 1026], got %v", monitoredEpisodeIDs)
	}
	if report.MonitoredSonarr != 1 {
		t.Fatalf("expected MonitoredSonarr=1, got %d", report.MonitoredSonarr)
	}
}

func TestSync_ActiveModeSonarr_AdditivePreservesUntaggedAndExistingMedia(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons)
			VALUES (701, 'SERIES', 99999, '2');
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert mapping failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     701,
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Untagged Existing Series S2"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)

	seriesUpdated := false
	var updatedSeries servarr.Series

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 42, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{
				{
					ID:        950,
					TVDBID:    99999,
					Title:     "Untagged Existing Series",
					Monitored: true,
					Tags:      []int{}, // Untagged!
					Seasons: []servarr.Season{
						{SeasonNumber: 1, Monitored: true}, // Existing season 1 monitored
						{SeasonNumber: 2, Monitored: false},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			_ = json.NewEncoder(w).Encode([]servarr.Episode{})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/series":
			seriesUpdated = true
			_ = json.NewDecoder(r.Body).Decode(&updatedSeries)
			_ = json.NewEncoder(w).Encode(updatedSeries)
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient("http://localhost:1234", "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !seriesUpdated {
		t.Fatal("expected UpdateSeries to be called")
	}
	// Season 1 must remain monitored (additive behavior)
	if !updatedSeries.Seasons[0].Monitored {
		t.Fatal("expected Season 1 to remain monitored")
	}
	// Season 2 must now be monitored
	if !updatedSeries.Seasons[1].Monitored {
		t.Fatal("expected Season 2 to be monitored")
	}
	if report.MonitoredSonarr != 1 {
		t.Fatalf("expected MonitoredSonarr=1, got %d", report.MonitoredSonarr)
	}
}

func TestSync_ActiveMode_UnmonitorDroppedOnlyWhenEnabledAndTagged(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err1 := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons)
			VALUES (801, 'SERIES', 10101, '1');
		`)
		if err1 != nil {
			return err1
		}
		_, err2 := tx.ExecContext(ctx, `
			INSERT INTO fribb_entries (anilist_id, tmdb_id, media_type, tvdb_season)
			VALUES (802, 20202, 'MOVIE', 1);
		`)
		if err2 != nil {
			return err2
		}
		_, err3 := tx.ExecContext(ctx, `
			INSERT INTO fribb_entries (anilist_id, tmdb_id, media_type, tvdb_season)
			VALUES (803, 30303, 'MOVIE', 1);
		`)
		return err3
	})
	if err != nil {
		t.Fatalf("insert mappings failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusDropped, // DROPPED, carries managed tag (42)
			Media: anilist.Media{
				ID:     801,
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Dropped Series Managed"},
			},
		},
		{
			ID:     2,
			Status: anilist.StatusDropped, // DROPPED, carries managed tag (42)
			Media: anilist.Media{
				ID:     802,
				Format: "MOVIE",
				Title:  anilist.MediaTitle{Romaji: "Dropped Movie Managed"},
			},
		},
		{
			ID:     3,
			Status: anilist.StatusDropped, // DROPPED, but UNTAGGED!
			Media: anilist.Media{
				ID:     803,
				Format: "MOVIE",
				Title:  anilist.MediaTitle{Romaji: "Dropped Movie Untagged"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)

	sonarrSeriesUpdated := false
	radarrMovieUpdatedCount := 0

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 42, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{
				{
					ID:        991,
					TVDBID:    10101,
					Title:     "Dropped Series Managed",
					Monitored: true,
					Tags:      []int{42}, // Tagged!
					Seasons:   []servarr.Season{{SeasonNumber: 1, Monitored: true}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			_ = json.NewEncoder(w).Encode([]servarr.Episode{})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/series":
			sonarrSeriesUpdated = true
			var body servarr.Series
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Seasons[0].Monitored {
				t.Errorf("expected Season 1 to be unmonitored")
				http.Error(w, "expected Season 1 to be unmonitored", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	radarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 42, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			if r.URL.Query().Get("tmdbId") == "20202" {
				_ = json.NewEncoder(w).Encode([]servarr.Movie{{
					ID:        992,
					TMDBID:    20202,
					Title:     "Dropped Movie Managed",
					Monitored: true,
					Tags:      []int{42}, // Tagged!
				}})
				return
			}
			if r.URL.Query().Get("tmdbId") == "30303" {
				_ = json.NewEncoder(w).Encode([]servarr.Movie{{
					ID:        993,
					TMDBID:    30303,
					Title:     "Dropped Movie Untagged",
					Monitored: true,
					Tags:      []int{}, // Untagged!
				}})
				return
			}
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/movie":
			radarrMovieUpdatedCount++
			var body servarr.Movie
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.TMDBID == 30303 {
				t.Errorf("untagged dropped movie 30303 must NOT be unmonitored")
				http.Error(w, "untagged dropped movie 30303 must NOT be unmonitored", http.StatusBadRequest)
				return
			}
			if body.Monitored {
				t.Errorf("expected managed movie 20202 to be unmonitored")
				http.Error(w, "expected managed movie 20202 to be unmonitored", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer radarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false
	cfg.UnmonitorDropped = true // Enabled

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !sonarrSeriesUpdated {
		t.Fatal("expected tagged dropped series to be unmonitored in Sonarr")
	}
	if radarrMovieUpdatedCount != 1 {
		t.Fatalf("expected exactly 1 call to update Radarr movie (only tagged), got %d", radarrMovieUpdatedCount)
	}
	if report.UnmonitoredSonarr != 1 {
		t.Fatalf("expected UnmonitoredSonarr=1, got %d", report.UnmonitoredSonarr)
	}
	if report.UnmonitoredRadarr != 1 {
		t.Fatalf("expected UnmonitoredRadarr=1, got %d", report.UnmonitoredRadarr)
	}
}

func TestSync_ActiveModeRadarr_AdditiveAdditionAndSearchDispatch(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO fribb_entries (anilist_id, tmdb_id, media_type, tvdb_season)
			VALUES (901, 55555, 'MOVIE', 1);
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert mapping failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     901,
				Format: "MOVIE",
				Title:  anilist.MediaTitle{Romaji: "Spirited Away"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)

	addedMovie := false

	radarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 42, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			_ = json.NewEncoder(w).Encode([]servarr.Movie{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/movie":
			addedMovie = true
			var body servarr.AddMovieRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Tags) == 0 || body.Tags[0] != 42 {
				t.Errorf("expected tag 42, got %v", body.Tags)
			}
			if body.AddOptions == nil || !body.AddOptions.SearchForMovie {
				t.Errorf("expected SearchForMovie in AddOptions to be true")
			}
			_ = json.NewEncoder(w).Encode(servarr.Movie{
				ID:     777,
				TMDBID: 55555,
				Title:  body.Title,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer radarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false
	cfg.RadarrSearchOnAdd = true

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient("http://localhost:1234", "key")
	radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !addedMovie {
		t.Fatal("expected Radarr AddMovie to be called")
	}
	if report.AddedRadarr != 1 {
		t.Fatalf("expected AddedRadarr=1, got %d", report.AddedRadarr)
	}
}

func TestGetSyncHistory(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err1 := tx.ExecContext(ctx, `
			INSERT INTO sync_history (
				duration_ms, status, items_scanned, added_radarr,
				monitored_sonarr, unmonitored_sonarr, unmonitored_radarr,
				queued_review, errors_json, trigger_type
			)
			VALUES (120, 'SUCCESS', 10, 1, 2, 0, 0, 1, '[]', 'CLI');
		`)
		return err1
	})
	if err != nil {
		t.Fatalf("insert history failed: %v", err)
	}

	orc := orchestrator.New(db, config.NewDefault(), nil, nil, nil)
	records, err := orc.GetSyncHistory(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetSyncHistory failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(records))
	}
	if records[0].Status != orchestrator.SyncStatusSuccess || records[0].Trigger != orchestrator.TriggerCLI {
		t.Fatalf("unexpected record: %+v", records[0])
	}
}

func TestSync_ActiveModeSonarr_AppendsMissingTargetSeasonOnAdd(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons)
			VALUES (701, 'SERIES', 99999, '3');
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert mapping failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     701,
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Anime Season 3"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)

	addedSeries := false
	var capturedSeasons []servarr.Season

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 42, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			// Lookup only knows seasons 1 and 2
			_ = json.NewEncoder(w).Encode([]servarr.Series{
				{
					TVDBID: 99999,
					Title:  "Anime",
					Seasons: []servarr.Season{
						{SeasonNumber: 1, Monitored: true},
						{SeasonNumber: 2, Monitored: true},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/series":
			addedSeries = true
			var body servarr.AddSeriesRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			capturedSeasons = body.Seasons
			_ = json.NewEncoder(w).Encode(servarr.Series{
				ID:     802,
				TVDBID: 99999,
				Title:  body.Title,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient("http://localhost:1234", "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !addedSeries {
		t.Fatal("expected Sonarr AddSeries to be called")
	}
	if len(capturedSeasons) != 3 {
		t.Fatalf("expected 3 seasons, got %d: %+v", len(capturedSeasons), capturedSeasons)
	}
	// Season 1 & 2 must be false, Season 3 must be true
	if capturedSeasons[0].Monitored || capturedSeasons[1].Monitored || !capturedSeasons[2].Monitored {
		t.Fatalf("expected season 3 monitored and seasons 1, 2 unmonitored: %+v", capturedSeasons)
	}
	if report.MonitoredSonarr != 1 {
		t.Fatalf("expected MonitoredSonarr=1, got %d", report.MonitoredSonarr)
	}
}

func TestSync_ActiveModeSonarr_SplitCourUnmonitorIdempotent(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons)
			VALUES (602, 'SERIES', 88889, '1');
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert mapping failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusDropped,
			Media: anilist.Media{
				ID:     602,
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Bleach: Thousand-Year Blood War Part 2"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)

	episodesUnmonitorCalled := false

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 42, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{
				{
					ID:        902,
					TVDBID:    88889,
					Title:     "Bleach: Thousand-Year Blood War",
					Monitored: true,
					Tags:      []int{42},
					Seasons:   []servarr.Season{{SeasonNumber: 1, Monitored: true}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			// Part 2 episodes (14-26) are ALREADY unmonitored
			_ = json.NewEncoder(w).Encode([]servarr.Episode{
				{ID: 2001, SeriesID: 902, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true, AirDate: "2022-10-11"},
				{ID: 2013, SeriesID: 902, SeasonNumber: 1, EpisodeNumber: 13, Monitored: true, FinaleType: "midseason", AirDate: "2022-12-27"},
				{ID: 2014, SeriesID: 902, SeasonNumber: 1, EpisodeNumber: 14, Monitored: false, AirDate: "2023-07-08"},
				{ID: 2026, SeriesID: 902, SeasonNumber: 1, EpisodeNumber: 26, Monitored: false, FinaleType: "season", AirDate: "2023-09-30"},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
			episodesUnmonitorCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false
	cfg.UnmonitorDropped = true

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient("http://localhost:1234", "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if episodesUnmonitorCalled {
		t.Fatal("expected MonitorEpisodes not to be called when all slice episodes are already unmonitored")
	}
	if report.UnmonitoredSonarr != 0 {
		t.Fatalf("expected UnmonitoredSonarr=0, got %d", report.UnmonitoredSonarr)
	}
}

func TestSync_UnmonitorZeroTagDoesNotMatchUntagged(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err1 := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons)
			VALUES (881, 'SERIES', 10102, '1');
		`)
		if err1 != nil {
			return err1
		}
		_, err2 := tx.ExecContext(ctx, `
			INSERT INTO fribb_entries (anilist_id, tmdb_id, media_type, tvdb_season)
			VALUES (882, 20203, 'MOVIE', 1);
		`)
		return err2
	})
	if err != nil {
		t.Fatalf("insert mappings failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusDropped,
			Media:  anilist.Media{ID: 881, Format: "TV", Title: anilist.MediaTitle{Romaji: "Series Untagged Zero"}},
		},
		{
			ID:     2,
			Status: anilist.StatusDropped,
			Media:  anilist.Media{ID: 882, Format: "MOVIE", Title: anilist.MediaTitle{Romaji: "Movie Untagged Zero"}},
		},
	}

	alSrv := setupMockAniList(t, entries)

	sonarrUpdated := false
	radarrUpdated := false

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			// Series has tag 0
			_ = json.NewEncoder(w).Encode([]servarr.Series{
				{
					ID: 994, TVDBID: 10102, Title: "Series Untagged Zero", Monitored: true,
					Tags:    []int{0},
					Seasons: []servarr.Season{{SeasonNumber: 1, Monitored: true}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			_ = json.NewEncoder(w).Encode([]servarr.Episode{})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/series":
			sonarrUpdated = true
			http.Error(w, "should not be updated", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	radarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			// Movie has tag 0
			_ = json.NewEncoder(w).Encode([]servarr.Movie{{
				ID: 995, TMDBID: 20203, Title: "Movie Untagged Zero", Monitored: true,
				Tags: []int{0},
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/movie":
			radarrUpdated = true
			http.Error(w, "should not be updated", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer radarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false
	cfg.UnmonitorDropped = true
	cfg.TagName = "" // Tag tracking disabled -> managedTagID = 0

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if sonarrUpdated {
		t.Fatal("expected Sonarr series with tag 0 not to be unmonitored when TagName is empty")
	}
	if radarrUpdated {
		t.Fatal("expected Radarr movie with tag 0 not to be unmonitored when TagName is empty")
	}
	if report.UnmonitoredSonarr != 0 {
		t.Fatalf("expected UnmonitoredSonarr=0, got %d", report.UnmonitoredSonarr)
	}
	if report.UnmonitoredRadarr != 0 {
		t.Fatalf("expected UnmonitoredRadarr=0, got %d", report.UnmonitoredRadarr)
	}
}

func TestSync_SyncedMedia_DryRunUpsertsUnmonitored(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tmdb_id)
			VALUES (1001, 'MOVIE', 55555);
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert mapping failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     1001,
				Format: "MOVIE",
				Title:  anilist.MediaTitle{Romaji: "Dry Run Movie"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)
	radarrSrv := setupMockRadarr(t)
	sonarrSrv := setupMockSonarr(t)

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = true

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)

	report, syncErr := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if syncErr != nil {
		t.Fatalf("Sync failed: %v", syncErr)
	}
	t.Logf("report: %+v", report)

	var status string
	if err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT monitored_status FROM synced_media WHERE anilist_id = 1001").Scan(&status)
	}); err != nil {
		t.Fatalf("failed to query synced_media row: %v", err)
	}
	if status != "UNMONITORED" {
		t.Fatalf("expected monitored_status = UNMONITORED on dry run, got %q", status)
	}
}

func TestSync_SyncedMedia_NilServarrClientNoUpsert(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tmdb_id)
			VALUES (1002, 'MOVIE', 55556);
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert mapping failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     1002,
				Format: "MOVIE",
				Title:  anilist.MediaTitle{Romaji: "Nil Client Movie"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)
	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false

	alClient := anilist.NewClient(alSrv.URL)

	// radarrClient is nil
	orc := orchestrator.New(db, cfg, alClient, nil, nil)

	report, syncErr := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if syncErr != nil {
		t.Fatalf("Sync failed: %v", syncErr)
	}
	t.Logf("report: %+v", report)

	var count int
	if err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM synced_media WHERE anilist_id = 1002").Scan(&count)
	}); err != nil {
		t.Fatalf("failed to count synced_media rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 synced_media rows when client is nil, got %d", count)
	}
}

func TestSync_SyncedMedia_ServarrAPIErrorNoUpsert(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tmdb_id)
			VALUES (1003, 'MOVIE', 55557);
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert mapping failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     1003,
				Format: "MOVIE",
				Title:  anilist.MediaTitle{Romaji: "Error Movie"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)
	radarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			http.Error(w, "internal server error", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer radarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false

	alClient := anilist.NewClient(alSrv.URL)
	radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, nil, radarrClient)

	report, syncErr := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if syncErr != nil {
		t.Fatalf("Sync failed: %v", syncErr)
	}
	if len(report.Errors) == 0 {
		t.Fatal("expected report errors for API 500 error")
	}

	var count int
	if err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM synced_media WHERE anilist_id = 1003").Scan(&count)
	}); err != nil {
		t.Fatalf("failed to count synced_media rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 synced_media rows on Servarr API error, got %d", count)
	}
}

func TestSync_SyncedMedia_StatusDropped_UnmonitorDroppedFalse(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tmdb_id)
			VALUES (1004, 'MOVIE', 55558);
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert mapping failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusDropped,
			Media: anilist.Media{
				ID:     1004,
				Format: "MOVIE",
				Title:  anilist.MediaTitle{Romaji: "Dropped Keep Monitored Movie"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)
	radarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			_ = json.NewEncoder(w).Encode([]servarr.Movie{{
				ID:        777,
				TMDBID:    55558,
				Title:     "Dropped Keep Monitored Movie",
				Monitored: true,
				Tags:      []int{1},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer radarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false
	cfg.UnmonitorDropped = false // explicitly false

	alClient := anilist.NewClient(alSrv.URL)
	radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, nil, radarrClient)

	report, syncErr := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if syncErr != nil {
		t.Fatalf("Sync failed: %v", syncErr)
	}
	t.Logf("report: %+v", report)

	var status string
	err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT monitored_status FROM synced_media WHERE anilist_id = 1004").Scan(&status)
	})
	if err != nil {
		t.Fatalf("failed to query synced_media: %v", err)
	}
	if status != "MONITORED" {
		t.Fatalf("expected monitored_status = MONITORED when UnmonitorDropped is false, got %q", status)
	}
}

func TestSync_SyncedMedia_SonarrSuccess(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons)
			VALUES (1005, 'SERIES', 44444, '1');
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("insert mapping failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     1005,
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Sonarr Synced Show"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)
	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{
				{
					ID:        999,
					TVDBID:    44444,
					Title:     "Sonarr Synced Show",
					Monitored: true,
					Tags:      []int{1},
					Seasons: []servarr.Season{
						{SeasonNumber: 1, Monitored: true},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			_ = json.NewEncoder(w).Encode([]servarr.Episode{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, nil)

	report, syncErr := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if syncErr != nil {
		t.Fatalf("Sync failed: %v", syncErr)
	}
	t.Logf("report: %+v", report)

	var status, targetService string
	var seasons sql.NullString
	err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT monitored_status, target_service, seasons FROM synced_media WHERE anilist_id = 1005").Scan(&status, &targetService, &seasons)
	})
	if err != nil {
		t.Fatalf("failed to query synced_media: %v", err)
	}
	if status != "MONITORED" {
		t.Fatalf("expected monitored_status = MONITORED, got %q", status)
	}
	if targetService != "SONARR" {
		t.Fatalf("expected target_service = SONARR, got %q", targetService)
	}
	if !seasons.Valid || seasons.String != "1" {
		t.Fatalf("expected seasons = 1, got %v", seasons)
	}
}

func TestSync_SyncedMedia_SonarrUnmonitoredParentSeriesAndIgnoredPreservesMapping(t *testing.T) {
	db := setupTestDB(t)

	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if _, writeErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons)
			VALUES (1006, 'SERIES', 44445, '1');
		`); writeErr != nil {
			return writeErr
		}
		if _, writeErr := tx.ExecContext(ctx, `
			INSERT INTO synced_media (
				anilist_id, media_type, format, title_romaji, anilist_status,
				target_service, tvdb_id, arr_title, monitored_status, seasons
			) VALUES (
				1007, 'SERIES', 'TV', 'Mapped Then Ignored', 'CURRENT',
				'SONARR', 77777, 'Custom Arr Title', 'MONITORED', '2'
			);
		`); writeErr != nil {
			return writeErr
		}
		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO ignored_titles (anilist_id, title_romaji, reason)
			VALUES (1007, 'Mapped Then Ignored', 'Blocked');
		`)
		return writeErr
	})
	if err != nil {
		t.Fatalf("seed db failed: %v", err)
	}

	entries := []anilist.MediaListEntry{
		{
			ID:     1,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     1006,
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Unmonitored Parent Series"},
			},
		},
		{
			ID:     2,
			Status: anilist.StatusCurrent,
			Media: anilist.Media{
				ID:     1007,
				Format: "TV",
				Title:  anilist.MediaTitle{Romaji: "Mapped Then Ignored"},
			},
		},
	}

	alSrv := setupMockAniList(t, entries)
	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{
				{
					ID:        998,
					TVDBID:    44445,
					Title:     "Unmonitored Parent Series",
					Monitored: false,
					Tags:      []int{1},
					Seasons: []servarr.Season{
						{SeasonNumber: 1, Monitored: true},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			_ = json.NewEncoder(w).Encode([]servarr.Episode{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "testuser"
	cfg.FirstRunDryRun = false

	alClient := anilist.NewClient(alSrv.URL)
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, nil)

	if _, syncErr := orc.Sync(context.Background(), orchestrator.TriggerManualWeb); syncErr != nil {
		t.Fatalf("Sync failed: %v", syncErr)
	}

	var status1006 string
	if err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT monitored_status FROM synced_media WHERE anilist_id = 1006").Scan(&status1006)
	}); err != nil {
		t.Fatalf("failed to query synced_media 1006: %v", err)
	}
	if status1006 != "UNMONITORED" {
		t.Fatalf("expected monitored_status = UNMONITORED when parent series is unmonitored, got %q", status1006)
	}

	var status1007, targetSvc1007 string
	var tvdb1007 sql.NullInt64
	var arrTitle1007, seasons1007 sql.NullString
	if err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT monitored_status, target_service, tvdb_id, arr_title, seasons FROM synced_media WHERE anilist_id = 1007").
			Scan(&status1007, &targetSvc1007, &tvdb1007, &arrTitle1007, &seasons1007)
	}); err != nil {
		t.Fatalf("failed to query synced_media 1007: %v", err)
	}
	if status1007 != "IGNORED" || targetSvc1007 != "SONARR" || tvdb1007.Int64 != 77777 || arrTitle1007.String != "Custom Arr Title" || seasons1007.String != "2" {
		t.Fatalf("expected ignored sync to preserve mapping columns, got status=%s service=%s tvdb=%v arrTitle=%v seasons=%v",
			status1007, targetSvc1007, tvdb1007, arrTitle1007, seasons1007)
	}
}

func TestSync_WatchlistStatusReconciliation(t *testing.T) {
	db := setupTestDB(t)

	// Seed 4 titles in synced_media all with CURRENT
	pastTime := time.Now().Add(-2 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	_ = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		// Custom override for 101
		_, err := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons, updated_at)
			VALUES (101, 'SERIES', 9001, '1', CURRENT_TIMESTAMP);
		`)
		if err != nil {
			return err
		}

		for _, id := range []int{101, 102, 103, 104} {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO synced_media (
					anilist_id, media_type, format, title_romaji, anilist_status,
					monitored_status, last_synced_at, updated_at
				) VALUES (?, 'SERIES', 'TV', 'Anime', 'CURRENT', 'MONITORED', ?, ?);
			`, id, pastTime, pastTime)
			if err != nil {
				return err
			}
		}
		return nil
	})

	// Setup mock AniList
	alSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		q, _ := body["query"].(string)

		w.Header().Set("Content-Type", "application/json")

		// Overview query returns:
		// 101 -> COMPLETED
		// 102 -> DROPPED
		// 103 -> PAUSED
		// 104 -> missing entirely
		if strings.Contains(q, "mediaId") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"MediaListCollection": map[string]any{
						"hasNextChunk": false,
						"lists": []map[string]any{
							{
								"name":   "Completed",
								"status": "COMPLETED",
								"entries": []map[string]any{
									{"id": 1, "mediaId": 101, "status": "COMPLETED", "progress": 12},
								},
							},
							{
								"name":   "Dropped",
								"status": "DROPPED",
								"entries": []map[string]any{
									{"id": 2, "mediaId": 102, "status": "DROPPED", "progress": 3},
								},
							},
							{
								"name":   "Paused",
								"status": "PAUSED",
								"entries": []map[string]any{
									{"id": 3, "mediaId": 103, "status": "PAUSED", "progress": 5},
								},
							},
						},
					},
				},
			})
			return
		}

		// Watchlist query for CURRENT returns empty
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"MediaListCollection": map[string]any{
					"hasNextChunk": false,
					"lists":        []map[string]any{},
				},
			},
		})
	}))
	defer alSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "reconcileuser"
	cfg.FirstRunDryRun = false

	alClient := anilist.NewClient(alSrv.URL)
	orc := orchestrator.New(db, cfg, alClient, nil, nil)

	report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if report.Status == orchestrator.SyncStatusFailed {
		t.Fatalf("Sync reported failed status: %v", report.Errors)
	}

	// Verify statuses in synced_media
	statusMap := make(map[int]string)
	updatedMap := make(map[int]string)
	var totalRows int
	if err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		rows, err := q.QueryContext(ctx, "SELECT anilist_id, anilist_status, updated_at FROM synced_media")
		if err != nil {
			return err
		}
		defer func() {
			_ = rows.Close()
		}()
		for rows.Next() {
			var id int
			var st, upd string
			if err := rows.Scan(&id, &st, &upd); err != nil {
				return err
			}
			statusMap[id] = st
			updatedMap[id] = upd
			totalRows++
		}
		return nil
	}); err != nil {
		t.Fatalf("query synced_media: %v", err)
	}

	if totalRows != 4 {
		t.Fatalf("expected 4 total rows preserved, got %d", totalRows)
	}

	if statusMap[101] != "COMPLETED" {
		t.Errorf("expected id 101 status COMPLETED, got %s", statusMap[101])
	}
	if statusMap[102] != "DROPPED" {
		t.Errorf("expected id 102 status DROPPED, got %s", statusMap[102])
	}
	if statusMap[103] != "PAUSED" {
		t.Errorf("expected id 103 status PAUSED, got %s", statusMap[103])
	}
	if statusMap[104] != "REMOVED" {
		t.Errorf("expected id 104 status REMOVED, got %s", statusMap[104])
	}

	for _, id := range []int{101, 102, 103, 104} {
		if updatedMap[id] == pastTime {
			t.Errorf("expected updated_at for id %d to be refreshed, but remained %s", id, pastTime)
		}
	}

	// Verify custom override for 101 was not deleted
	var overrideCount int
	if err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM mapping_overrides WHERE anilist_id = 101").Scan(&overrideCount)
	}); err != nil {
		t.Fatalf("query mapping_overrides: %v", err)
	}
	if overrideCount != 1 {
		t.Errorf("expected custom mapping override for 101 to be preserved, got count %d", overrideCount)
	}

	// Verify watching count is 0
	var watchingCount int
	if err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM synced_media WHERE anilist_status = 'CURRENT'").Scan(&watchingCount)
	}); err != nil {
		t.Fatalf("query watching count: %v", err)
	}
	if watchingCount != 0 {
		t.Errorf("expected WatchingCount 0, got %d", watchingCount)
	}
}

func TestSync_DroppedAndCompleted_Cleanup_SonarrAndRadarr(t *testing.T) {
	t.Run("Sonarr split cour completed unmonitor and file deletion in order", func(t *testing.T) {
		db := setupTestDB(t)

		// Seed fribb entry for anime ID 500
		_ = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO fribb_entries (anilist_id, tvdb_id, updated_at)
				VALUES (500, 70001, CURRENT_TIMESTAMP);
			`)
			return err
		})

		var callLog []string
		var deletedFiles []int
		var unmonitoredEpisodes []int

		alSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"MediaListCollection": map[string]any{
						"hasNextChunk": false,
						"lists": []map[string]any{
							{
								"name":   "Completed",
								"status": "COMPLETED",
								"entries": []map[string]any{
									{
										"id":       1,
										"mediaId":  500,
										"status":   "COMPLETED",
										"progress": 12,
										"media": map[string]any{
											"id":     500,
											"format": "TV",
											"status": "FINISHED",
											"title": map[string]any{
												"romaji": "Attack on Titan Season 3 Part 2",
											},
										},
									},
								},
							},
						},
					},
				},
			})
		}))
		defer alSrv.Close()

		sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
				_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 10, Label: "anilist-sync"}})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
				_ = json.NewEncoder(w).Encode([]servarr.Series{
					{
						ID:        1,
						TVDBID:    70001,
						Title:     "Attack on Titan",
						Monitored: true,
						Tags:      []int{10}, // Managed tag
						Seasons: []servarr.Season{
							{SeasonNumber: 3, Monitored: true},
						},
					},
				})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
				// Season 3 episodes: Part 1 = ep 1..12, Part 2 = ep 13..22
				eps := make([]servarr.Episode, 22)
				for i := 1; i <= 22; i++ {
					isPart2 := i > 12
					fileID := 0
					if isPart2 {
						fileID = 2000 + i
					} else {
						fileID = 1000 + i
					}
					finale := ""
					if i == 12 {
						finale = "midseason"
					}
					eps[i-1] = servarr.Episode{
						ID:            i,
						SeriesID:      1,
						SeasonNumber:  3,
						EpisodeNumber: i,
						Monitored:     true,
						HasFile:       true,
						EpisodeFileID: fileID,
						FinaleType:    finale,
					}
				}
				_ = json.NewEncoder(w).Encode(eps)
			case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				callLog = append(callLog, "unmonitor")
				if body["monitored"] != false {
					t.Errorf("expected monitored=false, got %v", body["monitored"])
				}
				if epIDs, ok := body["episodeIds"].([]any); ok {
					for _, id := range epIDs {
						unmonitoredEpisodes = append(unmonitoredEpisodes, int(id.(float64)))
					}
				}
				_ = json.NewEncoder(w).Encode([]any{})
			case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v3/episodefile/"):
				callLog = append(callLog, "delete_file")
				idStr := strings.TrimPrefix(r.URL.Path, "/api/v3/episodefile/")
				var id int
				for _, c := range idStr {
					id = id*10 + int(c-'0')
				}
				deletedFiles = append(deletedFiles, id)
				w.WriteHeader(http.StatusOK)
			default:
				http.NotFound(w, r)
			}
		}))
		defer sonarrSrv.Close()

		cfg := config.NewDefault()
		cfg.AniListUsername = "cleanupuser"
		cfg.TagName = "anilist-sync"
		cfg.UnmonitorCompleted = true
		cfg.DeleteCompleted = true
		cfg.FirstRunDryRun = false

		alClient := anilist.NewClient(alSrv.URL)
		sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
		orc := orchestrator.New(db, cfg, alClient, sonarrClient, nil)

		report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if report.Status == orchestrator.SyncStatusFailed {
			t.Fatalf("Sync reported failed: %v", report.Errors)
		}

		// Verify unmonitoring ran BEFORE file deletion
		if len(callLog) < 2 {
			t.Fatalf("expected at least 2 calls in callLog, got %v", callLog)
		}
		if callLog[0] != "unmonitor" {
			t.Fatalf("expected unmonitor to be called first, got %s", callLog[0])
		}

		// Verify only Part 2 (episodes 13-22) were unmonitored and deleted
		if len(unmonitoredEpisodes) != 10 {
			t.Fatalf("expected 10 episodes in Part 2 unmonitored, got %d (%v)", len(unmonitoredEpisodes), unmonitoredEpisodes)
		}
		for _, epID := range unmonitoredEpisodes {
			if epID < 13 || epID > 22 {
				t.Errorf("expected only part 2 episodes (13-22) unmonitored, got episode %d", epID)
			}
		}

		if len(deletedFiles) != 10 {
			t.Fatalf("expected 10 files in Part 2 deleted, got %d (%v)", len(deletedFiles), deletedFiles)
		}
		for _, fileID := range deletedFiles {
			if fileID < 2013 || fileID > 2022 {
				t.Errorf("expected only part 2 files (2013-2022) deleted, got file %d", fileID)
			}
		}
	})

	t.Run("Radarr dropped unmonitor and file deletion with managed tag", func(t *testing.T) {
		db := setupTestDB(t)

		_ = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO fribb_entries (anilist_id, tmdb_id, updated_at)
				VALUES (600, 80001, CURRENT_TIMESTAMP);
			`)
			return err
		})

		var callLog []string
		deletedFileID := 0

		alSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"MediaListCollection": map[string]any{
						"hasNextChunk": false,
						"lists": []map[string]any{
							{
								"name":   "Dropped",
								"status": "DROPPED",
								"entries": []map[string]any{
									{
										"id":       1,
										"mediaId":  600,
										"status":   "DROPPED",
										"progress": 0,
										"media": map[string]any{
											"id":     600,
											"format": "MOVIE",
											"status": "FINISHED",
											"title": map[string]any{
												"romaji": "Dropped Movie",
											},
										},
									},
								},
							},
						},
					},
				},
			})
		}))
		defer alSrv.Close()

		radarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
				_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 5, Label: "anilist-sync"}})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
				_ = json.NewEncoder(w).Encode([]servarr.Movie{{
					ID:          1,
					TMDBID:      80001,
					Title:       "Dropped Movie",
					Monitored:   true,
					Tags:        []int{5}, // Managed tag
					HasFile:     true,
					MovieFileID: 9999,
				}})
			case r.Method == http.MethodPut && r.URL.Path == "/api/v3/movie":
				callLog = append(callLog, "unmonitor_movie")
				_ = json.NewEncoder(w).Encode(servarr.Movie{ID: 1, Monitored: false})
			case r.Method == http.MethodDelete && r.URL.Path == "/api/v3/moviefile/9999":
				callLog = append(callLog, "delete_movie_file")
				deletedFileID = 9999
				w.WriteHeader(http.StatusOK)
			default:
				http.NotFound(w, r)
			}
		}))
		defer radarrSrv.Close()

		cfg := config.NewDefault()
		cfg.AniListUsername = "cleanupuser"
		cfg.TagName = "anilist-sync"
		cfg.UnmonitorDropped = true
		cfg.DeleteDropped = true
		cfg.FirstRunDryRun = false

		alClient := anilist.NewClient(alSrv.URL)
		radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")
		orc := orchestrator.New(db, cfg, alClient, nil, radarrClient)

		report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if report.Status == orchestrator.SyncStatusFailed {
			t.Fatalf("Sync reported failed: %v", report.Errors)
		}

		if len(callLog) != 2 || callLog[0] != "unmonitor_movie" || callLog[1] != "delete_movie_file" {
			t.Fatalf("expected [unmonitor_movie, delete_movie_file], got %v", callLog)
		}
		if deletedFileID != 9999 {
			t.Fatalf("expected deletedFileID 9999, got %d", deletedFileID)
		}
	})

	t.Run("Dry run stages ActionDeleteFile without modifying services", func(t *testing.T) {
		db := setupTestDB(t)

		_ = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO fribb_entries (anilist_id, tmdb_id, updated_at)
				VALUES (700, 90001, CURRENT_TIMESTAMP);
			`)
			return err
		})

		var callsMade []string

		alSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"MediaListCollection": map[string]any{
						"hasNextChunk": false,
						"lists": []map[string]any{
							{
								"name":   "Completed",
								"status": "COMPLETED",
								"entries": []map[string]any{
									{
										"id":      1,
										"mediaId": 700,
										"status":  "COMPLETED",
										"media": map[string]any{
											"id":     700,
											"format": "MOVIE",
											"status": "FINISHED",
											"title": map[string]any{
												"romaji": "Dry Run Movie",
											},
										},
									},
								},
							},
						},
					},
				},
			})
		}))
		defer alSrv.Close()

		radarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
				_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 5, Label: "anilist-sync"}})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
				_ = json.NewEncoder(w).Encode([]servarr.Movie{{
					ID:          10,
					TMDBID:      90001,
					Title:       "Dry Run Movie",
					Monitored:   true,
					Tags:        []int{5},
					HasFile:     true,
					MovieFileID: 5555,
				}})
			default:
				callsMade = append(callsMade, r.Method+" "+r.URL.Path)
				http.NotFound(w, r)
			}
		}))
		defer radarrSrv.Close()

		cfg := config.NewDefault()
		cfg.AniListUsername = "dryrunuser"
		cfg.TagName = "anilist-sync"
		cfg.UnmonitorCompleted = true
		cfg.DeleteCompleted = true
		cfg.FirstRunDryRun = true // Dry run active!

		alClient := anilist.NewClient(alSrv.URL)
		radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")
		orc := orchestrator.New(db, cfg, alClient, nil, radarrClient)

		report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if report.Status == orchestrator.SyncStatusFailed {
			t.Fatalf("Sync reported failed: %v", report.Errors)
		}

		if len(callsMade) > 0 {
			t.Fatalf("expected zero mutating calls in dry run, got %v", callsMade)
		}

		// Verify staged actions: UNMONITOR_MOVIE and DELETE_FILE
		staged, err := orc.GetStagedActions(context.Background(), orchestrator.StagedStatusPending)
		if err != nil {
			t.Fatalf("GetStagedActions failed: %v", err)
		}
		actionTypes := make(map[orchestrator.StagedActionType]bool)
		for _, a := range staged {
			actionTypes[a.ActionType] = true
		}
		if !actionTypes[orchestrator.ActionUnmonitorMovie] {
			t.Errorf("expected ActionUnmonitorMovie in staged actions, got %v", staged)
		}
		if !actionTypes[orchestrator.ActionDeleteFile] {
			t.Errorf("expected ActionDeleteFile in staged actions, got %v", staged)
		}
	})
}

func TestSync_WatchedEpisodeUnmonitoringAndPruning(t *testing.T) {
	t.Run("Split cour maps progress 5 in cour 2 to episodes 13-17 and deletes files in order", func(t *testing.T) {
		db := setupTestDB(t)

		_ = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO fribb_entries (anilist_id, tvdb_id, updated_at)
				VALUES (800, 60001, CURRENT_TIMESTAMP);
			`)
			return err
		})

		var callLog []string
		var unmonitoredEpIDs []int
		var deletedFileIDs []int

		alSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"MediaListCollection": map[string]any{
						"hasNextChunk": false,
						"lists": []map[string]any{
							{
								"name":   "Watching",
								"status": "CURRENT",
								"entries": []map[string]any{
									{
										"id":       1,
										"mediaId":  800,
										"status":   "CURRENT",
										"progress": 5, // Watched 5 episodes of cour 2
										"media": map[string]any{
											"id":     800,
											"format": "TV",
											"status": "RELEASING",
											"title": map[string]any{
												"romaji": "Re:Zero Season 2 Part 2",
											},
										},
									},
								},
							},
						},
					},
				},
			})
		}))
		defer alSrv.Close()

		sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
				_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 15, Label: "anilist-sync"}})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
				_ = json.NewEncoder(w).Encode([]servarr.Series{
					{
						ID:        1,
						TVDBID:    60001,
						Title:     "Re:Zero",
						Monitored: true,
						Tags:      []int{15}, // Managed tag
						Seasons: []servarr.Season{
							{SeasonNumber: 2, Monitored: true},
						},
					},
				})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
				// Season 2: cour 1 = ep 1..12, cour 2 = ep 13..25
				eps := make([]servarr.Episode, 25)
				for i := 1; i <= 25; i++ {
					finale := ""
					if i == 12 {
						finale = "midseason"
					}
					eps[i-1] = servarr.Episode{
						ID:            i,
						SeriesID:      1,
						SeasonNumber:  2,
						EpisodeNumber: i,
						Monitored:     true,
						HasFile:       true,
						EpisodeFileID: 3000 + i,
						FinaleType:    finale,
					}
				}
				_ = json.NewEncoder(w).Encode(eps)
			case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				callLog = append(callLog, "unmonitor")
				if body["monitored"] != false {
					t.Errorf("expected monitored=false, got %v", body["monitored"])
				}
				if epIDs, ok := body["episodeIds"].([]any); ok {
					for _, id := range epIDs {
						unmonitoredEpIDs = append(unmonitoredEpIDs, int(id.(float64)))
					}
				}
				_ = json.NewEncoder(w).Encode([]any{})
			case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v3/episodefile/"):
				callLog = append(callLog, "delete_file")
				idStr := strings.TrimPrefix(r.URL.Path, "/api/v3/episodefile/")
				var id int
				for _, c := range idStr {
					id = id*10 + int(c-'0')
				}
				deletedFileIDs = append(deletedFileIDs, id)
				w.WriteHeader(http.StatusOK)
			default:
				http.NotFound(w, r)
			}
		}))
		defer sonarrSrv.Close()

		cfg := config.NewDefault()
		cfg.AniListUsername = "pruneuser"
		cfg.TagName = "anilist-sync"
		cfg.UnmonitorWatched = true
		cfg.DeleteWatched = true
		cfg.FirstRunDryRun = false

		alClient := anilist.NewClient(alSrv.URL)
		sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
		orc := orchestrator.New(db, cfg, alClient, sonarrClient, nil)

		report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if report.Status == orchestrator.SyncStatusFailed {
			t.Fatalf("Sync reported failed: %v", report.Errors)
		}

		if len(callLog) < 2 || callLog[0] != "unmonitor" {
			t.Fatalf("expected unmonitor to be called before delete in callLog, got %v", callLog)
		}

		// Progress 5 in cour 2 (eps 13..25) must resolve strictly to episodes 13 through 17
		expectedEps := []int{13, 14, 15, 16, 17}
		if len(unmonitoredEpIDs) != 5 {
			t.Fatalf("expected 5 unmonitored episodes, got %d (%v)", len(unmonitoredEpIDs), unmonitoredEpIDs)
		}
		for i, id := range unmonitoredEpIDs {
			if id != expectedEps[i] {
				t.Errorf("expected ep %d at index %d, got %d", expectedEps[i], i, id)
			}
		}

		expectedFiles := []int{3013, 3014, 3015, 3016, 3017}
		if len(deletedFileIDs) != 5 {
			t.Fatalf("expected 5 deleted files, got %d (%v)", len(deletedFileIDs), deletedFileIDs)
		}
		for i, id := range deletedFileIDs {
			if id != expectedFiles[i] {
				t.Errorf("expected file %d at index %d, got %d", expectedFiles[i], i, id)
			}
		}
	})

	t.Run("Dry run stages ActionUnmonitorEpisode and ActionDeleteEpisodeFile", func(t *testing.T) {
		db := setupTestDB(t)

		_ = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO fribb_entries (anilist_id, tvdb_id, updated_at)
				VALUES (801, 60002, CURRENT_TIMESTAMP);
			`)
			return err
		})

		var mutatingCalls []string

		alSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"MediaListCollection": map[string]any{
						"hasNextChunk": false,
						"lists": []map[string]any{
							{
								"name":   "Watching",
								"status": "CURRENT",
								"entries": []map[string]any{
									{
										"id":       1,
										"mediaId":  801,
										"status":   "CURRENT",
										"progress": 3,
										"media": map[string]any{
											"id":     801,
											"format": "TV",
											"status": "RELEASING",
											"title": map[string]any{
												"romaji": "Single Season Anime",
											},
										},
									},
								},
							},
						},
					},
				},
			})
		}))
		defer alSrv.Close()

		sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
				_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 15, Label: "anilist-sync"}})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
				_ = json.NewEncoder(w).Encode([]servarr.Series{
					{
						ID:        2,
						TVDBID:    60002,
						Title:     "Single Season Anime",
						Monitored: true,
						Tags:      []int{15},
						Seasons: []servarr.Season{
							{SeasonNumber: 1, Monitored: true},
						},
					},
				})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
				eps := []servarr.Episode{
					{ID: 1, SeriesID: 2, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true, HasFile: true, EpisodeFileID: 501},
					{ID: 2, SeriesID: 2, SeasonNumber: 1, EpisodeNumber: 2, Monitored: true, HasFile: true, EpisodeFileID: 502},
					{ID: 3, SeriesID: 2, SeasonNumber: 1, EpisodeNumber: 3, Monitored: true, HasFile: true, EpisodeFileID: 503},
					{ID: 4, SeriesID: 2, SeasonNumber: 1, EpisodeNumber: 4, Monitored: true, HasFile: false, EpisodeFileID: 0},
				}
				_ = json.NewEncoder(w).Encode(eps)
			default:
				mutatingCalls = append(mutatingCalls, r.Method+" "+r.URL.Path)
				http.NotFound(w, r)
			}
		}))
		defer sonarrSrv.Close()

		cfg := config.NewDefault()
		cfg.AniListUsername = "pruneuser"
		cfg.TagName = "anilist-sync"
		cfg.UnmonitorWatched = true
		cfg.DeleteWatched = true
		cfg.FirstRunDryRun = true // Dry run active!

		alClient := anilist.NewClient(alSrv.URL)
		sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
		orc := orchestrator.New(db, cfg, alClient, sonarrClient, nil)

		report, err := orc.Sync(context.Background(), orchestrator.TriggerManualWeb)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if report.Status == orchestrator.SyncStatusFailed {
			t.Fatalf("Sync reported failed: %v", report.Errors)
		}

		if len(mutatingCalls) > 0 {
			t.Fatalf("expected 0 mutating calls during dry run, got %v", mutatingCalls)
		}

		staged, err := orc.GetStagedActions(context.Background(), orchestrator.StagedStatusPending)
		if err != nil {
			t.Fatalf("GetStagedActions failed: %v", err)
		}

		actionMap := make(map[orchestrator.StagedActionType]orchestrator.StagedAction)
		for _, a := range staged {
			actionMap[a.ActionType] = a
		}

		if _, ok := actionMap[orchestrator.ActionUnmonitorEpisode]; !ok {
			t.Errorf("expected ActionUnmonitorEpisode staged, got actions %v", staged)
		}
		if _, ok := actionMap[orchestrator.ActionDeleteEpisodeFile]; !ok {
			t.Errorf("expected ActionDeleteEpisodeFile staged, got actions %v", staged)
		}

		// Now test ApplyStagedAction executes them cleanly against Sonarr
		var appliedUnmonitor bool
		var appliedDeleteFiles []int
		applySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
				_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 15, Label: "anilist-sync"}})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/2":
				_ = json.NewEncoder(w).Encode(servarr.Series{ID: 2, TVDBID: 60002, Tags: []int{15}})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
				_ = json.NewEncoder(w).Encode([]servarr.Episode{
					{ID: 1, SeriesID: 2, SeasonNumber: 1, EpisodeNumber: 1, Monitored: !appliedUnmonitor, HasFile: true, EpisodeFileID: 501},
					{ID: 2, SeriesID: 2, SeasonNumber: 1, EpisodeNumber: 2, Monitored: !appliedUnmonitor, HasFile: true, EpisodeFileID: 502},
					{ID: 3, SeriesID: 2, SeasonNumber: 1, EpisodeNumber: 3, Monitored: !appliedUnmonitor, HasFile: true, EpisodeFileID: 503},
				})
			case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode unmonitor request: %v", err)
				}
				if body["monitored"] != false {
					t.Errorf("expected monitored=false, got %v", body["monitored"])
				}
				appliedUnmonitor = true
				_ = json.NewEncoder(w).Encode([]any{})
			case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v3/episodefile/"):
				idStr := strings.TrimPrefix(r.URL.Path, "/api/v3/episodefile/")
				var id int
				for _, c := range idStr {
					id = id*10 + int(c-'0')
				}
				appliedDeleteFiles = append(appliedDeleteFiles, id)
				w.WriteHeader(http.StatusOK)
			default:
				http.NotFound(w, r)
			}
		}))
		defer applySrv.Close()

		applySonarr, _ := servarr.NewSonarrClient(applySrv.URL, "key")
		applyOrc := orchestrator.New(db, cfg, alClient, applySonarr, nil)

		unmonAction := actionMap[orchestrator.ActionUnmonitorEpisode]
		if err := applyOrc.ApplyStagedAction(context.Background(), unmonAction.ID); err != nil {
			t.Fatalf("ApplyStagedAction for unmonitor failed: %v", err)
		}
		if !appliedUnmonitor {
			t.Errorf("expected appliedUnmonitor to be true")
		}

		delAction := actionMap[orchestrator.ActionDeleteEpisodeFile]
		if err := applyOrc.ApplyStagedAction(context.Background(), delAction.ID); err != nil {
			t.Fatalf("ApplyStagedAction for delete file failed: %v", err)
		}
		slices.Sort(appliedDeleteFiles)
		if !slices.Equal(appliedDeleteFiles, []int{501, 502, 503}) {
			t.Errorf("expected files 501, 502, 503 deleted exactly once, got %v", appliedDeleteFiles)
		}
	})
}

package orchestrator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie/lookup/tmdb":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie/lookup":
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie/lookup/tmdb":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie/lookup":
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie/lookup/tmdb":
			if r.URL.Query().Get("tmdbId") == "20202" {
				_ = json.NewEncoder(w).Encode(servarr.Movie{
					ID:        992,
					TMDBID:    20202,
					Title:     "Dropped Movie Managed",
					Monitored: true,
					Tags:      []int{42}, // Tagged!
				})
				return
			}
			if r.URL.Query().Get("tmdbId") == "30303" {
				_ = json.NewEncoder(w).Encode(servarr.Movie{
					ID:        993,
					TMDBID:    30303,
					Title:     "Dropped Movie Untagged",
					Monitored: true,
					Tags:      []int{}, // Untagged!
				})
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie/lookup/tmdb":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie/lookup":
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie/lookup/tmdb":
			// Movie has tag 0
			_ = json.NewEncoder(w).Encode(servarr.Movie{
				ID: 995, TMDBID: 20203, Title: "Movie Untagged Zero", Monitored: true,
				Tags: []int{0},
			})
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

package orchestrator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

func TestSync_QualityProfileOverrides_DirectAdd(t *testing.T) {
	db := setupTestDB(t)
	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, stmt := range []string{
			`INSERT INTO fribb_entries (anilist_id, tvdb_id, media_type, tvdb_season) VALUES (101, 1001, 'TV', 1)`,
			`INSERT INTO fribb_entries (anilist_id, tvdb_id, media_type, tvdb_season) VALUES (102, 1002, 'TV', 1)`,
			`INSERT INTO fribb_entries (anilist_id, tvdb_id, media_type, tvdb_season) VALUES (103, 1003, 'TV', 1)`,
			`INSERT INTO fribb_entries (anilist_id, tmdb_id, media_type) VALUES (201, 2001, 'MOVIE')`,
			`INSERT INTO fribb_entries (anilist_id, tmdb_id, media_type) VALUES (202, 2002, 'MOVIE')`,
		} {
			if _, execErr := tx.ExecContext(ctx, stmt); execErr != nil {
				return execErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	entries := []anilist.MediaListEntry{
		{ID: 1, Status: anilist.StatusCurrent, Media: anilist.Media{ID: 101, Format: "ONA", Status: anilist.MediaStatusFinished, CountryOfOrigin: "CN", Title: anilist.MediaTitle{Romaji: "Link Click"}}},
		{ID: 2, Status: anilist.StatusCurrent, Media: anilist.Media{ID: 102, Format: "TV", Status: anilist.MediaStatusFinished, CountryOfOrigin: "JP", Title: anilist.MediaTitle{Romaji: "Frieren"}}},
		{ID: 3, Status: anilist.StatusCurrent, Media: anilist.Media{ID: 103, Format: "TV", Status: anilist.MediaStatusFinished, CountryOfOrigin: "", Title: anilist.MediaTitle{Romaji: "No Country"}}},
		{ID: 4, Status: anilist.StatusCurrent, Media: anilist.Media{ID: 201, Format: "MOVIE", Status: anilist.MediaStatusFinished, CountryOfOrigin: "CN", Title: anilist.MediaTitle{Romaji: "Ne Zha"}}},
		{ID: 5, Status: anilist.StatusCurrent, Media: anilist.Media{ID: 202, Format: "MOVIE", Status: anilist.MediaStatusFinished, CountryOfOrigin: "KR", Title: anilist.MediaTitle{Romaji: "Seoul Station"}}},
	}
	alSrv := setupMockAniList(t, entries)

	var mu sync.Mutex
	sonarrAdded := make(map[int]servarr.AddSeriesRequest)
	radarrAdded := make(map[int]servarr.AddMovieRequest)

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/series":
			var req servarr.AddSeriesRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			sonarrAdded[req.TVDBID] = req
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(servarr.Series{ID: req.TVDBID, TVDBID: req.TVDBID, Title: req.Title, QualityProfileID: req.QualityProfileID, Monitored: req.Monitored, Seasons: req.Seasons})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	radarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			_ = json.NewEncoder(w).Encode([]servarr.Movie{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/movie":
			var req servarr.AddMovieRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			radarrAdded[req.TMDBID] = req
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(servarr.Movie{ID: req.TMDBID, TMDBID: req.TMDBID, Title: req.Title, QualityProfileID: req.QualityProfileID, Monitored: req.Monitored})
		default:
			http.NotFound(w, r)
		}
	}))
	defer radarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "tester"
	cfg.FirstRunDryRun = false
	cfg.SonarrQualityProfileID = 1
	cfg.SonarrRootFolderPath = "/tv"
	cfg.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"CN": 4}
	cfg.RadarrQualityProfileID = 2
	cfg.RadarrRootFolderPath = "/movies"
	cfg.RadarrQualityProfileOverrides = config.CountryQualityProfiles{"CN": 6}

	alClient := anilist.NewClient(alSrv.URL, anilist.WithRateLimit(60000))
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)
	report, err := orc.Sync(context.Background(), orchestrator.TriggerCLI)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if len(report.Errors) > 0 {
		t.Fatalf("unexpected sync errors: %v", report.Errors)
	}

	if got := sonarrAdded[1001].QualityProfileID; got != 4 {
		t.Errorf("CN series QualityProfileID = %d, want 4", got)
	}
	if got := sonarrAdded[1002].QualityProfileID; got != 1 {
		t.Errorf("JP series QualityProfileID = %d, want default 1", got)
	}
	if got := sonarrAdded[1003].QualityProfileID; got != 1 {
		t.Errorf("empty-country series QualityProfileID = %d, want default 1", got)
	}
	if got := radarrAdded[2001].QualityProfileID; got != 6 {
		t.Errorf("CN movie QualityProfileID = %d, want 6", got)
	}
	if got := radarrAdded[2002].QualityProfileID; got != 2 {
		t.Errorf("KR movie QualityProfileID = %d, want default 2", got)
	}
}

func TestSync_QualityProfileOverrides_StagedDryRunAndApply(t *testing.T) {
	db := setupTestDB(t)
	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, stmt := range []string{
			`INSERT INTO fribb_entries (anilist_id, tvdb_id, media_type, tvdb_season) VALUES (301, 3001, 'TV', 1)`,
			`INSERT INTO fribb_entries (anilist_id, tmdb_id, media_type) VALUES (302, 3002, 'MOVIE')`,
		} {
			if _, execErr := tx.ExecContext(ctx, stmt); execErr != nil {
				return execErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	entries := []anilist.MediaListEntry{
		{ID: 1, Status: anilist.StatusCurrent, Media: anilist.Media{ID: 301, Format: "ONA", Status: anilist.MediaStatusFinished, CountryOfOrigin: "CN", Title: anilist.MediaTitle{Romaji: "Link Click"}}},
		{ID: 2, Status: anilist.StatusCurrent, Media: anilist.Media{ID: 302, Format: "MOVIE", Status: anilist.MediaStatusFinished, CountryOfOrigin: "KR", Title: anilist.MediaTitle{Romaji: "Korean Film"}}},
	}
	alSrv := setupMockAniList(t, entries)

	var gotSeriesReq servarr.AddSeriesRequest
	var gotMovieReq servarr.AddMovieRequest

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/series":
			_ = json.NewDecoder(r.Body).Decode(&gotSeriesReq)
			_ = json.NewEncoder(w).Encode(servarr.Series{ID: 10, TVDBID: gotSeriesReq.TVDBID, QualityProfileID: gotSeriesReq.QualityProfileID})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	radarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			_ = json.NewEncoder(w).Encode([]servarr.Movie{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/movie":
			_ = json.NewDecoder(r.Body).Decode(&gotMovieReq)
			_ = json.NewEncoder(w).Encode(servarr.Movie{ID: 20, TMDBID: gotMovieReq.TMDBID, QualityProfileID: gotMovieReq.QualityProfileID})
		default:
			http.NotFound(w, r)
		}
	}))
	defer radarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "tester"
	cfg.FirstRunDryRun = true
	cfg.SonarrQualityProfileID = 1
	cfg.SonarrRootFolderPath = "/tv"
	cfg.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"CN": 4}
	cfg.RadarrQualityProfileID = 2
	cfg.RadarrRootFolderPath = "/movies"
	cfg.RadarrQualityProfileOverrides = config.CountryQualityProfiles{"KR": 7}

	alClient := anilist.NewClient(alSrv.URL, anilist.WithRateLimit(60000))
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)
	report, err := orc.Sync(context.Background(), orchestrator.TriggerCLI)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if len(report.Errors) > 0 {
		t.Fatalf("unexpected sync errors: %v", report.Errors)
	}

	actions, err := orc.GetStagedActions(context.Background(), orchestrator.StagedStatusPending)
	if err != nil {
		t.Fatalf("GetStagedActions failed: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("expected 2 staged actions, got %d", len(actions))
	}

	var ids []int
	for _, a := range actions {
		ids = append(ids, a.ID)
	}
	if _, err := orc.ApplyStagedActions(context.Background(), ids); err != nil {
		t.Fatalf("ApplyStagedActions failed: %v", err)
	}

	if gotSeriesReq.QualityProfileID != 4 {
		t.Errorf("staged ADD_SERIES applied QualityProfileID = %d, want 4", gotSeriesReq.QualityProfileID)
	}
	if gotMovieReq.QualityProfileID != 7 {
		t.Errorf("staged ADD_MOVIE applied QualityProfileID = %d, want 7", gotMovieReq.QualityProfileID)
	}
}

func TestPrepareReview_QualityProfileOverrides(t *testing.T) {
	db := setupTestDB(t)

	// Sync diverts an unmapped CN series into review_queue (recording country_of_origin = 'CN').
	entries := []anilist.MediaListEntry{
		{ID: 1, Status: anilist.StatusCurrent, Media: anilist.Media{ID: 501, Format: "ONA", Status: anilist.MediaStatusFinished, CountryOfOrigin: "CN", Title: anilist.MediaTitle{Romaji: "Unmapped Donghua"}}},
		{ID: 2, Status: anilist.StatusCurrent, Media: anilist.Media{ID: 502, Format: "TV", Status: anilist.MediaStatusFinished, CountryOfOrigin: "JP", Title: anilist.MediaTitle{Romaji: "Unmapped Anime"}}},
	}
	alSrv := setupMockAniList(t, entries)

	var mu sync.Mutex
	addedByTVDB := make(map[int]servarr.AddSeriesRequest)

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/tag":
			var tag servarr.Tag
			_ = json.NewDecoder(r.Body).Decode(&tag)
			tag.ID = 99
			_ = json.NewEncoder(w).Encode(tag)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{
				{ID: 0, TVDBID: 5001, Title: "Looked Up CN", Seasons: []servarr.Season{{SeasonNumber: 1}}},
				{ID: 0, TVDBID: 5002, Title: "Looked Up JP", Seasons: []servarr.Season{{SeasonNumber: 1}}},
				{ID: 0, TVDBID: 5003, Title: "Looked Up KR", Seasons: []servarr.Season{{SeasonNumber: 1}}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/series":
			var req servarr.AddSeriesRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			addedByTVDB[req.TVDBID] = req
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(servarr.Series{ID: req.TVDBID, TVDBID: req.TVDBID, Title: req.Title, QualityProfileID: req.QualityProfileID, Tags: req.Tags, Seasons: req.Seasons})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/5001":
			_ = json.NewEncoder(w).Encode(servarr.Series{ID: 5001, TVDBID: 5001, Title: "Looked Up", QualityProfileID: 4, Seasons: []servarr.Season{{SeasonNumber: 1}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/5002":
			_ = json.NewEncoder(w).Encode(servarr.Series{ID: 5002, TVDBID: 5002, Title: "Looked Up", QualityProfileID: 1, Seasons: []servarr.Season{{SeasonNumber: 1}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/5003":
			_ = json.NewEncoder(w).Encode(servarr.Series{ID: 5003, TVDBID: 5003, Title: "Looked Up", QualityProfileID: 5, Seasons: []servarr.Season{{SeasonNumber: 1}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			_ = json.NewEncoder(w).Encode([]servarr.Episode{{ID: 1, SeasonNumber: 1, EpisodeNumber: 1}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/command":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 10, "name": "RefreshSeries", "status": "completed", "body": map[string]any{"isNewSeries": true, "seriesIds": []int{5001, 5002, 5003}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "tester"
	cfg.FirstRunDryRun = false
	cfg.SonarrQualityProfileID = 1
	cfg.SonarrRootFolderPath = "/tv"
	cfg.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"CN": 4, "KR": 5}

	alClient := anilist.NewClient(alSrv.URL, anilist.WithRateLimit(60000))
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, nil)
	if _, err := orc.Sync(context.Background(), orchestrator.TriggerCLI); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Also insert a legacy review_queue row without country_of_origin, backed by synced_media with KR.
	var legacyReviewID int
	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO synced_media (anilist_id, media_type, title_romaji, anilist_status, target_service, monitored_status, country_of_origin)
			VALUES (503, 'SERIES', 'Korean Legacy', 'CURRENT', 'NONE', 'PENDING_REVIEW', 'KR')
		`); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO review_queue (anilist_id, media_type, title_romaji, candidates_json, reason, status)
			VALUES (503, 'SERIES', 'Korean Legacy', '[]', 'UNMAPPED', 'PENDING')
		`)
		if err != nil {
			return err
		}
		id64, _ := res.LastInsertId()
		legacyReviewID = int(id64)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var cnReviewID, jpReviewID int
	if err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM review_queue WHERE anilist_id = 501`).Scan(&cnReviewID); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT id FROM review_queue WHERE anilist_id = 502`).Scan(&jpReviewID)
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := orc.PrepareReview(context.Background(), cnReviewID, 5001); err != nil {
		t.Fatalf("PrepareReview(CN) failed: %v", err)
	}
	if _, err := orc.PrepareReview(context.Background(), jpReviewID, 5002); err != nil {
		t.Fatalf("PrepareReview(JP) failed: %v", err)
	}
	if _, err := orc.PrepareReview(context.Background(), legacyReviewID, 5003); err != nil {
		t.Fatalf("PrepareReview(KR fallback) failed: %v", err)
	}

	if got := addedByTVDB[5001].QualityProfileID; got != 4 {
		t.Errorf("PrepareReview(CN) QualityProfileID = %d, want 4", got)
	}
	if got := addedByTVDB[5002].QualityProfileID; got != 1 {
		t.Errorf("PrepareReview(JP) QualityProfileID = %d, want default 1", got)
	}
	if got := addedByTVDB[5003].QualityProfileID; got != 5 {
		t.Errorf("PrepareReview(KR via synced_media) QualityProfileID = %d, want 5", got)
	}
}

func TestSync_QualityProfileOverrides_PreservesExistingMediaProfiles(t *testing.T) {
	db := setupTestDB(t)
	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, stmt := range []string{
			`INSERT INTO fribb_entries (anilist_id, tvdb_id, media_type, tvdb_season) VALUES (601, 6001, 'TV', 1)`,
			`INSERT INTO fribb_entries (anilist_id, tmdb_id, media_type) VALUES (602, 6002, 'MOVIE')`,
		} {
			if _, execErr := tx.ExecContext(ctx, stmt); execErr != nil {
				return execErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	entries := []anilist.MediaListEntry{
		{ID: 1, Status: anilist.StatusCurrent, Media: anilist.Media{ID: 601, Format: "ONA", Status: anilist.MediaStatusFinished, CountryOfOrigin: "CN", Title: anilist.MediaTitle{Romaji: "Existing Donghua"}}},
		{ID: 2, Status: anilist.StatusDropped, Media: anilist.Media{ID: 602, Format: "MOVIE", Status: anilist.MediaStatusFinished, CountryOfOrigin: "CN", Title: anilist.MediaTitle{Romaji: "Existing CN Movie"}}},
	}
	alSrv := setupMockAniList(t, entries)

	var updatedSeries servarr.Series
	var updatedMovie servarr.Movie
	seriesUpdated := false
	movieUpdated := false

	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{{
				ID:               61,
				TVDBID:           6001,
				Title:            "Existing Donghua",
				QualityProfileID: 9,
				Monitored:        true,
				Tags:             []int{1},
				Seasons:          []servarr.Season{{SeasonNumber: 1, Monitored: false}},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			_ = json.NewEncoder(w).Encode([]servarr.Episode{{ID: 1, SeriesID: 61, SeasonNumber: 1, EpisodeNumber: 1}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/series":
			_ = json.NewDecoder(r.Body).Decode(&updatedSeries)
			seriesUpdated = true
			_ = json.NewEncoder(w).Encode(updatedSeries)
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarrSrv.Close()

	radarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			_ = json.NewEncoder(w).Encode([]servarr.Movie{{
				ID:               62,
				TMDBID:           6002,
				Title:            "Existing CN Movie",
				QualityProfileID: 8,
				Monitored:        true,
				Tags:             []int{1},
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/movie":
			_ = json.NewDecoder(r.Body).Decode(&updatedMovie)
			movieUpdated = true
			_ = json.NewEncoder(w).Encode(updatedMovie)
		default:
			http.NotFound(w, r)
		}
	}))
	defer radarrSrv.Close()

	cfg := config.NewDefault()
	cfg.AniListUsername = "tester"
	cfg.FirstRunDryRun = false
	cfg.UnmonitorDropped = true
	cfg.SonarrQualityProfileID = 1
	cfg.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"CN": 4}
	cfg.RadarrQualityProfileID = 2
	cfg.RadarrQualityProfileOverrides = config.CountryQualityProfiles{"CN": 6}

	alClient := anilist.NewClient(alSrv.URL, anilist.WithRateLimit(60000))
	sonarrClient, _ := servarr.NewSonarrClient(sonarrSrv.URL, "key")
	radarrClient, _ := servarr.NewRadarrClient(radarrSrv.URL, "key")

	orc := orchestrator.New(db, cfg, alClient, sonarrClient, radarrClient)
	report, err := orc.Sync(context.Background(), orchestrator.TriggerCLI)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if len(report.Errors) > 0 {
		t.Fatalf("unexpected sync errors: %v", report.Errors)
	}

	if !seriesUpdated {
		t.Fatal("expected existing Sonarr series to be updated for season monitoring")
	}
	if updatedSeries.QualityProfileID != 9 {
		t.Errorf("existing Sonarr series QualityProfileID changed to %d, want preserved 9", updatedSeries.QualityProfileID)
	}
	if !movieUpdated {
		t.Fatal("expected existing Radarr movie to be updated for unmonitoring")
	}
	if updatedMovie.QualityProfileID != 8 {
		t.Errorf("existing Radarr movie QualityProfileID changed to %d, want preserved 8", updatedMovie.QualityProfileID)
	}
}

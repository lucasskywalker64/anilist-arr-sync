package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/resolver"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

type cleanupLibrary struct {
	series        servarr.Series
	episodes      []servarr.Episode
	movie         servarr.Movie
	calls         []string
	failUnmonitor bool
	failTags      bool
	newSeries     bool
	emptyEpisodes bool
	tagLookups    int
}

func newCleanupTest(t *testing.T) (*Orchestrator, *cleanupLibrary) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "cleanup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	library := &cleanupLibrary{
		series: servarr.Series{ID: 1, TVDBID: 10, Monitored: true, Tags: []int{1}, Seasons: []servarr.Season{{SeasonNumber: 1, Monitored: true}}},
		movie:  servarr.Movie{ID: 2, TMDBID: 20, Monitored: true, Tags: []int{1}, HasFile: true, MovieFileID: 200},
		episodes: []servarr.Episode{
			{ID: 1, SeriesID: 1, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true, HasFile: true, EpisodeFileID: 101},
			{ID: 2, SeriesID: 1, SeasonNumber: 1, EpisodeNumber: 2, Monitored: true, HasFile: true, EpisodeFileID: 102},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			library.calls = append(library.calls, r.Method+" "+r.URL.Path)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			library.tagLookups++
			if library.failTags {
				http.Error(w, "tag failure", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			movies := []servarr.Movie{}
			if library.movie.ID > 0 {
				movies = append(movies, library.movie)
			}
			_ = json.NewEncoder(w).Encode(movies)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie/lookup/tmdb":
			_ = json.NewEncoder(w).Encode(servarr.Movie{TMDBID: 20})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie/2":
			_ = json.NewEncoder(w).Encode(library.movie)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			series := library.series
			if library.newSeries {
				series.ID = 0
			}
			_ = json.NewEncoder(w).Encode([]servarr.Series{series})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/1":
			_ = json.NewEncoder(w).Encode(library.series)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			if library.emptyEpisodes {
				_ = json.NewEncoder(w).Encode([]servarr.Episode{})
				return
			}
			_ = json.NewEncoder(w).Encode(library.episodes)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/series":
			var req servarr.AddSeriesRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.AddOptions.SearchForMissingEpisodes {
				library.calls = append(library.calls, "search on add")
			}
			library.newSeries = false
			_ = json.NewEncoder(w).Encode(library.series)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/movie":
			_ = json.NewEncoder(w).Encode(library.movie)
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
			var body struct {
				EpisodeIDs []int `json:"episodeIds"`
				Monitored  bool  `json:"monitored"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !body.Monitored && library.failUnmonitor {
				http.Error(w, "unmonitor failure", http.StatusUnauthorized)
				return
			}
			for i := range library.episodes {
				if containsInt(body.EpisodeIDs, library.episodes[i].ID) {
					library.episodes[i].Monitored = body.Monitored
				}
			}
			_ = json.NewEncoder(w).Encode([]any{})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/series":
			var updated servarr.Series
			_ = json.NewDecoder(r.Body).Decode(&updated)
			for _, season := range updated.Seasons {
				for _, old := range library.series.Seasons {
					if season.SeasonNumber == old.SeasonNumber && season.Monitored != old.Monitored {
						for i := range library.episodes {
							if library.episodes[i].SeasonNumber == season.SeasonNumber {
								library.episodes[i].Monitored = season.Monitored
							}
						}
					}
				}
			}
			library.series = updated
			_ = json.NewEncoder(w).Encode(updated)
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/movie":
			_ = json.NewDecoder(r.Body).Decode(&library.movie)
			_ = json.NewEncoder(w).Encode(library.movie)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/command":
			var body struct {
				EpisodeIDs []int `json:"episodeIds"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			for _, id := range body.EpisodeIDs {
				if id == 1 {
					t.Error("search includes watched episode")
				}
			}
			_ = json.NewEncoder(w).Encode(servarr.Command{ID: 1})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	sonarr, _ := servarr.NewSonarrClient(srv.URL, "key")
	radarr, _ := servarr.NewRadarrClient(srv.URL, "key")
	cfg := config.NewDefault()
	cfg.FirstRunDryRun = false
	cfg.TagName = "anilist-sync"
	return New(db, cfg, nil, sonarr, radarr), library
}

func cleanupEntry(status anilist.MediaListStatus) anilist.MediaListEntry {
	return anilist.MediaListEntry{Status: status, Progress: 1, Media: anilist.Media{ID: 100, Format: "TV", Title: anilist.MediaTitle{Romaji: "Anime"}}}
}

func TestCleanup_UnresolvedCour(t *testing.T) {
	for _, status := range []anilist.MediaListStatus{anilist.StatusCompleted, anilist.StatusCurrent} {
		t.Run(string(status), func(t *testing.T) {
			o, lib := newCleanupTest(t)
			o.cfg.DeleteCompleted, o.cfg.DeleteWatched, o.cfg.UnmonitorWatched = true, true, true
			entry := cleanupEntry(status)
			entry.Media.Title.Romaji = "Anime Part 2"
			report := &SyncReport{}
			o.dispatchSonarr(context.Background(), entry, &resolver.Result{TargetID: 10, TVDBSeason: 1}, report)
			if len(lib.calls) != 0 || len(report.Errors) == 0 {
				t.Fatalf("expected unresolved scope error without mutations, calls=%v errors=%v", lib.calls, report.Errors)
			}
		})
	}
}

func TestCleanup_UnmatchedKnownCour(t *testing.T) {
	for _, title := range []string{"Anime Part 3", "Anime"} {
		t.Run(title, func(t *testing.T) {
			o, lib := newCleanupTest(t)
			o.cfg.DeleteCompleted = true
			lib.episodes[0].FinaleType = "midseason"
			entry := cleanupEntry(anilist.StatusCompleted)
			entry.Media.Title.Romaji = title
			report := &SyncReport{}
			o.dispatchSonarr(context.Background(), entry, &resolver.Result{TargetID: 10, TVDBSeason: 1}, report)
			if len(lib.calls) != 0 || len(report.Errors) != 1 {
				t.Fatalf("unresolved split season mutated: calls=%v errors=%v", lib.calls, report.Errors)
			}
		})
	}
}

func TestCleanup_SharedFileAcrossCours(t *testing.T) {
	o, lib := newCleanupTest(t)
	o.cfg.DeleteCompleted = true
	lib.episodes[0].FinaleType = "midseason"
	lib.episodes[1].EpisodeFileID = 101
	entry := cleanupEntry(anilist.StatusCompleted)
	entry.Media.Title.Romaji = "Anime Part 2"
	report := &SyncReport{}
	o.dispatchSonarr(context.Background(), entry, &resolver.Result{TargetID: 10, TVDBSeason: 1}, report)
	if len(lib.calls) != 0 || len(report.Errors) != 0 {
		t.Fatalf("file shared across cour boundary deleted: calls=%v errors=%v", lib.calls, report.Errors)
	}
}

func TestCleanup_SeasonZero(t *testing.T) {
	o, lib := newCleanupTest(t)
	o.cfg.DeleteCompleted = true
	lib.series.Seasons = append(lib.series.Seasons, servarr.Season{SeasonNumber: 0, Monitored: true})
	lib.episodes = append(lib.episodes, servarr.Episode{ID: 3, SeasonNumber: 0, EpisodeNumber: 1, HasFile: true, EpisodeFileID: 103})
	entry := cleanupEntry(anilist.StatusCompleted)
	entry.Media.Title.Romaji = "Anime Season 1 Special"
	report := &SyncReport{}
	out := o.dispatchSonarr(context.Background(), entry, &resolver.Result{TargetID: 10, TVDBSeason: 0, Tier: resolver.TierOverride}, report)
	if !reflect.DeepEqual(lib.calls, []string{"DELETE /api/v3/episodefile/103"}) || out.season != 0 {
		t.Fatalf("wrong special scope: calls=%v outcome=%+v errors=%v", lib.calls, out, report.Errors)
	}
}

func TestCleanup_SharedFile(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "staged"}[dryRun], func(t *testing.T) {
			o, lib := newCleanupTest(t)
			o.cfg.DeleteWatched, o.cfg.FirstRunDryRun = true, dryRun
			lib.episodes[1].EpisodeFileID = 101
			report := &SyncReport{}
			o.dispatchSonarr(context.Background(), cleanupEntry(anilist.StatusCurrent), &resolver.Result{TargetID: 10, TVDBSeason: 1}, report)
			if len(lib.calls) != 0 {
				t.Fatalf("deleted shared file: %v", lib.calls)
			}
			actions, err := o.GetStagedActions(context.Background(), StagedStatusPending)
			if err != nil || len(actions) != 0 {
				t.Fatalf("staged shared file: %v, %v", actions, err)
			}
		})
	}
}

func TestCleanup_WatchedMonitoringAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserve watched prefix", true: "stop deletion on failure"}[fail], func(t *testing.T) {
			o, lib := newCleanupTest(t)
			o.cfg.UnmonitorWatched, o.cfg.DeleteWatched = true, true
			lib.failUnmonitor = fail
			lib.series.Seasons[0].Monitored = false
			lib.episodes[1].Monitored = false
			report := &SyncReport{}
			o.dispatchSonarr(context.Background(), cleanupEntry(anilist.StatusCurrent), &resolver.Result{TargetID: 10, TVDBSeason: 1}, report)
			if fail {
				for _, call := range lib.calls {
					if strings.HasPrefix(call, "DELETE") {
						t.Fatalf("deleted after unmonitor failed: %v", lib.calls)
					}
				}
				if len(report.Errors) == 0 {
					t.Fatal("missing unmonitor error")
				}
			} else if lib.episodes[0].Monitored || !lib.episodes[1].Monitored {
				t.Fatalf("wrong monitoring state: %+v", lib.episodes)
			}
		})
	}
}

func TestCleanup_DryRunPreservesMonitoring(t *testing.T) {
	o, _ := newCleanupTest(t)
	o.cfg.FirstRunDryRun, o.cfg.UnmonitorCompleted = true, true
	out := o.dispatchSonarr(context.Background(), cleanupEntry(anilist.StatusCompleted), &resolver.Result{TargetID: 10, TVDBSeason: 1}, &SyncReport{})
	if out.monitoredStatus != "MONITORED" {
		t.Fatalf("dry run changed monitoring to %s", out.monitoredStatus)
	}
}

func TestCleanup_RadarrLibraryLookup(t *testing.T) {
	o, lib := newCleanupTest(t)
	o.cfg.UnmonitorCompleted, o.cfg.DeleteCompleted = true, true
	report := &SyncReport{}
	o.dispatchRadarr(context.Background(), cleanupEntry(anilist.StatusCompleted), &resolver.Result{TargetID: 20}, report)
	if !reflect.DeepEqual(lib.calls, []string{"PUT /api/v3/movie", "DELETE /api/v3/moviefile/200"}) {
		t.Fatalf("cleanup failed: calls=%v errors=%v", lib.calls, report.Errors)
	}
}

func TestCleanup_CompletedImports(t *testing.T) {
	o, lib := newCleanupTest(t)
	o.cfg.AniListTvStatus, o.cfg.AniListMovieStatus = "COMPLETED", "COMPLETED"
	lib.newSeries, lib.movie.ID = true, 0
	report := &SyncReport{}
	o.dispatchSonarr(context.Background(), cleanupEntry(anilist.StatusCompleted), &resolver.Result{TargetID: 10, TVDBSeason: 1}, report)
	o.dispatchRadarr(context.Background(), cleanupEntry(anilist.StatusCompleted), &resolver.Result{TargetID: 20}, report)
	if !reflect.DeepEqual(lib.calls, []string{"POST /api/v3/series", "POST /api/v3/movie"}) {
		t.Fatalf("completed imports skipped: %v errors=%v", lib.calls, report.Errors)
	}
}

func TestCleanup_TagErrors(t *testing.T) {
	o, lib := newCleanupTest(t)
	o.cfg.DeleteCompleted = true
	lib.failTags = true
	report := &SyncReport{}
	o.dispatchSonarr(context.Background(), cleanupEntry(anilist.StatusCompleted), &resolver.Result{TargetID: 10, TVDBSeason: 1}, report)
	if len(report.Errors) != 1 || len(lib.calls) != 0 {
		t.Fatalf("tag failure hidden: %v calls=%v", report.Errors, lib.calls)
	}
}

func TestCleanup_StagedOrderingAndTagRemoval(t *testing.T) {
	o, lib := newCleanupTest(t)
	o.cfg.FirstRunDryRun, o.cfg.UnmonitorCompleted, o.cfg.DeleteCompleted = true, true, true
	o.dispatchSonarr(context.Background(), cleanupEntry(anilist.StatusCompleted), &resolver.Result{TargetID: 10, TVDBSeason: 1}, &SyncReport{})
	actions, err := o.GetStagedActions(context.Background(), StagedStatusPending)
	if err != nil || len(actions) != 2 {
		t.Fatalf("staging failed: %v %v", actions, err)
	}
	if err := o.ApplyStagedAction(context.Background(), actions[1].ID); err == nil {
		t.Fatal("deletion allowed before unmonitoring")
	}
	if err := o.ApplyStagedAction(context.Background(), actions[0].ID); err != nil {
		t.Fatal(err)
	}
	lib.series.Tags = nil
	if err := o.ApplyStagedAction(context.Background(), actions[1].ID); err == nil {
		t.Fatal("deletion allowed after managed tag removed")
	}
	lib.series.Tags = []int{1}
	if err := o.ApplyStagedAction(context.Background(), actions[1].ID); err != nil {
		t.Fatal(err)
	}
}

func TestCleanup_StagedMovieGuards(t *testing.T) {
	o, lib := newCleanupTest(t)
	o.cfg.FirstRunDryRun, o.cfg.UnmonitorCompleted, o.cfg.DeleteCompleted = true, true, true
	o.dispatchRadarr(context.Background(), cleanupEntry(anilist.StatusCompleted), &resolver.Result{TargetID: 20}, &SyncReport{})
	actions, err := o.GetStagedActions(context.Background(), StagedStatusPending)
	if err != nil || len(actions) != 2 {
		t.Fatalf("staging failed: %v %v", actions, err)
	}
	deleteID := actions[1].ID
	if err := o.ApplyStagedAction(context.Background(), deleteID); err == nil {
		t.Fatal("movie deleted before unmonitor")
	}
	if err := o.ApplyStagedAction(context.Background(), actions[0].ID); err != nil {
		t.Fatal(err)
	}
	lib.movie.Tags = nil
	if err := o.ApplyStagedAction(context.Background(), deleteID); err == nil {
		t.Fatal("untagged movie deleted")
	}
	lib.movie.Tags, lib.movie.MovieFileID = []int{1}, 201
	if err := o.ApplyStagedAction(context.Background(), deleteID); err == nil {
		t.Fatal("replaced movie file deleted")
	}
	lib.movie.MovieFileID = 200
	if err := o.ApplyStagedAction(context.Background(), deleteID); err != nil {
		t.Fatal(err)
	}
}

func TestCleanup_LegacyDeletionRequiresRestaging(t *testing.T) {
	o, lib := newCleanupTest(t)
	if err := o.stageAction(context.Background(), ActionDeleteFile, "MOVIE", "Movie", "RADARR", DeleteFilePayload{MovieID: 2, MovieFileID: 200}); err != nil {
		t.Fatal(err)
	}
	actions, err := o.GetStagedActions(context.Background(), StagedStatusPending)
	if err != nil || len(actions) != 1 {
		t.Fatalf("staging failed: %v %v", actions, err)
	}
	if err := o.ApplyStagedAction(context.Background(), actions[0].ID); err == nil || len(lib.calls) != 0 {
		t.Fatalf("legacy deletion ran without prerequisites: calls=%v err=%v", lib.calls, err)
	}
}

func TestCleanup_StagedFileScopeChanges(t *testing.T) {
	for _, change := range []string{"shared", "missing"} {
		t.Run(change, func(t *testing.T) {
			o, lib := newCleanupTest(t)
			o.cfg.FirstRunDryRun, o.cfg.DeleteWatched = true, true
			o.dispatchSonarr(context.Background(), cleanupEntry(anilist.StatusCurrent), &resolver.Result{TargetID: 10, TVDBSeason: 1}, &SyncReport{})
			actions, err := o.GetStagedActions(context.Background(), StagedStatusPending)
			if err != nil || len(actions) != 1 {
				t.Fatalf("staging failed: %v %v", actions, err)
			}
			if change == "shared" {
				lib.episodes[1].EpisodeFileID = 101
			} else {
				lib.episodes[0].HasFile, lib.episodes[0].EpisodeFileID = false, 0
			}
			err = o.ApplyStagedAction(context.Background(), actions[0].ID)
			if (change == "shared" && err == nil) || (change == "missing" && err != nil) || len(lib.calls) != 0 {
				t.Fatalf("unsafe staged file deletion: calls=%v err=%v", lib.calls, err)
			}
		})
	}
}

func TestCleanup_OverviewErrorRecordedOnce(t *testing.T) {
	o, _ := newCleanupTest(t)
	o.cfg.AniListUsername = "user"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.Query, "mediaId") {
			http.Error(w, "overview failure", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"MediaListCollection":{"lists":[],"hasNextChunk":false}}}`))
	}))
	defer srv.Close()
	o.al = anilist.NewClient(srv.URL)
	report, err := o.Sync(context.Background(), TriggerManualWeb)
	if err != nil || len(report.Errors) != 1 {
		t.Fatalf("expected one error, got %v %v", report, err)
	}
}

func TestCleanup_HistoricalUnmappedTitles(t *testing.T) {
	o, _ := newCleanupTest(t)
	o.cfg.AniListUsername, o.cfg.DeleteCompleted = "user", true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"MediaListCollection": map[string]any{"lists": []any{map[string]any{"status": "COMPLETED", "entries": []anilist.MediaListEntry{cleanupEntry(anilist.StatusCompleted)}}}, "hasNextChunk": false}}})
	}))
	defer srv.Close()
	o.al = anilist.NewClient(srv.URL)
	report, err := o.Sync(context.Background(), TriggerManualWeb)
	if err != nil || report.QueuedReview != 0 {
		t.Fatalf("historical title queued: %v %v", report, err)
	}
	if err := o.db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		var count int
		if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM review_queue").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("historical title persisted in review queue")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanup_StagingFailureStopsDeletion(t *testing.T) {
	o, lib := newCleanupTest(t)
	o.cfg.FirstRunDryRun, o.cfg.UnmonitorWatched, o.cfg.DeleteWatched = true, true, true
	if err := o.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TRIGGER reject_unmonitor BEFORE INSERT ON staged_sync_actions WHEN NEW.action_type = 'UNMONITOR_EPISODE' BEGIN SELECT RAISE(FAIL, 'staging failure'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	report := &SyncReport{}
	o.dispatchSonarr(context.Background(), cleanupEntry(anilist.StatusCurrent), &resolver.Result{TargetID: 10, TVDBSeason: 1}, report)
	actions, err := o.GetStagedActions(context.Background(), StagedStatusPending)
	if err != nil || len(actions) != 0 || len(report.Errors) == 0 || len(lib.calls) != 0 {
		t.Fatalf("deletion survived staging failure: actions=%v errors=%v err=%v", actions, report.Errors, err)
	}
}

func TestCleanup_NewSeriesPrunesBeforeSearch(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "staged"}[dryRun], func(t *testing.T) {
			o, lib := newCleanupTest(t)
			o.cfg.FirstRunDryRun, o.cfg.UnmonitorWatched, o.cfg.SonarrSearchOnAdd = dryRun, true, true
			lib.newSeries = true
			for i := range lib.episodes {
				lib.episodes[i].HasFile = false
			}
			report := &SyncReport{}
			o.dispatchSonarr(context.Background(), cleanupEntry(anilist.StatusCurrent), &resolver.Result{TargetID: 10, TVDBSeason: 1}, report)
			if dryRun {
				if len(lib.calls) != 0 {
					t.Fatalf("dry run made changes: %v", lib.calls)
				}
				actions, err := o.GetStagedActions(context.Background(), StagedStatusPending)
				if err != nil || len(actions) != 1 {
					t.Fatalf("staging failed: %v %v", actions, err)
				}
				if err := o.ApplyStagedAction(context.Background(), actions[0].ID); err != nil {
					t.Fatal(err)
				}
			}
			want := []string{"POST /api/v3/series", "PUT /api/v3/episode/monitor", "POST /api/v3/command"}
			if !reflect.DeepEqual(lib.calls, want) || lib.episodes[0].Monitored || len(report.Errors) > 0 {
				t.Fatalf("search ran before pruning: calls=%v errors=%v", lib.calls, report.Errors)
			}
		})
	}
}

func TestCleanup_StagedAdditionRetriesPruningWithoutAddingAgain(t *testing.T) {
	o, lib := newCleanupTest(t)
	o.cfg.FirstRunDryRun, o.cfg.UnmonitorWatched, o.cfg.SonarrSearchOnAdd = true, true, true
	lib.newSeries, lib.failUnmonitor = true, true
	for i := range lib.episodes {
		lib.episodes[i].HasFile = false
	}
	o.dispatchSonarr(context.Background(), cleanupEntry(anilist.StatusCurrent), &resolver.Result{TargetID: 10, TVDBSeason: 1}, &SyncReport{})
	actions, err := o.GetStagedActions(context.Background(), StagedStatusPending)
	if err != nil || len(actions) != 1 {
		t.Fatalf("staging failed: %v %v", actions, err)
	}
	if err := o.ApplyStagedAction(context.Background(), actions[0].ID); err == nil {
		t.Fatal("failed pruning reported success")
	}
	for _, call := range lib.calls {
		if call == "POST /api/v3/command" || call == "search on add" {
			t.Fatalf("searched after failed pruning: %v", lib.calls)
		}
	}
	lib.failUnmonitor = false
	if err := o.ApplyStagedAction(context.Background(), actions[0].ID); err != nil {
		t.Fatal(err)
	}
	adds := 0
	for _, call := range lib.calls {
		if call == "POST /api/v3/series" {
			adds++
		}
	}
	if adds != 1 || lib.episodes[0].Monitored {
		t.Fatalf("retry added twice or lost progress: %v", lib.calls)
	}
}

func setupCleanupSync(t *testing.T, o *Orchestrator) {
	t.Helper()
	o.cfg.AniListUsername = "user"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"MediaListCollection": map[string]any{
			"lists":        []any{map[string]any{"status": "CURRENT", "entries": []anilist.MediaListEntry{cleanupEntry(anilist.StatusCurrent)}}},
			"hasNextChunk": false,
		}}})
	}))
	t.Cleanup(srv.Close)
	o.al = anilist.NewClient(srv.URL)
	if err := o.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons) VALUES (100, 'SERIES', 10, '1')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanup_NormalMonitoringDoesNotNeedTags(t *testing.T) {
	o, lib := newCleanupTest(t)
	setupCleanupSync(t, o)
	lib.failTags = true
	lib.series.Seasons[0].Monitored = false
	report, err := o.Sync(context.Background(), TriggerManualWeb)
	if err != nil || len(report.Errors) != 0 || !lib.series.Seasons[0].Monitored || lib.tagLookups != 0 {
		t.Fatalf("normal monitoring depended on tags: report=%+v err=%v lookups=%d", report, err, lib.tagLookups)
	}
	if err := o.db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		var count int
		if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM synced_media WHERE anilist_id = 100").Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Errorf("monitored series was not recorded")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanup_DelayedEpisodesRetainSearch(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "staged"}[dryRun], func(t *testing.T) {
			o, lib := newCleanupTest(t)
			setupCleanupSync(t, o)
			o.cfg.FirstRunDryRun, o.cfg.UnmonitorWatched, o.cfg.SonarrSearchOnAdd = dryRun, true, true
			lib.newSeries, lib.emptyEpisodes = true, true
			for i := range lib.episodes {
				lib.episodes[i].HasFile = false
			}
			report, err := o.Sync(context.Background(), TriggerManualWeb)
			if err != nil || len(report.Errors) != 0 {
				t.Fatalf("initial sync failed: %+v %v", report, err)
			}
			var actionID int
			if dryRun {
				actions, err := o.GetStagedActions(context.Background(), StagedStatusPending)
				if err != nil || len(actions) != 1 {
					t.Fatalf("staging failed: %v %v", actions, err)
				}
				actionID = actions[0].ID
				if err := o.ApplyStagedAction(context.Background(), actionID); err == nil {
					t.Fatal("empty episode list completed staged addition")
				}
			}
			if !reflect.DeepEqual(lib.calls, []string{"POST /api/v3/series"}) {
				t.Fatalf("searched or pruned before refresh: %v", lib.calls)
			}
			if err := o.db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
				var count int
				if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM synced_media WHERE anilist_id = 100").Scan(&count); err != nil {
					return err
				}
				if count != 1 {
					t.Error("successful addition was not recorded")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			// A fresh orchestrator must retain the follow-up across sync runs.
			o = New(o.db, o.cfg, o.al, o.sonarr, o.radarr)
			lib.emptyEpisodes = false
			if dryRun {
				if err := o.ApplyStagedAction(context.Background(), actionID); err != nil {
					t.Fatal(err)
				}
				actions, err := o.GetStagedActions(context.Background(), StagedStatusPending)
				if err != nil || len(actions) != 0 {
					t.Fatalf("completed addition remained pending: %v %v", actions, err)
				}
			} else {
				report, err = o.Sync(context.Background(), TriggerManualWeb)
				if err != nil || len(report.Errors) != 0 {
					t.Fatalf("follow-up sync failed: %+v %v", report, err)
				}
				if _, err := o.Sync(context.Background(), TriggerManualWeb); err != nil {
					t.Fatal(err)
				}
			}
			want := []string{"POST /api/v3/series", "PUT /api/v3/episode/monitor", "POST /api/v3/command"}
			if !reflect.DeepEqual(lib.calls, want) || lib.episodes[0].Monitored {
				t.Fatalf("deferred follow-up lost or repeated: %v", lib.calls)
			}
		})
	}
}

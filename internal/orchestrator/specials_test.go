package orchestrator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func TestSync_SpecialsEpisodeScope(t *testing.T) {
	for _, seasonNumber := range []int{0, 1} {
		for _, dryRun := range []bool{false, true} {
			for _, scenario := range []string{"monitor", "watched", "completed", "dropped", "missing", "absent series", "missing before apply", "shared file"} {
				t.Run(fmt.Sprintf("season=%d/%s/staged=%v", seasonNumber, scenario, dryRun), func(t *testing.T) {
					ctx := context.Background()
					db := setupTestDB(t)
					if err := db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
						_, err := tx.ExecContext(ctx, `INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons, episode_numbers) VALUES (204431, 'SERIES', 423075, ?, '[7,4,6,5]')`, fmt.Sprint(seasonNumber))
						return err
					}); err != nil {
						t.Fatal(err)
					}
					entry := anilist.MediaListEntry{Status: anilist.StatusCurrent, Media: anilist.Media{ID: 204431, Format: "SPECIAL", Title: anilist.MediaTitle{Romaji: "Narumi no Heijitsu"}}}
					if scenario == "watched" {
						entry.Progress = 2
					}
					if scenario == "completed" || scenario == "shared file" {
						entry.Status = anilist.StatusCompleted
					}
					if scenario == "dropped" {
						entry.Status = anilist.StatusDropped
					}
					episodes := []servarr.Episode{}
					for _, number := range []int{7, 2, 4, 1, 6, 5} {
						hasFile := scenario != "monitor" && scenario != "missing before apply" && (scenario != "watched" || number < 6)
						episodes = append(episodes, servarr.Episode{ID: 100 + number, SeriesID: 10, SeasonNumber: seasonNumber, EpisodeNumber: number, Monitored: number < 4 || hasFile, HasFile: hasFile, EpisodeFileID: 200 + number})
					}
					if scenario == "missing" {
						episodes = episodes[1:]
					}
					if scenario == "shared file" {
						episodes[1].EpisodeFileID = 204
					}
					series := servarr.Series{ID: 10, TVDBID: 423075, Title: "Kaiju No. 8", Monitored: true, Tags: []int{1}, Seasons: []servarr.Season{{SeasonNumber: seasonNumber, Monitored: true}}}
					var monitored, unmonitored, searched, deleted []int
					mutations := 0
					sonarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						if r.Method != http.MethodGet {
							mutations++
						}
						switch {
						case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
							lookup := series
							if scenario == "absent series" {
								lookup.ID = 0
							}
							_ = json.NewEncoder(w).Encode([]servarr.Series{lookup})
						case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/10":
							_ = json.NewEncoder(w).Encode(series)
						case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
							_ = json.NewEncoder(w).Encode(episodes)
						case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
							_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
						case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
							var body struct {
								EpisodeIDs []int
								Monitored  bool
							}
							if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
								t.Error(err)
							}
							if body.Monitored {
								monitored = append(monitored, body.EpisodeIDs...)
							} else {
								unmonitored = append(unmonitored, body.EpisodeIDs...)
							}
							for i := range episodes {
								if slices.Contains(body.EpisodeIDs, episodes[i].ID) {
									episodes[i].Monitored = body.Monitored
								}
							}
							_, _ = w.Write([]byte(`[]`))
						case r.Method == http.MethodPost && r.URL.Path == "/api/v3/command":
							var body struct {
								Name       string
								EpisodeIDs []int
							}
							if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
								t.Error(err)
							}
							if body.Name != "EpisodeSearch" {
								t.Errorf("unscoped search %s", body.Name)
							}
							searched = append(searched, body.EpisodeIDs...)
							_ = json.NewEncoder(w).Encode(servarr.Command{ID: 1})
						case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v3/episodefile/"):
							var id int
							_, _ = fmt.Sscanf(r.URL.Path, "/api/v3/episodefile/%d", &id)
							deleted = append(deleted, id)
							for i := range episodes {
								if episodes[i].EpisodeFileID == id {
									episodes[i].HasFile = false
								}
							}
						default:
							t.Errorf("unexpected Sonarr request: %s %s", r.Method, r.URL.Path)
							http.NotFound(w, r)
						}
					}))
					defer sonarr.Close()
					al := setupMockAniList(t, []anilist.MediaListEntry{entry})
					cfg := config.NewDefault()
					cfg.AniListUsername = "user"
					cfg.FirstRunDryRun = dryRun
					cfg.SonarrSearchOnAdd = true
					cfg.TagName = "anilist-sync"
					cfg.UnmonitorWatched, cfg.DeleteWatched = true, true
					cfg.UnmonitorCompleted, cfg.DeleteCompleted = true, true
					cfg.UnmonitorDropped, cfg.DeleteDropped = true, true
					client, err := servarr.NewSonarrClient(sonarr.URL, "key")
					if err != nil {
						t.Fatal(err)
					}
					o := orchestrator.New(db, cfg, anilist.NewClient(al.URL), client, nil)
					report, err := o.Sync(ctx, orchestrator.TriggerCLI)
					if err != nil || len(report.Errors) > 0 {
						t.Fatalf("sync: %v, %+v", err, report)
					}
					actions, err := o.GetStagedActions(ctx, orchestrator.StagedStatusPending)
					if err != nil {
						t.Fatal(err)
					}
					if scenario == "missing" || scenario == "absent series" {
						if report.QueuedReview != 1 || mutations != 0 || len(actions) != 0 {
							t.Fatalf("missing selection produced actions: %+v, mutations=%d, actions=%+v", report, mutations, actions)
						}
						assertSpecialsInReview(t, db)
						return
					}
					if dryRun {
						if mutations != 0 || len(actions) == 0 {
							t.Fatalf("dry run: mutations=%d, actions=%+v", mutations, actions)
						}
						if scenario == "missing before apply" {
							episodes = episodes[1:]
						}
						for _, action := range actions {
							var payload struct{ EpisodeNumbers []int }
							if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
								t.Fatal(err)
							}
							if len(payload.EpisodeNumbers) == 0 {
								t.Fatalf("missing preview scope: %s", action.PayloadJSON)
							}
							err := o.ApplyStagedAction(ctx, action.ID)
							if scenario == "missing before apply" {
								if err == nil || mutations != 0 {
									t.Fatalf("applied missing selection: %v, mutations=%d", err, mutations)
								}
								assertSpecialsInReview(t, db)
								return
							}
							if err != nil {
								t.Fatal(err)
							}
						}
					}
					wantMonitor, wantUnmonitor, wantSearch, wantDelete := []int{104, 105, 106, 107}, []int(nil), []int{104, 105, 106, 107}, []int(nil)
					switch scenario {
					case "watched":
						wantMonitor, wantUnmonitor, wantSearch, wantDelete = []int{106, 107}, []int{104, 105}, []int{106, 107}, []int{204, 205}
					case "completed", "dropped":
						wantMonitor, wantUnmonitor, wantSearch, wantDelete = nil, []int{104, 105, 106, 107}, nil, []int{204, 205, 206, 207}
					case "shared file":
						wantMonitor, wantUnmonitor, wantSearch, wantDelete = nil, []int{104, 105, 106, 107}, nil, []int{205, 206, 207}
					}
					for _, check := range []struct {
						name      string
						got, want []int
					}{{"monitor", monitored, wantMonitor}, {"unmonitor", unmonitored, wantUnmonitor}, {"search", searched, wantSearch}, {"delete", deleted, wantDelete}} {
						slices.Sort(check.got)
						if !reflect.DeepEqual(check.got, check.want) {
							t.Errorf("%s: got %v, want %v", check.name, check.got, check.want)
						}
					}
					for _, ep := range episodes {
						if (ep.EpisodeNumber == 1 || ep.EpisodeNumber == 2) && !ep.Monitored {
							t.Errorf("outside episode changed: %+v", ep)
						}
					}
				})
			}
		}
	}
}

func assertSpecialsInReview(t *testing.T, db *storage.DB) {
	t.Helper()
	var count int
	if err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_queue WHERE anilist_id = 204431 AND status = 'PENDING' AND reason = 'MISSING_OVERRIDE_EPISODES'`).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one pending review, got %d", count)
	}
}

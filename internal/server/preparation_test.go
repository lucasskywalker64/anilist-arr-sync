package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

func TestReviewPreparationLifecycle(t *testing.T) {
	for _, tc := range []struct{ existing, confirm, cancelEarly, adopted, failDelete bool }{{}, {existing: true}, {confirm: true}, {cancelEarly: true}, {adopted: true}, {failDelete: true}} {
		existing := tc.existing
		name := map[bool]string{false: "new", true: "existing"}[existing]
		if tc.confirm {
			name += "/confirm"
		}
		if tc.cancelEarly {
			name += "/cancel-during-refresh"
		}
		if tc.adopted {
			name += "/adopted"
		}
		if tc.failDelete {
			name += "/retry-cleanup"
		}
		t.Run(name, func(t *testing.T) {
			db := newTestStorage(t)
			if err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `INSERT INTO review_queue(id,anilist_id,media_type,title_romaji,candidates_json,reason,status) VALUES(1,100,'SERIES','Anime Part 2','[]','UNMAPPED','PENDING')`)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			ready := existing
			added := existing
			deleted := false
			failDelete := tc.failDelete
			series := servarr.Series{ID: 10, TVDBID: 123, Title: "Anime", Path: "/anime/Anime", QualityProfileID: 1, Seasons: []servarr.Season{{SeasonNumber: 1}}}
			series.Monitored = existing
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v3/series/lookup":
					result := series
					if !added {
						result.ID = 0
					}
					_ = json.NewEncoder(w).Encode([]servarr.Series{result})
				case r.URL.Path == "/api/v3/tag" && r.Method == http.MethodGet:
					_ = json.NewEncoder(w).Encode([]servarr.Tag{{ID: 1, Label: "anilist-sync"}})
				case r.URL.Path == "/api/v3/tag" && r.Method == http.MethodPost:
					var tag servarr.Tag
					_ = json.NewDecoder(r.Body).Decode(&tag)
					tag.ID = 99
					_ = json.NewEncoder(w).Encode(tag)
				case r.URL.Path == "/api/v3/tag/99" && r.Method == http.MethodDelete:
					w.WriteHeader(http.StatusOK)
				case r.URL.Path == "/api/v3/series" && r.Method == http.MethodPost:
					var req servarr.AddSeriesRequest
					_ = json.NewDecoder(r.Body).Decode(&req)
					if req.Monitored || req.AddOptions == nil || req.AddOptions.Monitor != "none" || req.AddOptions.SearchForMissingEpisodes || req.AddOptions.SearchForCutoffUnmetEpisodes {
						t.Errorf("unsafe add: %+v", req)
					}
					series.Tags = req.Tags
					added = true
					series.AddOptions = req.AddOptions
					_ = json.NewEncoder(w).Encode(series)
				case r.URL.Path == "/api/v3/series" && r.Method == http.MethodPut:
					_ = json.NewDecoder(r.Body).Decode(&series)
					_ = json.NewEncoder(w).Encode(series)
				case r.URL.Path == "/api/v3/series/10" && r.Method == http.MethodDelete:
					if failDelete {
						http.Error(w, "try again", http.StatusBadRequest)
						return
					}
					if existing {
						t.Error("deleted pre-existing series")
					}
					if r.URL.Query().Get("deleteFiles") != "false" || r.URL.Query().Get("addImportListExclusion") != "false" {
						t.Error("unsafe deletion flags")
					}
					deleted = true
					added = false
				case r.URL.Path == "/api/v3/series/10":
					result := series
					if ready {
						result.AddOptions = nil
					}
					_ = json.NewEncoder(w).Encode(result)
				case r.URL.Path == "/api/v3/command":
					status := "started"
					if ready {
						status = "completed"
					}
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 7, "name": "RefreshSeries", "status": status, "body": map[string]any{"seriesIds": []int{10}, "isNewSeries": true}}})
				case r.URL.Path == "/api/v3/episode":
					_ = json.NewEncoder(w).Encode([]servarr.Episode{{ID: 101, SeriesID: 10, SeasonNumber: 1, EpisodeNumber: 1}, {ID: 113, SeriesID: 10, SeasonNumber: 1, EpisodeNumber: 13, Title: "Part 2 premiere"}})
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer remote.Close()
			cfg := config.NewDefault()
			cfg.APIKey = "key"
			cfg.AuthenticationRequired = "DisabledForLocalAddresses"
			cfg.SonarrURL = remote.URL
			cfg.SonarrAPIKey = "key"
			cfg.SonarrRootFolderPath = "/anime"
			client, _ := servarr.NewSonarrClient(remote.URL, "key")
			orch := orchestrator.New(db, cfg, nil, client, nil)
			handler := NewServer(cfg, "", db, orch, nil).Handler()
			request := func(method, path, body string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.RemoteAddr = "127.0.0.1:1234"
				req.Header.Set("X-Api-Key", "key")
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				return rec
			}
			rec := request(http.MethodPost, "/api/v1/review/1/prepare", `{"targetId":123}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("prepare %d: %s", rec.Code, rec.Body.String())
			}
			if !existing && !strings.Contains(rec.Body.String(), `"status":"WAITING"`) {
				t.Fatalf("not waiting: %s", rec.Body.String())
			}
			if tc.cancelEarly {
				rec = request(http.MethodDelete, "/api/v1/review/1/preparation", "")
				if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"CANCELLING"`) || deleted {
					t.Fatalf("cancel during refresh: %d %s", rec.Code, rec.Body.String())
				}
				ready = true
				restarted := orchestrator.New(db, cfg, nil, client, nil)
				if err := restarted.MaintainReviewPreparations(context.Background()); err != nil {
					t.Fatal(err)
				}
				if !deleted {
					t.Fatal("restart lost cleanup ownership")
				}
				return
			}
			ready = true
			rec = request(http.MethodGet, "/api/v1/review/1/preparation", "")
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"READY"`) || !strings.Contains(rec.Body.String(), "Part 2 premiere") {
				t.Fatalf("poll: %d %s", rec.Code, rec.Body.String())
			}
			if tc.confirm {
				rec = request(http.MethodPost, "/api/v1/review/1/resolve", `{"mediaType":"SERIES","tvdbId":123,"seasons":"1","episodeNumbers":[13]}`)
				if rec.Code != 200 {
					t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
				}
				rec = request(http.MethodGet, "/api/v1/overrides", "")
				if !strings.Contains(rec.Body.String(), `"episodeNumbers":[13]`) {
					t.Fatalf("lost episode mapping: %s", rec.Body.String())
				}
			}
			if tc.adopted {
				series.Monitored = true
			}
			rec = request(http.MethodDelete, "/api/v1/review/1/preparation", "")
			if tc.failDelete {
				if rec.Code == 200 {
					t.Fatal("cleanup failure was hidden")
				}
				failDelete = false
				restarted := orchestrator.New(db, cfg, nil, client, nil)
				if err := restarted.MaintainReviewPreparations(context.Background()); err != nil {
					t.Fatal(err)
				}
				if !deleted {
					t.Fatal("failed deletion was not retried")
				}
				return
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("cancel: %d %s", rec.Code, rec.Body.String())
			}
			if deleted == (existing || tc.confirm || tc.adopted) {
				t.Fatalf("deleted=%v, pre-existing=%v", deleted, existing)
			}
			if existing && !series.Monitored {
				t.Fatal("changed pre-existing monitoring")
			}
		})
	}
}

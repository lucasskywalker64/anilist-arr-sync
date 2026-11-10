package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/resolver"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

func TestSpecialsOverrideValidationAndResolution(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"selected specials", `"mediaType":"SERIES","tvdbId":423075,"seasons":"0","episodeNumbers":[7,4,6,5]`, true},
		{"empty specials", `"mediaType":"SERIES","tvdbId":423075,"seasons":"0","episodeNumbers":[]`, false},
		{"omitted episodes", `"mediaType":"SERIES","tvdbId":423075,"seasons":"0"`, false},
		{"padded zero season", `"mediaType":"SERIES","tvdbId":423075,"seasons":"00"`, false},
		{"signed zero season", `"mediaType":"SERIES","tvdbId":423075,"seasons":"+0"`, false},
		{"mixed specials without episodes", `"mediaType":"SERIES","tvdbId":423075,"seasons":"0,1"`, false},
		{"later specials without episodes", `"mediaType":"SERIES","tvdbId":423075,"seasons":"1;0"`, false},
		{"zero episode", `"mediaType":"SERIES","tvdbId":423075,"seasons":"0","episodeNumbers":[0]`, false},
		{"negative episode", `"mediaType":"SERIES","tvdbId":423075,"seasons":"0","episodeNumbers":[-1]`, false},
		{"duplicate episode", `"mediaType":"SERIES","tvdbId":423075,"seasons":"0","episodeNumbers":[4,4]`, false},
		{"multiple regular seasons", `"mediaType":"SERIES","tvdbId":423075,"seasons":"1,2","episodeNumbers":[4]`, false},
		{"multiple seasons", `"mediaType":"SERIES","tvdbId":423075,"seasons":"0,1","episodeNumbers":[4]`, false},
		{"movie", `"mediaType":"MOVIE","tmdbId":1,"seasons":"0","episodeNumbers":[4]`, false},
	} {
		for _, endpoint := range []string{"review", "overrides"} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				db := newTestStorage(t)
				cfg := config.NewDefault()
				cfg.AuthenticationRequired = "DisabledForLocalAddresses"
				cfg.APIKey = "key"
				handler := NewServer(cfg, "", db, nil, nil).Handler()
				if err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, `INSERT INTO review_queue (id, anilist_id, media_type, title_romaji, reason, status, candidates_json) VALUES (1, 204431, 'SERIES', 'Narumi no Heijitsu', 'SPECIAL_WITHOUT_MAPPING', 'PENDING', '[]')`)
					return err
				}); err != nil {
					t.Fatal(err)
				}
				path := "/api/v1/overrides"
				if endpoint == "review" {
					path = "/api/v1/review/1/resolve"
				}
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"anilistId":204431,`+tc.body+`}`))
				req.RemoteAddr = "127.0.0.1:1234"
				req.Header.Set("X-Api-Key", "key")
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if !tc.valid {
					if rec.Code != http.StatusBadRequest {
						t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
					}
				} else if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
					t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
				}
				req = httptest.NewRequest(http.MethodGet, "/api/v1/overrides", nil)
				req.RemoteAddr = "127.0.0.1:1234"
				rec = httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				var overrides []MappingOverride
				if err := json.Unmarshal(rec.Body.Bytes(), &overrides); err != nil {
					t.Fatal(err)
				}
				if !tc.valid {
					if len(overrides) != 0 {
						t.Fatalf("invalid override saved: %+v", overrides)
					}
					return
				}
				if len(overrides) != 1 || overrides[0].Seasons != "0" || !reflect.DeepEqual(overrides[0].EpisodeNumbers, []int{4, 5, 6, 7}) {
					t.Fatalf("saved override: %+v", overrides)
				}
				result, err := resolver.New(db).Resolve(context.Background(), anilist.Media{ID: 204431, Format: "SPECIAL"})
				if err != nil {
					t.Fatal(err)
				}
				if !result.Resolved || result.TargetID != 423075 || result.TVDBSeason != 0 || !reflect.DeepEqual(result.EpisodeNumbers, []int{4, 5, 6, 7}) {
					t.Fatalf("resolved mapping: %+v", result)
				}
			})
		}
	}
}

func TestSpecialEpisodesListsNumbersAndTitles(t *testing.T) {
	sonarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series/lookup":
			_ = json.NewEncoder(w).Encode([]servarr.Series{{ID: 10, TVDBID: 423075}})
		case "/api/v3/episode":
			if r.URL.Query().Get("seriesId") != "10" {
				t.Error("wrong series")
			}
			_ = json.NewEncoder(w).Encode([]servarr.Episode{
				{ID: 107, SeasonNumber: 0, EpisodeNumber: 7, Title: "Special seven"},
				{ID: 11, SeasonNumber: 1, EpisodeNumber: 1, Title: "Regular episode"},
				{ID: 104, SeasonNumber: 0, EpisodeNumber: 4, Title: "Special four"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarr.Close()
	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.SonarrURL, cfg.SonarrAPIKey = sonarr.URL, "key"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sonarr/specials/423075", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	NewServer(cfg, "", nil, nil, nil).Handler().ServeHTTP(rec, req)
	var episodes []servarr.Episode
	if err := json.Unmarshal(rec.Body.Bytes(), &episodes); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String())
	}
	if rec.Code != http.StatusOK || len(episodes) != 2 || episodes[0].EpisodeNumber != 4 || episodes[0].Title != "Special four" || episodes[1].EpisodeNumber != 7 {
		t.Fatalf("got %d: %+v", rec.Code, episodes)
	}
}

package servarr_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

func TestSonarrClient_LookupSeriesByTVDBID(t *testing.T) {
	t.Run("successfully returns series by tvdb id", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Fatalf("expected GET, got %s", r.Method)
			}
			if r.Header.Get("X-Api-Key") != "sonarr-key" {
				t.Fatalf("expected X-Api-Key 'sonarr-key', got %q", r.Header.Get("X-Api-Key"))
			}

			if r.URL.Path == "/api/v3/series/lookup" && r.URL.Query().Get("term") == "tvdb:81797" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]servarr.Series{
					{
						TVDBID:           81797,
						Title:            "One Punch Man",
						SeriesType:       "anime",
						QualityProfileID: 1,
						Monitored:        true,
						Seasons: []servarr.Season{
							{SeasonNumber: 1, Monitored: true},
							{SeasonNumber: 2, Monitored: false},
						},
					},
				})
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		series, err := client.LookupSeriesByTVDBID(context.Background(), 81797)
		if err != nil {
			t.Fatalf("LookupSeriesByTVDBID failed: %v", err)
		}
		if series == nil {
			t.Fatal("expected non-nil series")
		}
		if series.TVDBID != 81797 {
			t.Fatalf("expected TVDBID 81797, got %d", series.TVDBID)
		}
		if series.Title != "One Punch Man" {
			t.Fatalf("expected title 'One Punch Man', got %q", series.Title)
		}
		if len(series.Seasons) != 2 {
			t.Fatalf("expected 2 seasons, got %d", len(series.Seasons))
		}
	})

	t.Run("returns ErrNotFound when series not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v3/series/lookup" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]servarr.Series{})
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		series, err := client.LookupSeriesByTVDBID(context.Background(), 999999)
		if !errors.Is(err, servarr.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
		if series != nil {
			t.Fatalf("expected nil series, got %+v", series)
		}
	})
}

func TestSonarrClient_AddSeries(t *testing.T) {
	t.Run("successfully adds series with initial season monitoring", func(t *testing.T) {
		var received servarr.AddSeriesRequest
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/v3/series" {
				t.Fatalf("expected POST /api/v3/series, got %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("X-Api-Key") != "sonarr-key" {
				t.Fatalf("expected X-Api-Key 'sonarr-key', got %q", r.Header.Get("X-Api-Key"))
			}

			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode body: %v", err)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(servarr.Series{
				ID:               202,
				Title:            received.Title,
				TVDBID:           received.TVDBID,
				QualityProfileID: received.QualityProfileID,
				RootFolderPath:   received.RootFolderPath,
				SeriesType:       received.SeriesType,
				Monitored:        received.Monitored,
				SeasonFolder:     received.SeasonFolder,
				Seasons:          received.Seasons,
				Tags:             received.Tags,
			})
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		req := servarr.AddSeriesRequest{
			Title:            "Attack on Titan",
			TVDBID:           267440,
			QualityProfileID: 1,
			RootFolderPath:   "/tv",
			SeriesType:       "anime",
			Monitored:        true,
			SeasonFolder:     true,
			Seasons: []servarr.Season{
				{SeasonNumber: 1, Monitored: true},
				{SeasonNumber: 2, Monitored: false},
			},
			Tags: []int{42},
			AddOptions: &servarr.AddSeriesOptions{
				SearchForMissingEpisodes: true,
			},
		}

		series, err := client.AddSeries(context.Background(), req)
		if err != nil {
			t.Fatalf("AddSeries failed: %v", err)
		}
		if series.ID != 202 {
			t.Fatalf("expected ID 202, got %d", series.ID)
		}
		if series.Title != "Attack on Titan" {
			t.Fatalf("expected title 'Attack on Titan', got %q", series.Title)
		}
		if len(series.Seasons) != 2 || series.Seasons[1].Monitored {
			t.Fatalf("unexpected seasons in created series: %+v", series.Seasons)
		}
		if received.TVDBID != 267440 {
			t.Fatalf("expected TVDBID 267440, got %d", received.TVDBID)
		}
	})
}

func TestSonarrClient_GetEpisodes(t *testing.T) {
	t.Run("successfully queries episode premiere dates", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/api/v3/episode" {
				t.Fatalf("expected GET /api/v3/episode, got %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("X-Api-Key") != "sonarr-key" {
				t.Fatalf("expected X-Api-Key 'sonarr-key', got %q", r.Header.Get("X-Api-Key"))
			}
			if r.URL.Query().Get("seriesId") != "202" {
				t.Fatalf("expected seriesId=202, got %q", r.URL.Query().Get("seriesId"))
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]servarr.Episode{
				{
					ID:            1001,
					SeriesID:      202,
					SeasonNumber:  1,
					EpisodeNumber: 1,
					Title:         "To You, in 2000 Years",
					AirDate:       "2013-04-07",
					Monitored:     true,
				},
				{
					ID:            1026,
					SeriesID:      202,
					SeasonNumber:  2,
					EpisodeNumber: 1,
					Title:         "Beast Titan",
					AirDate:       "2017-04-01",
					Monitored:     false,
					FinaleType:    "season",
				},
			})
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		episodes, err := client.GetEpisodes(context.Background(), 202)
		if err != nil {
			t.Fatalf("GetEpisodes failed: %v", err)
		}
		if len(episodes) != 2 {
			t.Fatalf("expected 2 episodes, got %d", len(episodes))
		}
		if episodes[0].SeasonNumber != 1 || episodes[0].EpisodeNumber != 1 {
			t.Fatalf("unexpected episode 0: %+v", episodes[0])
		}
		if episodes[1].FinaleType != "season" {
			t.Fatalf("expected finale type 'season', got %q", episodes[1].FinaleType)
		}

		airDate, ok := episodes[0].PremiereDate()
		if !ok || airDate.Year() != 2013 || airDate.Month() != 4 || airDate.Day() != 7 {
			t.Fatalf("unexpected parsed air date: %v, ok=%v", airDate, ok)
		}
	})
}

func TestSonarrClient_GetSeriesByID(t *testing.T) {
	t.Run("successfully returns series by id", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/api/v3/series/202" {
				t.Fatalf("expected GET /api/v3/series/202, got %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(servarr.Series{
				ID:     202,
				Title:  "Attack on Titan",
				TVDBID: 267440,
			})
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		series, err := client.GetSeriesByID(context.Background(), 202)
		if err != nil {
			t.Fatalf("GetSeriesByID failed: %v", err)
		}
		if series.ID != 202 {
			t.Fatalf("expected ID 202, got %d", series.ID)
		}
	})

	t.Run("returns ErrNotFound when series does not exist", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		series, err := client.GetSeriesByID(context.Background(), 999)
		if !errors.Is(err, servarr.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
		if series != nil {
			t.Fatalf("expected nil series, got %+v", series)
		}
	})
}

func TestSonarrClient_UpdateSeries(t *testing.T) {
	t.Run("successfully updates series without conflict", func(t *testing.T) {
		var received servarr.Series
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut || r.URL.Path != "/api/v3/series" {
				t.Fatalf("expected PUT /api/v3/series, got %s %s", r.Method, r.URL.Path)
			}
			_ = json.NewDecoder(r.Body).Decode(&received)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(received)
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		target := &servarr.Series{
			ID:     202,
			Title:  "Attack on Titan",
			TVDBID: 267440,
			Seasons: []servarr.Season{
				{SeasonNumber: 1, Monitored: true},
				{SeasonNumber: 2, Monitored: true},
			},
		}

		updated, err := client.UpdateSeries(context.Background(), target)
		if err != nil {
			t.Fatalf("UpdateSeries failed: %v", err)
		}
		if len(updated.Seasons) != 2 || !updated.Seasons[1].Monitored {
			t.Fatalf("expected season 2 to be monitored: %+v", updated.Seasons)
		}
	})

	t.Run("catches 409 conflict, refetches latest model, and retries successfully", func(t *testing.T) {
		putAttempts := 0
		getAttempts := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPut && r.URL.Path == "/api/v3/series":
				putAttempts++
				if putAttempts == 1 {
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(`{"message": "Series has been modified by another process"}`))
					return
				}
				var body servarr.Series
				_ = json.NewDecoder(r.Body).Decode(&body)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(body)

			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/202":
				getAttempts++
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(servarr.Series{
					ID:     202,
					Title:  "Attack on Titan (Refreshed)",
					TVDBID: 267440,
					Seasons: []servarr.Season{
						{SeasonNumber: 1, Monitored: true},
						{SeasonNumber: 2, Monitored: false},
						{SeasonNumber: 3, Monitored: false},
					},
				})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		target := &servarr.Series{
			ID:     202,
			Title:  "Attack on Titan",
			TVDBID: 267440,
			Seasons: []servarr.Season{
				{SeasonNumber: 1, Monitored: true},
				{SeasonNumber: 2, Monitored: true},
			},
		}

		updated, err := client.UpdateSeries(context.Background(), target)
		if err != nil {
			t.Fatalf("UpdateSeries failed: %v", err)
		}

		if putAttempts != 2 {
			t.Fatalf("expected 2 PUT attempts, got %d", putAttempts)
		}
		if getAttempts != 1 {
			t.Fatalf("expected 1 GET refetch attempt, got %d", getAttempts)
		}
		if len(updated.Seasons) != 3 {
			t.Fatalf("expected 3 seasons from refreshed model, got %d", len(updated.Seasons))
		}
		if !updated.Seasons[1].Monitored {
			t.Fatalf("expected season 2 to remain monitored on retry")
		}
	})

	t.Run("returns ErrConflict when 409 persists after retries", func(t *testing.T) {
		putAttempts := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut && r.URL.Path == "/api/v3/series" {
				putAttempts++
				w.WriteHeader(http.StatusConflict)
				return
			}
			if r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/202" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(servarr.Series{
					ID:     202,
					Title:  "Attack on Titan",
					TVDBID: 267440,
					Seasons: []servarr.Season{
						{SeasonNumber: 1, Monitored: false},
					},
				})
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		target := &servarr.Series{
			ID:     202,
			Title:  "Attack on Titan",
			TVDBID: 267440,
			Seasons: []servarr.Season{
				{SeasonNumber: 1, Monitored: true},
			},
		}

		_, err = client.UpdateSeries(context.Background(), target)
		if !errors.Is(err, servarr.ErrConflict) {
			t.Fatalf("expected ErrConflict, got %v", err)
		}
		if putAttempts != 4 {
			t.Fatalf("expected 4 PUT attempts, got %d", putAttempts)
		}
	})
}

func TestSonarrClient_SearchSeason(t *testing.T) {
	t.Run("dispatches season search command", func(t *testing.T) {
		var received map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/v3/command" {
				t.Fatalf("expected POST /api/v3/command, got %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("X-Api-Key") != "sonarr-key" {
				t.Fatalf("expected X-Api-Key 'sonarr-key', got %q", r.Header.Get("X-Api-Key"))
			}

			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode command body: %v", err)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(servarr.Command{
				ID:     88,
				Name:   "SeasonSearch",
				Status: "queued",
			})
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		cmd, err := client.SearchSeason(context.Background(), 202, 2)
		if err != nil {
			t.Fatalf("SearchSeason failed: %v", err)
		}
		if cmd.ID != 88 {
			t.Fatalf("expected command ID 88, got %d", cmd.ID)
		}
		if cmd.Name != "SeasonSearch" {
			t.Fatalf("expected command name SeasonSearch, got %q", cmd.Name)
		}
		if received["name"] != "SeasonSearch" {
			t.Fatalf("expected name SeasonSearch, got %v", received["name"])
		}
		if int(received["seriesId"].(float64)) != 202 {
			t.Fatalf("expected seriesId 202, got %v", received["seriesId"])
		}
		if int(received["seasonNumber"].(float64)) != 2 {
			t.Fatalf("expected seasonNumber 2, got %v", received["seasonNumber"])
		}
	})
}

func TestSonarrClient_MonitorEpisodes(t *testing.T) {
	t.Run("successfully updates episode monitoring", func(t *testing.T) {
		var received map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut || r.URL.Path != "/api/v3/episode/monitor" {
				t.Fatalf("expected PUT /api/v3/episode/monitor, got %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("X-Api-Key") != "sonarr-key" {
				t.Fatalf("expected X-Api-Key 'sonarr-key', got %q", r.Header.Get("X-Api-Key"))
			}

			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode monitor body: %v", err)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		err = client.MonitorEpisodes(context.Background(), []int{101, 102}, true)
		if err != nil {
			t.Fatalf("MonitorEpisodes failed: %v", err)
		}

		if received["monitored"] != true {
			t.Fatalf("expected monitored true, got %v", received["monitored"])
		}
		ids, ok := received["episodeIds"].([]any)
		if !ok || len(ids) != 2 {
			t.Fatalf("expected 2 episode IDs, got %v", received["episodeIds"])
		}
	})

	t.Run("returns error when episode list is empty", func(t *testing.T) {
		client, err := servarr.NewSonarrClient("http://localhost:8989", "key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		err = client.MonitorEpisodes(context.Background(), []int{}, true)
		if err == nil {
			t.Fatal("expected error for empty episode ids, got nil")
		}
	})
}

func TestSonarrClient_SearchEpisodes(t *testing.T) {
	t.Run("dispatches episode search command", func(t *testing.T) {
		var received map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/v3/command" {
				t.Fatalf("expected POST /api/v3/command, got %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("X-Api-Key") != "sonarr-key" {
				t.Fatalf("expected X-Api-Key 'sonarr-key', got %q", r.Header.Get("X-Api-Key"))
			}

			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode command body: %v", err)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(servarr.Command{
				ID:     99,
				Name:   "EpisodeSearch",
				Status: "queued",
			})
		}))
		defer srv.Close()

		client, err := servarr.NewSonarrClient(srv.URL, "sonarr-key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		cmd, err := client.SearchEpisodes(context.Background(), 101, 102)
		if err != nil {
			t.Fatalf("SearchEpisodes failed: %v", err)
		}
		if cmd.ID != 99 {
			t.Fatalf("expected command ID 99, got %d", cmd.ID)
		}
		if cmd.Name != "EpisodeSearch" {
			t.Fatalf("expected command name EpisodeSearch, got %q", cmd.Name)
		}
		if received["name"] != "EpisodeSearch" {
			t.Fatalf("expected name EpisodeSearch, got %v", received["name"])
		}
		ids, ok := received["episodeIds"].([]any)
		if !ok || len(ids) != 2 {
			t.Fatalf("expected 2 episode IDs, got %v", received["episodeIds"])
		}
	})

	t.Run("returns error when episode ids are empty", func(t *testing.T) {
		client, err := servarr.NewSonarrClient("http://localhost:8989", "key")
		if err != nil {
			t.Fatalf("NewSonarrClient failed: %v", err)
		}

		_, err = client.SearchEpisodes(context.Background())
		if err == nil {
			t.Fatal("expected error for empty episode ids, got nil")
		}
	})
}

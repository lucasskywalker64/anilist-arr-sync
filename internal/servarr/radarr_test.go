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

func TestRadarrClient_LookupMovieByTMDBID(t *testing.T) {
	t.Run("successfully returns movie by tmdb id", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if r.Header.Get("X-Api-Key") != "radarr-key" {
				t.Errorf("expected X-Api-Key 'radarr-key', got %q", r.Header.Get("X-Api-Key"))
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Radarr v3 lookup by tmdbId
			if r.URL.Path == "/api/v3/movie/lookup/tmdb" && r.URL.Query().Get("tmdbId") == "508947" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(servarr.Movie{
					TMDBID:           508947,
					Title:            "Turning Red",
					Year:             2022,
					QualityProfileID: 1,
					Monitored:        true,
				})
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client, err := servarr.NewRadarrClient(srv.URL, "radarr-key")
		if err != nil {
			t.Fatalf("NewRadarrClient failed: %v", err)
		}

		movie, err := client.LookupMovieByTMDBID(context.Background(), 508947)
		if err != nil {
			t.Fatalf("LookupMovieByTMDBID failed: %v", err)
		}
		if movie == nil {
			t.Fatal("expected non-nil movie")
		}
		if movie.TMDBID != 508947 {
			t.Fatalf("expected tmdbId 508947, got %d", movie.TMDBID)
		}
		if movie.Title != "Turning Red" {
			t.Fatalf("expected title 'Turning Red', got %q", movie.Title)
		}
	})

	t.Run("returns ErrNotFound when movie not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client, err := servarr.NewRadarrClient(srv.URL, "radarr-key")
		if err != nil {
			t.Fatalf("NewRadarrClient failed: %v", err)
		}

		movie, err := client.LookupMovieByTMDBID(context.Background(), 999999)
		if !errors.Is(err, servarr.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
		if movie != nil {
			t.Fatalf("expected nil movie, got %+v", movie)
		}
	})

	t.Run("returns ErrNotFound when fallback term search does not match tmdb id", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v3/movie/lookup/tmdb" {
				http.NotFound(w, r)
				return
			}
			if r.URL.Path == "/api/v3/movie/lookup" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]servarr.Movie{
					{TMDBID: 11111, Title: "Some Other Movie"},
				})
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client, err := servarr.NewRadarrClient(srv.URL, "radarr-key")
		if err != nil {
			t.Fatalf("NewRadarrClient failed: %v", err)
		}

		movie, err := client.LookupMovieByTMDBID(context.Background(), 999999)
		if !errors.Is(err, servarr.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
		if movie != nil {
			t.Fatalf("expected nil movie, got %+v", movie)
		}
	})
}

func TestRadarrClient_AddMovie(t *testing.T) {
	t.Run("successfully adds movie to radarr", func(t *testing.T) {
		var received servarr.AddMovieRequest
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/v3/movie" {
				t.Errorf("expected POST /api/v3/movie, got %s %s", r.Method, r.URL.Path)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if r.Header.Get("X-Api-Key") != "radarr-key" {
				t.Errorf("expected X-Api-Key 'radarr-key', got %q", r.Header.Get("X-Api-Key"))
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Errorf("decode body: %v", err)
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(servarr.Movie{
				ID:                  101,
				Title:               received.Title,
				QualityProfileID:    received.QualityProfileID,
				RootFolderPath:      received.RootFolderPath,
				Monitored:           received.Monitored,
				MinimumAvailability: received.MinimumAvailability,
				TMDBID:              received.TMDBID,
				Year:                received.Year,
				Tags:                received.Tags,
			})
		}))
		defer srv.Close()

		client, err := servarr.NewRadarrClient(srv.URL, "radarr-key")
		if err != nil {
			t.Fatalf("NewRadarrClient failed: %v", err)
		}

		req := servarr.AddMovieRequest{
			Title:               "Akira",
			TMDBID:              149,
			Year:                1988,
			QualityProfileID:    1,
			RootFolderPath:      "/movies",
			Monitored:           true,
			MinimumAvailability: "announced",
			Tags:                []int{7},
			AddOptions: &servarr.AddMovieOptions{
				SearchForMovie: true,
			},
		}

		movie, err := client.AddMovie(context.Background(), req)
		if err != nil {
			t.Fatalf("AddMovie failed: %v", err)
		}
		if movie.ID != 101 {
			t.Fatalf("expected created movie ID 101, got %d", movie.ID)
		}
		if movie.Title != "Akira" {
			t.Fatalf("expected title 'Akira', got %q", movie.Title)
		}
		if received.TMDBID != 149 {
			t.Fatalf("expected TMDBID 149, got %d", received.TMDBID)
		}
	})
}

func TestRadarrClient_SearchMovies(t *testing.T) {
	t.Run("dispatches movies search command", func(t *testing.T) {
		var received map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/v3/command" {
				t.Errorf("expected POST /api/v3/command, got %s %s", r.Method, r.URL.Path)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if r.Header.Get("X-Api-Key") != "radarr-key" {
				t.Errorf("expected X-Api-Key 'radarr-key', got %q", r.Header.Get("X-Api-Key"))
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Errorf("decode command body: %v", err)
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(servarr.Command{
				ID:     55,
				Name:   "MoviesSearch",
				Status: "queued",
			})
		}))
		defer srv.Close()

		client, err := servarr.NewRadarrClient(srv.URL, "radarr-key")
		if err != nil {
			t.Fatalf("NewRadarrClient failed: %v", err)
		}

		cmd, err := client.SearchMovies(context.Background(), 101, 102)
		if err != nil {
			t.Fatalf("SearchMovies failed: %v", err)
		}
		if cmd.ID != 55 {
			t.Fatalf("expected command ID 55, got %d", cmd.ID)
		}
		if cmd.Name != "MoviesSearch" {
			t.Fatalf("expected command name MoviesSearch, got %q", cmd.Name)
		}
		if received["name"] != "MoviesSearch" {
			t.Fatalf("expected name MoviesSearch, got %v", received["name"])
		}
		movieIDs, ok := received["movieIds"].([]any)
		if !ok || len(movieIDs) != 2 {
			t.Fatalf("expected 2 movie IDs, got %v", received["movieIds"])
		}
	})
}

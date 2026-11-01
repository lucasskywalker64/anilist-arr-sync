package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func TestLibraryEndpoints(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}

	err = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, execErr := tx.ExecContext(ctx, `
			INSERT INTO synced_media (
				anilist_id, media_type, format, title_romaji, title_english,
				release_year, poster_url, anilist_status, target_service,
				tvdb_id, tmdb_id, arr_title, monitored_status, seasons
			) VALUES
			(1, 'SERIES', 'TV', 'Shingeki no Kyojin', 'Attack on Titan', 2013, 'https://example.com/aot.jpg', 'CURRENT', 'SONARR', 267440, NULL, 'Attack on Titan', 'MONITORED', '1-4'),
			(2, 'SERIES', 'TV', 'Sousou no Frieren', 'Frieren: Beyond Journey''s End', 2023, 'https://example.com/frieren.jpg', 'COMPLETED', 'SONARR', 408240, NULL, 'Frieren', 'MONITORED', '1'),
			(3, 'MOVIE', 'MOVIE', 'Sen to Chihiro no Kamikakushi', 'Spirited Away', 2001, 'https://example.com/spirited.jpg', 'COMPLETED', 'RADARR', NULL, 129, 'Spirited Away', 'MONITORED', NULL),
			(4, 'MOVIE', 'MOVIE', 'ONE PIECE FILM RED', 'One Piece Film Red', 2022, 'https://example.com/opred.jpg', 'PLANNING', 'RADARR', NULL, 901362, 'One Piece Film Red', 'UNMONITORED', NULL),
			(5, 'SERIES', 'TV', 'Chainsaw Man', 'Chainsaw Man', 2022, 'https://example.com/csm.jpg', 'PLANNING', 'NONE', NULL, NULL, NULL, 'PENDING_REVIEW', NULL);
		`)
		return execErr
	})
	if err != nil {
		t.Fatalf("failed to seed test data: %v", err)
	}

	cfg := config.NewDefault()
	cfg.AuthenticationMethod = "Forms"
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	s := NewServer(cfg, "", db, nil, nil)
	handler := s.Handler()

	newReq := func(target string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		r.RemoteAddr = "127.0.0.1:12345"
		return r
	}

	t.Run("GET /api/v1/library returns all items", func(t *testing.T) {
		req := newReq("/api/v1/library")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var items []SyncedMediaItem
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("failed to parse json response: %v", err)
		}
		if len(items) != 5 {
			t.Fatalf("expected 5 items, got %d", len(items))
		}
	})

	t.Run("GET /api/v1/library with service filter", func(t *testing.T) {
		req := newReq("/api/v1/library?service=SONARR")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var items []SyncedMediaItem
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("failed to parse json response: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 Sonarr items, got %d", len(items))
		}
		for _, it := range items {
			if it.TargetService != "SONARR" {
				t.Errorf("expected target_service SONARR, got %s", it.TargetService)
			}
		}
	})

	t.Run("GET /api/v1/library with status filter", func(t *testing.T) {
		req := newReq("/api/v1/library?status=PLANNING")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var items []SyncedMediaItem
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("failed to parse json response: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 Planning items, got %d", len(items))
		}
	})

	t.Run("GET /api/v1/library with search term", func(t *testing.T) {
		req := newReq("/api/v1/library?search=Frieren")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var items []SyncedMediaItem
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("failed to parse json response: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 search result, got %d", len(items))
		}
		if items[0].AniListID != 2 {
			t.Errorf("expected AniList ID 2, got %d", items[0].AniListID)
		}
	})

	t.Run("GET /api/v1/library/stats returns aggregate metrics", func(t *testing.T) {
		req := newReq("/api/v1/library/stats")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var stats LibraryStats
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("failed to parse json response: %v", err)
		}

		if stats.Total != 5 {
			t.Errorf("expected total 5, got %d", stats.Total)
		}
		if stats.Monitored != 3 {
			t.Errorf("expected monitored 3, got %d", stats.Monitored)
		}
		if stats.Unmonitored != 1 {
			t.Errorf("expected unmonitored 1, got %d", stats.Unmonitored)
		}
		if stats.SonarrCount != 2 {
			t.Errorf("expected sonarrCount 2, got %d", stats.SonarrCount)
		}
		if stats.RadarrCount != 2 {
			t.Errorf("expected radarrCount 2, got %d", stats.RadarrCount)
		}
		if stats.WatchingCount != 1 {
			t.Errorf("expected watchingCount 1, got %d", stats.WatchingCount)
		}
		if stats.PlanningCount != 2 {
			t.Errorf("expected planningCount 2, got %d", stats.PlanningCount)
		}
		if stats.CompletedCount != 2 {
			t.Errorf("expected completedCount 2, got %d", stats.CompletedCount)
		}
	})
}

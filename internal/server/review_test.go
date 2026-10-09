package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func TestReviewQueueEndpoints(t *testing.T) {
	db := newTestStorage(t)
	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "test-api-key"

	srv := NewServer(cfg, "", db, nil, nil)
	handler := srv.Handler()

	// Seed review queue
	var item1ID, item2ID int64
	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		res1, err := tx.ExecContext(ctx, `
			INSERT INTO review_queue (anilist_id, media_type, title_romaji, title_english, release_year, poster_url, candidates_json, reason, status)
			VALUES (101, 'SERIES', 'Frieren: Beyond Journeys End', 'Frieren', 2023, 'https://img.test/101.jpg', '[]', 'UNMAPPED', 'PENDING');
		`)
		if err != nil {
			return err
		}
		item1ID, _ = res1.LastInsertId()

		res2, err := tx.ExecContext(ctx, `
			INSERT INTO review_queue (anilist_id, media_type, title_romaji, title_english, release_year, poster_url, candidates_json, reason, status)
			VALUES (102, 'MOVIE', 'Spirited Away', 'Spirited Away', 2001, 'https://img.test/102.jpg', '[]', 'LOW_CONFIDENCE', 'PENDING');
		`)
		if err != nil {
			return err
		}
		item2ID, _ = res2.LastInsertId()
		return nil
	})
	if err != nil {
		t.Fatalf("failed to seed review queue: %v", err)
	}

	// 1. GET /api/v1/review returns pending items
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected GET /api/v1/review to return 200, got %d", rec.Code)
		}

		var items []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("failed to parse review queue json: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 pending review items, got %d", len(items))
		}
	}

	// 2. POST /api/v1/review/{id}/resolve resolves item 1 and inserts into mapping_overrides
	{
		resolvePayload, _ := json.Marshal(map[string]any{
			"mediaType":     "SERIES",
			"tvdbId":        414833,
			"tmdbId":        0,
			"seasons":       "1",
			"titleOverride": "Frieren",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/review/1/resolve", bytes.NewReader(resolvePayload))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", "test-api-key")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected resolve to return 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify mapping_overrides table has entry
		err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
			var mediaType, seasons string
			var tvdbID int
			err := q.QueryRowContext(ctx, "SELECT media_type, tvdb_id, seasons FROM mapping_overrides WHERE anilist_id = 101;").
				Scan(&mediaType, &tvdbID, &seasons)
			if err != nil {
				return err
			}
			if mediaType != "SERIES" || tvdbID != 414833 || seasons != "1" {
				t.Errorf("unexpected mapping override: mediaType=%s, tvdbID=%d, seasons=%s", mediaType, tvdbID, seasons)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("failed to query mapping_overrides: %v", err)
		}

		// Verify review_queue item status is RESOLVED
		err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
			var status string
			err := q.QueryRowContext(ctx, "SELECT status FROM review_queue WHERE id = ?;", item1ID).Scan(&status)
			if err != nil {
				return err
			}
			if status != "RESOLVED" {
				t.Errorf("expected review queue status RESOLVED, got %s", status)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("failed to query review_queue: %v", err)
		}
	}

	// 3. POST /api/v1/review/{id}/ignore inserts into ignored_titles and resolves queue item
	{
		ignorePayload, _ := json.Marshal(map[string]string{
			"reason": "Not interested in watching",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/review/2/ignore", bytes.NewReader(ignorePayload))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", "test-api-key")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected ignore to return 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify ignored_titles table
		err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
			var title, reason string
			err := q.QueryRowContext(ctx, "SELECT title_romaji, reason FROM ignored_titles WHERE anilist_id = 102;").
				Scan(&title, &reason)
			if err != nil {
				return err
			}
			if title != "Spirited Away" || reason != "Not interested in watching" {
				t.Errorf("unexpected ignored entry: title=%s, reason=%s", title, reason)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("failed to query ignored_titles: %v", err)
		}

		// Verify review_queue item status is RESOLVED
		err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
			var status string
			err := q.QueryRowContext(ctx, "SELECT status FROM review_queue WHERE id = ?;", item2ID).Scan(&status)
			if err != nil {
				return err
			}
			if status != "RESOLVED" {
				t.Errorf("expected review queue status RESOLVED, got %s", status)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("failed to query review_queue: %v", err)
		}
	}

	// 4. GET /api/v1/review should now return 0 items
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var items []map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &items)
		if len(items) != 0 {
			t.Errorf("expected 0 pending items, got %d", len(items))
		}
	}
}

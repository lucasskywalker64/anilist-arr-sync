package resolver_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/resolver"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func setupTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("setupTestDB: storage.Open failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestResolve_Tier1MappingOverrides(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestDB(t)

	// Seed Tier 1 mapping overrides
	err := db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		// Series override with TVDB ID
		_, err := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, tmdb_id, seasons, title_override)
			VALUES (100, 'SERIES', 81472, NULL, '1', 'Custom Title');
		`)
		if err != nil {
			return err
		}

		// Movie override with TMDB ID
		_, err = tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, tmdb_id, seasons, title_override)
			VALUES (200, 'MOVIE', NULL, 500123, '1', NULL);
		`)
		return err
	})
	if err != nil {
		t.Fatalf("failed to seed mapping_overrides: %v", err)
	}

	r := resolver.New(db)

	t.Run("resolves series override against TVDB ID", func(t *testing.T) {
		media := anilist.Media{
			ID:     100,
			Format: "TV",
			Title: anilist.MediaTitle{
				Romaji: "Shingeki no Kyojin",
			},
		}

		res, err := r.Resolve(ctx, media)
		if err != nil {
			t.Fatalf("Resolve returned unexpected error: %v", err)
		}
		if !res.Resolved {
			t.Fatalf("expected Resolved to be true, got false")
		}
		if res.Tier != resolver.TierOverride {
			t.Errorf("expected TierOverride, got %v", res.Tier)
		}
		if res.TargetService != "SONARR" {
			t.Errorf("expected TargetService SONARR, got %q", res.TargetService)
		}
		if res.TargetID != 81472 {
			t.Errorf("expected TargetID 81472, got %d", res.TargetID)
		}
		if res.TitleOverride != "Custom Title" {
			t.Errorf("expected TitleOverride 'Custom Title', got %q", res.TitleOverride)
		}
	})

	t.Run("resolves movie override against TMDB ID", func(t *testing.T) {
		media := anilist.Media{
			ID:     200,
			Format: "MOVIE",
			Title: anilist.MediaTitle{
				Romaji: "Kimi no Na wa.",
			},
		}

		res, err := r.Resolve(ctx, media)
		if err != nil {
			t.Fatalf("Resolve returned unexpected error: %v", err)
		}
		if !res.Resolved {
			t.Fatalf("expected Resolved to be true, got false")
		}
		if res.Tier != resolver.TierOverride {
			t.Errorf("expected TierOverride, got %v", res.Tier)
		}
		if res.TargetService != "RADARR" {
			t.Errorf("expected TargetService RADARR, got %q", res.TargetService)
		}
		if res.TargetID != 500123 {
			t.Errorf("expected TargetID 500123, got %d", res.TargetID)
		}
	})
}

func TestResolve_Tier2FribbEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestDB(t)

	// Seed Tier 2 Fribb entries
	err := db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		// Series in Fribb with TVDB ID and season
		_, err := tx.ExecContext(ctx, `
			INSERT INTO fribb_entries (anilist_id, tvdb_id, tmdb_id, mal_id, media_type, tvdb_season)
			VALUES (300, 305288, NULL, 31240, 'TV', 2);
		`)
		if err != nil {
			return err
		}

		// Movie in Fribb with TMDB ID
		_, err = tx.ExecContext(ctx, `
			INSERT INTO fribb_entries (anilist_id, tvdb_id, tmdb_id, mal_id, media_type, tvdb_season)
			VALUES (400, NULL, 635302, 38883, 'Movie', 1);
		`)
		return err
	})
	if err != nil {
		t.Fatalf("failed to seed fribb_entries: %v", err)
	}

	r := resolver.New(db)

	t.Run("resolves series against Fribb TVDB ID and season", func(t *testing.T) {
		media := anilist.Media{
			ID:     300,
			Format: "TV",
			Title: anilist.MediaTitle{
				Romaji: "Re:Zero kara Hajimeru Isekai Seikatsu 2nd Season",
			},
		}

		res, err := r.Resolve(ctx, media)
		if err != nil {
			t.Fatalf("Resolve returned unexpected error: %v", err)
		}
		if !res.Resolved {
			t.Fatalf("expected Resolved to be true, got false")
		}
		if res.Tier != resolver.TierFribb {
			t.Errorf("expected TierFribb, got %v", res.Tier)
		}
		if res.TargetService != "SONARR" {
			t.Errorf("expected TargetService SONARR, got %q", res.TargetService)
		}
		if res.TargetID != 305288 {
			t.Errorf("expected TargetID 305288, got %d", res.TargetID)
		}
		if res.TVDBSeason != 2 {
			t.Errorf("expected TVDBSeason 2, got %d", res.TVDBSeason)
		}
	})

	t.Run("resolves movie against Fribb TMDB ID", func(t *testing.T) {
		media := anilist.Media{
			ID:     400,
			Format: "MOVIE",
			Title: anilist.MediaTitle{
				Romaji: "Kimetsu no Yaiba: Mugen Ressha-hen",
			},
		}

		res, err := r.Resolve(ctx, media)
		if err != nil {
			t.Fatalf("Resolve returned unexpected error: %v", err)
		}
		if !res.Resolved {
			t.Fatalf("expected Resolved to be true, got false")
		}
		if res.Tier != resolver.TierFribb {
			t.Errorf("expected TierFribb, got %v", res.Tier)
		}
		if res.TargetService != "RADARR" {
			t.Errorf("expected TargetService RADARR, got %q", res.TargetService)
		}
		if res.TargetID != 635302 {
			t.Errorf("expected TargetID 635302, got %d", res.TargetID)
		}
	})
}

func TestResolve_NullOrMissingTargetIDsUnresolved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestDB(t)

	err := db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		// Tier 1 entry with NULL tvdb_id
		_, err := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, tmdb_id)
			VALUES (500, 'SERIES', NULL, NULL);
		`)
		if err != nil {
			return err
		}

		// Tier 2 entry with valid TVDB ID for 500 (fallthrough case)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO fribb_entries (anilist_id, tvdb_id, tmdb_id, mal_id, media_type, tvdb_season)
			VALUES (500, 99999, NULL, 12345, 'TV', 1);
		`)
		if err != nil {
			return err
		}

		// Tier 2 entry with NULL target IDs
		_, err = tx.ExecContext(ctx, `
			INSERT INTO fribb_entries (anilist_id, tvdb_id, tmdb_id, mal_id, media_type, tvdb_season)
			VALUES (600, NULL, NULL, 54321, 'TV', 1);
		`)
		return err
	})
	if err != nil {
		t.Fatalf("failed to seed test data: %v", err)
	}

	r := resolver.New(db)

	t.Run("falls through to Tier 2 when Tier 1 target ID is null", func(t *testing.T) {
		media := anilist.Media{
			ID:     500,
			Format: "TV",
			Title: anilist.MediaTitle{
				Romaji: "Fallthrough Series",
			},
		}

		res, err := r.Resolve(ctx, media)
		if err != nil {
			t.Fatalf("Resolve returned unexpected error: %v", err)
		}
		if !res.Resolved {
			t.Fatalf("expected Resolved to be true, got false")
		}
		if res.Tier != resolver.TierFribb {
			t.Errorf("expected TierFribb, got %v", res.Tier)
		}
		if res.TargetID != 99999 {
			t.Errorf("expected TargetID 99999, got %d", res.TargetID)
		}
	})

	t.Run("treats entry with null target service ID in Fribb as unresolved", func(t *testing.T) {
		media := anilist.Media{
			ID:     600,
			Format: "TV",
			Title: anilist.MediaTitle{
				Romaji: "Unresolved Series",
			},
		}

		res, err := r.Resolve(ctx, media)
		if err != nil {
			t.Fatalf("Resolve returned unexpected error: %v", err)
		}
		if res.Resolved || !res.Diverted {
			t.Fatalf("expected unresolved and diverted, got Resolved=%v Diverted=%v", res.Resolved, res.Diverted)
		}
	})
}

func TestResolve_DivertsUnmappedToReviewQueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestDB(t)
	r := resolver.New(db)

	year := 2022
	media := anilist.Media{
		ID:     700,
		Format: "TV",
		Title: anilist.MediaTitle{
			Romaji:  "Bocchi the Rock!",
			English: "BOCCHI THE ROCK!",
		},
		StartDate: anilist.Date{
			Year: &year,
		},
		CoverImage: anilist.MediaCoverImage{
			Large: "https://example.com/bocchi.jpg",
		},
	}

	res, err := r.Resolve(ctx, media)
	if err != nil {
		t.Fatalf("Resolve returned unexpected error: %v", err)
	}
	if res.Resolved {
		t.Fatalf("expected Resolved to be false for unmapped entry, got true")
	}
	if !res.Diverted {
		t.Fatalf("expected Diverted to be true for unmapped entry, got false")
	}
	if res.DivertReason != "UNMAPPED" {
		t.Errorf("expected DivertReason UNMAPPED, got %q", res.DivertReason)
	}

	// Verify row in review_queue
	var count int
	var anilistID, releaseYear int
	var mediaType, titleRomaji, titleEnglish, posterURL, candidatesJSON, reason, status string
	err = db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		row := q.QueryRowContext(ctx, `
			SELECT anilist_id, media_type, title_romaji, title_english, release_year, poster_url, candidates_json, reason, status
			FROM review_queue
			WHERE anilist_id = 700;
		`)
		return row.Scan(&anilistID, &mediaType, &titleRomaji, &titleEnglish, &releaseYear, &posterURL, &candidatesJSON, &reason, &status)
	})
	if err != nil {
		t.Fatalf("query review_queue failed: %v", err)
	}

	if anilistID != 700 {
		t.Errorf("expected anilist_id 700, got %d", anilistID)
	}
	if mediaType != "SERIES" {
		t.Errorf("expected media_type SERIES, got %q", mediaType)
	}
	if titleRomaji != "Bocchi the Rock!" {
		t.Errorf("expected title_romaji 'Bocchi the Rock!', got %q", titleRomaji)
	}
	if titleEnglish != "BOCCHI THE ROCK!" {
		t.Errorf("expected title_english 'BOCCHI THE ROCK!', got %q", titleEnglish)
	}
	if releaseYear != 2022 {
		t.Errorf("expected release_year 2022, got %d", releaseYear)
	}
	if posterURL != "https://example.com/bocchi.jpg" {
		t.Errorf("expected poster_url 'https://example.com/bocchi.jpg', got %q", posterURL)
	}
	if candidatesJSON != "[]" {
		t.Errorf("expected candidates_json '[]', got %q", candidatesJSON)
	}
	if reason != "UNMAPPED" {
		t.Errorf("expected reason UNMAPPED, got %q", reason)
	}
	if status != "PENDING" {
		t.Errorf("expected status PENDING, got %q", status)
	}

	_ = count
}

func TestResolve_DivertsUnmappedSpecialToReviewQueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestDB(t)
	r := resolver.New(db)

	year := 2021
	media := anilist.Media{
		ID:     800,
		Format: "SPECIAL",
		Title: anilist.MediaTitle{
			Romaji: "Attack on Titan: Chronicle",
		},
		StartDate: anilist.Date{
			Year: &year,
		},
		CoverImage: anilist.MediaCoverImage{
			Color: "#ff0000",
		},
	}

	res, err := r.Resolve(ctx, media)
	if err != nil {
		t.Fatalf("Resolve returned unexpected error: %v", err)
	}
	if res.Resolved {
		t.Fatalf("expected Resolved false, got true")
	}
	if !res.Diverted {
		t.Fatalf("expected Diverted true, got false")
	}
	if res.DivertReason != "SPECIAL_WITHOUT_MAPPING" {
		t.Errorf("expected DivertReason SPECIAL_WITHOUT_MAPPING, got %q", res.DivertReason)
	}

	var reason, status string
	err = db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		row := q.QueryRowContext(ctx, `
			SELECT reason, status
			FROM review_queue
			WHERE anilist_id = 800;
		`)
		return row.Scan(&reason, &status)
	})
	if err != nil {
		t.Fatalf("query review_queue failed: %v", err)
	}
	if reason != "SPECIAL_WITHOUT_MAPPING" {
		t.Errorf("expected reason SPECIAL_WITHOUT_MAPPING, got %q", reason)
	}
	if status != "PENDING" {
		t.Errorf("expected status PENDING, got %q", status)
	}
}

func TestResolve_DeduplicatesPendingReviewQueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestDB(t)
	r := resolver.New(db)

	media := anilist.Media{
		ID:     900,
		Format: "TV",
		Title: anilist.MediaTitle{
			Romaji: "Odd Taxi",
		},
	}

	// First call diverts to review_queue
	res1, err := r.Resolve(ctx, media)
	if err != nil {
		t.Fatalf("first Resolve failed: %v", err)
	}
	if !res1.Diverted {
		t.Fatalf("expected res1 to be diverted")
	}

	// Second call should also be handled without duplicate insertion
	res2, err := r.Resolve(ctx, media)
	if err != nil {
		t.Fatalf("second Resolve failed: %v", err)
	}
	if !res2.Diverted {
		t.Fatalf("expected res2 to be diverted")
	}

	// Verify only 1 pending record exists
	var count int
	err = db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM review_queue
			WHERE anilist_id = 900 AND status = 'PENDING';
		`).Scan(&count)
	})
	if err != nil {
		t.Fatalf("query review_queue count failed: %v", err)
	}

	if count != 1 {
		t.Errorf("expected exactly 1 pending review_queue record, got %d", count)
	}
}

func TestResolve_SpecialWithMappings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestDB(t)

	err := db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		// Tier 1 override for special
		_, err := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (anilist_id, media_type, tvdb_id, seasons)
			VALUES (1001, 'SERIES', 44444, '0');
		`)
		if err != nil {
			return err
		}

		// Tier 2 Fribb entry for special
		_, err = tx.ExecContext(ctx, `
			INSERT INTO fribb_entries (anilist_id, tvdb_id, media_type, tvdb_season)
			VALUES (1002, 55555, 'Special', 0);
		`)
		return err
	})
	if err != nil {
		t.Fatalf("failed to seed test data: %v", err)
	}

	r := resolver.New(db)

	t.Run("resolves special with Tier 1 override", func(t *testing.T) {
		media := anilist.Media{
			ID:     1001,
			Format: "SPECIAL",
		}
		res, err := r.Resolve(ctx, media)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if !res.Resolved {
			t.Errorf("expected Resolved true, got false")
		}
		if res.Tier != resolver.TierOverride {
			t.Errorf("expected TierOverride, got %v", res.Tier)
		}
		if res.TargetID != 44444 {
			t.Errorf("expected TargetID 44444, got %d", res.TargetID)
		}
		if res.TVDBSeason != 0 {
			t.Errorf("expected TVDBSeason 0, got %d", res.TVDBSeason)
		}
	})

	t.Run("bypasses Tier 2 Fribb entry for special and diverts to review queue", func(t *testing.T) {
		media := anilist.Media{
			ID:     1002,
			Format: "SPECIAL",
			Title: anilist.MediaTitle{
				Romaji: "Special In Fribb",
			},
		}
		res, err := r.Resolve(ctx, media)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if res.Resolved {
			t.Errorf("expected Resolved false for special in Fribb, got true")
		}
		if !res.Diverted {
			t.Errorf("expected Diverted true for special in Fribb, got false")
		}
		if res.DivertReason != "SPECIAL_WITHOUT_MAPPING" {
			t.Errorf("expected DivertReason SPECIAL_WITHOUT_MAPPING, got %q", res.DivertReason)
		}

		var count int
		err = db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
			return q.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM review_queue
				WHERE anilist_id = 1002 AND reason = 'SPECIAL_WITHOUT_MAPPING' AND status = 'PENDING';
			`).Scan(&count)
		})
		if err != nil {
			t.Fatalf("query review_queue failed: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1 record in review_queue, got %d", count)
		}
	})
}

func TestResolve_DivertsUnmappedMovie(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestDB(t)
	r := resolver.New(db)

	media := anilist.Media{
		ID:     1100,
		Format: "MOVIE",
		Title: anilist.MediaTitle{
			Romaji: "Suzume no Tojimari",
		},
	}

	res, err := r.Resolve(ctx, media)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if res.Resolved {
		t.Fatalf("expected Resolved false, got true")
	}
	if !res.Diverted {
		t.Fatalf("expected Diverted true, got false")
	}
	if res.DivertReason != "UNMAPPED" {
		t.Errorf("expected DivertReason UNMAPPED, got %q", res.DivertReason)
	}

	var mediaType, reason, status string
	var englishTitle sql.NullString
	var releaseYear sql.NullInt64
	err = db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		row := q.QueryRowContext(ctx, `
			SELECT media_type, title_english, release_year, reason, status
			FROM review_queue
			WHERE anilist_id = 1100;
		`)
		return row.Scan(&mediaType, &englishTitle, &releaseYear, &reason, &status)
	})
	if err != nil {
		t.Fatalf("query review_queue failed: %v", err)
	}

	if mediaType != "MOVIE" {
		t.Errorf("expected media_type MOVIE, got %q", mediaType)
	}
	if englishTitle.Valid {
		t.Errorf("expected null title_english, got %q", englishTitle.String)
	}
	if releaseYear.Valid {
		t.Errorf("expected null release_year, got %d", releaseYear.Int64)
	}
	if reason != "UNMAPPED" {
		t.Errorf("expected reason UNMAPPED, got %q", reason)
	}
	if status != "PENDING" {
		t.Errorf("expected status PENDING, got %q", status)
	}
}

func TestResolve_DiscardFormats(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestDB(t)
	r := resolver.New(db)

	formats := []string{"MUSIC", "MANGA", "NOVEL", "ONE_SHOT"}
	for _, fmtStr := range formats {
		media := anilist.Media{
			ID:     1200,
			Format: fmtStr,
			Title: anilist.MediaTitle{
				Romaji: "Discard Entry",
			},
		}

		res, err := r.Resolve(ctx, media)
		if err != nil {
			t.Fatalf("Resolve(%s) failed: %v", fmtStr, err)
		}
		if res.Resolved {
			t.Errorf("expected Resolved false for %s, got true", fmtStr)
		}
		if res.Diverted {
			t.Errorf("expected Diverted false for %s, got true", fmtStr)
		}
	}

	// Verify no entries added to review_queue
	var count int
	err := db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM review_queue;").Scan(&count)
	})
	if err != nil {
		t.Fatalf("query review_queue count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 review_queue entries, got %d", count)
	}
}

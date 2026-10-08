// Package resolver provides a two-tier entity resolution pipeline linking AniList
// entries to TVDB and TMDb identifiers, diverting unmapped entries to the review queue.
package resolver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/domain/router"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

// Tier identifies which tier resolved a media entry.
type Tier int

const (
	// TierNone indicates the entry was not resolved by either tier.
	TierNone Tier = iota
	// TierOverride indicates resolution through Tier 1 local database overrides.
	TierOverride
	// TierFribb indicates resolution through Tier 2 cached Fribb community mappings.
	TierFribb
)

// String returns the string representation of the resolution tier.
func (t Tier) String() string {
	switch t {
	case TierOverride:
		return "OVERRIDE"
	case TierFribb:
		return "FRIBB"
	default:
		return "NONE"
	}
}

// Result encapsulates the outcome of resolving an AniList entry.
type Result struct {
	Resolved      bool
	Tier          Tier
	TargetService string
	TargetID      int
	TVDBSeason    int
	TitleOverride string
	Diverted      bool
	DivertReason  string
}

// Resolver coordinates two-tier entity resolution and review queue diversion.
type Resolver struct {
	db *storage.DB
}

// New creates a Resolver instance backed by the given database.
func New(db *storage.DB) *Resolver {
	return &Resolver{db: db}
}

// Resolve attempts to resolve an AniList media item across Tier 1 (mapping_overrides)
// and Tier 2 (fribb_entries), diverting unmapped entries into the review queue.
func (r *Resolver) Resolve(ctx context.Context, media anilist.Media) (*Result, error) {
	normalizedFormat := router.MediaFormat(strings.ToUpper(strings.TrimSpace(media.Format)))
	decision := router.Route(normalizedFormat)
	if decision == router.RouteDiscard {
		return &Result{
			Resolved: false,
			Diverted: false,
		}, nil
	}

	var overrideMediaType sql.NullString
	var tvdbID, tmdbID sql.NullInt64
	var seasons, titleOverride sql.NullString

	err := r.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		row := q.QueryRowContext(ctx, `
			SELECT media_type, tvdb_id, tmdb_id, seasons, title_override
			FROM mapping_overrides
			WHERE anilist_id = ?;
		`, media.ID)
		return row.Scan(&overrideMediaType, &tvdbID, &tmdbID, &seasons, &titleOverride)
	})

	if err == nil {
		seasonNum := 1
		if seasons.Valid && seasons.String != "" {
			parts := strings.FieldsFunc(seasons.String, func(c rune) bool {
				return c == ',' || c == ' ' || c == ';'
			})
			if len(parts) > 0 {
				if parsed, parseErr := strconv.Atoi(parts[0]); parseErr == nil && parsed > 0 {
					seasonNum = parsed
				}
			}
		}

		switch overrideMediaType.String {
		case "MOVIE":
			if tmdbID.Valid && tmdbID.Int64 > 0 {
				return &Result{
					Resolved:      true,
					Tier:          TierOverride,
					TargetService: "RADARR",
					TargetID:      int(tmdbID.Int64),
					TVDBSeason:    seasonNum,
					TitleOverride: titleOverride.String,
				}, nil
			}
		case "SERIES":
			if tvdbID.Valid && tvdbID.Int64 > 0 {
				return &Result{
					Resolved:      true,
					Tier:          TierOverride,
					TargetService: "SONARR",
					TargetID:      int(tvdbID.Int64),
					TVDBSeason:    seasonNum,
					TitleOverride: titleOverride.String,
				}, nil
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("resolver: query mapping_overrides failed: %w", err)
	}

	// Tier 2: Fribb community mappings
	var fribbTVDBID, fribbTMDBID sql.NullInt64
	var fribbSeason sql.NullInt64

	err = r.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		row := q.QueryRowContext(ctx, `
			SELECT tvdb_id, tmdb_id, tvdb_season
			FROM fribb_entries
			WHERE anilist_id = ?;
		`, media.ID)
		return row.Scan(&fribbTVDBID, &fribbTMDBID, &fribbSeason)
	})

	if err == nil {
		season := 1
		if fribbSeason.Valid && fribbSeason.Int64 > 0 {
			season = int(fribbSeason.Int64)
		}

		if decision == router.RouteRadarr {
			if fribbTMDBID.Valid && fribbTMDBID.Int64 > 0 {
				return &Result{
					Resolved:      true,
					Tier:          TierFribb,
					TargetService: "RADARR",
					TargetID:      int(fribbTMDBID.Int64),
					TVDBSeason:    season,
				}, nil
			}
		} else {
			if fribbTVDBID.Valid && fribbTVDBID.Int64 > 0 {
				return &Result{
					Resolved:      true,
					Tier:          TierFribb,
					TargetService: "SONARR",
					TargetID:      int(fribbTVDBID.Int64),
					TVDBSeason:    season,
				}, nil
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("resolver: query fribb_entries failed: %w", err)
	}

	// Unmapped: divert to review_queue
	queueMediaType := "SERIES"
	if decision == router.RouteRadarr {
		queueMediaType = "MOVIE"
	}

	reason := "UNMAPPED"
	if normalizedFormat == router.FormatSpecial {
		reason = "SPECIAL_WITHOUT_MAPPING"
	}

	titleRomaji := media.Title.Romaji
	if titleRomaji == "" {
		titleRomaji = media.Title.UserPreferred
	}
	if titleRomaji == "" {
		titleRomaji = "Unknown"
	}

	var titleEnglish *string
	if media.Title.English != "" {
		titleEnglish = &media.Title.English
	}

	posterURL := media.CoverImageURL()

	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var exists int
		checkErr := tx.QueryRowContext(ctx, `
			SELECT 1 FROM review_queue
			WHERE anilist_id = ? AND status = 'PENDING'
			LIMIT 1;
		`, media.ID).Scan(&exists)
		if checkErr == nil {
			return nil
		} else if !errors.Is(checkErr, sql.ErrNoRows) {
			return checkErr
		}

		_, writeErr := tx.ExecContext(ctx, `
			INSERT INTO review_queue (
				anilist_id, media_type, title_romaji, title_english,
				release_year, poster_url, candidates_json, reason, status
			)
			VALUES (?, ?, ?, ?, ?, ?, '[]', ?, 'PENDING');
		`, media.ID, queueMediaType, titleRomaji, titleEnglish, media.StartDate.Year, posterURL, reason)
		return writeErr
	})
	if err != nil {
		return nil, fmt.Errorf("resolver: failed to divert to review_queue: %w", err)
	}

	return &Result{
		Resolved:     false,
		Diverted:     true,
		DivertReason: reason,
	}, nil
}

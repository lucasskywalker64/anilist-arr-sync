package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

// SyncedMediaItem represents an anime or movie entry in synced_media.
type SyncedMediaItem struct {
	AniListID       int       `json:"anilistId"`
	MediaType       string    `json:"mediaType"`
	Format          string    `json:"format"`
	TitleRomaji     string    `json:"titleRomaji"`
	TitleEnglish    *string   `json:"titleEnglish,omitempty"`
	ReleaseYear     *int      `json:"releaseYear,omitempty"`
	PosterURL       *string   `json:"posterUrl,omitempty"`
	AniListStatus   string    `json:"anilistStatus"`
	TargetService   string    `json:"targetService"`
	TVDBID          *int      `json:"tvdbId,omitempty"`
	TMDBID          *int      `json:"tmdbId,omitempty"`
	ArrTitle        *string   `json:"arrTitle,omitempty"`
	MonitoredStatus string    `json:"monitoredStatus"`
	Seasons         *string   `json:"seasons,omitempty"`
	LastSyncedAt    time.Time `json:"lastSyncedAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// LibraryStats represents aggregate metrics for the library.
type LibraryStats struct {
	Total          int `json:"total"`
	Monitored      int `json:"monitored"`
	Unmonitored    int `json:"unmonitored"`
	SonarrCount    int `json:"sonarrCount"`
	RadarrCount    int `json:"radarrCount"`
	WatchingCount  int `json:"watchingCount"`
	PlanningCount  int `json:"planningCount"`
	CompletedCount int `json:"completedCount"`
}

// handleListLibrary returns filtered and sorted items from synced_media.
func (s *Server) handleListLibrary(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	qStatus := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	qService := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("service")))
	qMonitored := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("monitored")))
	qSearch := strings.TrimSpace(r.URL.Query().Get("search"))
	qSort := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
	qOrder := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("order")))

	queryBase := `
		SELECT anilist_id, media_type, COALESCE(format, ''), title_romaji, title_english,
		       release_year, poster_url, anilist_status, COALESCE(target_service, 'NONE'),
		       tvdb_id, tmdb_id, arr_title, monitored_status, seasons,
		       last_synced_at, updated_at
		FROM synced_media
	`

	var whereClauses []string
	var args []any

	if qStatus != "" && qStatus != "ALL" {
		whereClauses = append(whereClauses, "anilist_status = ?")
		args = append(args, qStatus)
	}

	if qService != "" && qService != "ALL" {
		whereClauses = append(whereClauses, "target_service = ?")
		args = append(args, qService)
	}

	if qMonitored != "" && qMonitored != "ALL" {
		whereClauses = append(whereClauses, "monitored_status = ?")
		args = append(args, qMonitored)
	}

	if qSearch != "" {
		likeTerm := "%" + qSearch + "%"
		whereClauses = append(whereClauses, "(title_romaji LIKE ? OR title_english LIKE ? OR arr_title LIKE ?)")
		args = append(args, likeTerm, likeTerm, likeTerm)
	}

	if len(whereClauses) > 0 {
		queryBase += " WHERE " + strings.Join(whereClauses, " AND ")
	}

	var orderBy string
	switch qSort {
	case "year":
		orderBy = "release_year"
	case "status":
		orderBy = "anilist_status"
	case "synced", "updated":
		orderBy = "last_synced_at"
	default:
		orderBy = "title_romaji"
	}

	if qOrder == "desc" {
		orderBy += " DESC"
	} else {
		orderBy += " ASC"
	}

	queryBase += " ORDER BY " + orderBy + ";"

	var items []SyncedMediaItem
	err := s.db.Read(r.Context(), func(ctx context.Context, q storage.Querier) error {
		rows, err := q.QueryContext(ctx, queryBase, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var it SyncedMediaItem
			var eng, poster, arrTitle, seasons sql.NullString
			var yr, tvdb, tmdb sql.NullInt64

			scanErr := rows.Scan(
				&it.AniListID, &it.MediaType, &it.Format, &it.TitleRomaji, &eng,
				&yr, &poster, &it.AniListStatus, &it.TargetService,
				&tvdb, &tmdb, &arrTitle, &it.MonitoredStatus, &seasons,
				&it.LastSyncedAt, &it.UpdatedAt,
			)
			if scanErr != nil {
				return scanErr
			}

			if eng.Valid {
				it.TitleEnglish = &eng.String
			}
			if poster.Valid {
				it.PosterURL = &poster.String
			}
			if yr.Valid {
				y := int(yr.Int64)
				it.ReleaseYear = &y
			}
			if tvdb.Valid {
				t := int(tvdb.Int64)
				it.TVDBID = &t
			}
			if tmdb.Valid {
				t := int(tmdb.Int64)
				it.TMDBID = &t
			}
			if arrTitle.Valid {
				it.ArrTitle = &arrTitle.String
			}
			if seasons.Valid {
				it.Seasons = &seasons.String
			}

			items = append(items, it)
		}
		return rows.Err()
	})

	if err != nil {
		log.Printf("list library query failed: %v", err)
		s.writeJSONError(w, http.StatusInternalServerError, "failed to query library")
		return
	}

	if items == nil {
		items = make([]SyncedMediaItem, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(items)
}

// handleGetLibraryStats returns aggregate statistics for the library.
func (s *Server) handleGetLibraryStats(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	var stats LibraryStats
	err := s.db.Read(r.Context(), func(ctx context.Context, q storage.Querier) error {
		row := q.QueryRowContext(ctx, `
			SELECT
				COUNT(*),
				COALESCE(SUM(CASE WHEN monitored_status = 'MONITORED' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN monitored_status = 'UNMONITORED' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN target_service = 'SONARR' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN target_service = 'RADARR' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN anilist_status = 'CURRENT' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN anilist_status = 'PLANNING' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN anilist_status = 'COMPLETED' THEN 1 ELSE 0 END), 0)
			FROM synced_media;
		`)
		return row.Scan(
			&stats.Total,
			&stats.Monitored,
			&stats.Unmonitored,
			&stats.SonarrCount,
			&stats.RadarrCount,
			&stats.WatchingCount,
			&stats.PlanningCount,
			&stats.CompletedCount,
		)
	})

	if err != nil {
		log.Printf("library stats query failed: %v", err)
		s.writeJSONError(w, http.StatusInternalServerError, "failed to calculate library stats")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(stats)
}

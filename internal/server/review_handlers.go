package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

// ReviewQueueItem represents an item waiting in review_queue.
type ReviewQueueItem struct {
	ID             int        `json:"id"`
	AniListID      int        `json:"anilistId"`
	MediaType      string     `json:"mediaType"`
	TitleRomaji    string     `json:"titleRomaji"`
	TitleEnglish   *string    `json:"titleEnglish,omitempty"`
	ReleaseYear    *int       `json:"releaseYear,omitempty"`
	PosterURL      *string    `json:"posterUrl,omitempty"`
	CandidatesJSON string     `json:"candidatesJson"`
	Reason         string     `json:"reason"`
	Status         string     `json:"status"`
	ResolvedID     *int       `json:"resolvedId,omitempty"`
	ResolvedSeason *int       `json:"resolvedSeason,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	ResolvedAt     *time.Time `json:"resolvedAt,omitempty"`
}

type resolveReviewRequest struct {
	MediaType     string `json:"mediaType"`
	TVDBID        int    `json:"tvdbId"`
	TMDBID        int    `json:"tmdbId"`
	Seasons       string `json:"seasons"`
	TitleOverride string `json:"titleOverride"`
}

type ignoreReviewRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) handleListReview(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	var items []ReviewQueueItem
	err := s.db.Read(r.Context(), func(ctx context.Context, q storage.Querier) error {
		rows, err := q.QueryContext(ctx, `
			SELECT id, anilist_id, media_type, title_romaji, title_english, release_year,
			       poster_url, candidates_json, reason, status, resolved_id, resolved_season,
			       created_at, resolved_at
			FROM review_queue
			WHERE status = 'PENDING'
			ORDER BY id ASC;
		`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var item ReviewQueueItem
			var english, poster sql.NullString
			var year, resolvedID, resolvedSeason sql.NullInt64
			var resolvedAt sql.NullTime

			if scanErr := rows.Scan(
				&item.ID, &item.AniListID, &item.MediaType, &item.TitleRomaji,
				&english, &year, &poster, &item.CandidatesJSON, &item.Reason,
				&item.Status, &resolvedID, &resolvedSeason, &item.CreatedAt, &resolvedAt,
			); scanErr != nil {
				return scanErr
			}

			if english.Valid {
				item.TitleEnglish = &english.String
			}
			if poster.Valid {
				item.PosterURL = &poster.String
			}
			if year.Valid {
				y := int(year.Int64)
				item.ReleaseYear = &y
			}
			if resolvedID.Valid {
				rid := int(resolvedID.Int64)
				item.ResolvedID = &rid
			}
			if resolvedSeason.Valid {
				rs := int(resolvedSeason.Int64)
				item.ResolvedSeason = &rs
			}
			if resolvedAt.Valid {
				t := resolvedAt.Time
				item.ResolvedAt = &t
			}

			items = append(items, item)
		}
		return rows.Err()
	})

	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "query review queue failed: "+err.Error())
		return
	}

	if items == nil {
		items = []ReviewQueueItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(items)
}

func (s *Server) handleResolveReview(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		s.writeJSONError(w, http.StatusBadRequest, "invalid review queue id")
		return
	}

	var req resolveReviewRequest
	if r.Body != nil {
		if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil && !errors.Is(decErr, io.EOF) {
			s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	if req.TVDBID <= 0 && req.TMDBID <= 0 {
		s.writeJSONError(w, http.StatusBadRequest, "tvdbId or tmdbId is required")
		return
	}

	reqMedia := strings.ToUpper(strings.TrimSpace(req.MediaType))
	if reqMedia != "" && reqMedia != "MOVIE" && reqMedia != "SERIES" {
		s.writeJSONError(w, http.StatusBadRequest, "mediaType must be MOVIE or SERIES")
		return
	}

	var anilistID int
	err = s.db.Write(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		var queueMediaType, titleRomaji string
		queryErr := tx.QueryRowContext(ctx, `
			SELECT anilist_id, media_type, title_romaji
			FROM review_queue
			WHERE id = ? AND status = 'PENDING';
		`, id).Scan(&anilistID, &queueMediaType, &titleRomaji)
		if queryErr != nil {
			return queryErr
		}

		mediaType := reqMedia
		if mediaType == "" {
			mediaType = queueMediaType
		}

		seasons := strings.TrimSpace(req.Seasons)
		if seasons == "" {
			seasons = "1"
		}

		resolvedID := req.TVDBID
		if resolvedID == 0 {
			resolvedID = req.TMDBID
		}
		var resolvedSeason sql.NullInt64
		if sNum, parseErr := strconv.Atoi(seasons); parseErr == nil && sNum > 0 {
			resolvedSeason = sql.NullInt64{Int64: int64(sNum), Valid: true}
		}

		_, overrideErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (
				anilist_id, media_type, tvdb_id, tmdb_id, seasons, title_override, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(anilist_id) DO UPDATE SET
				media_type = excluded.media_type,
				tvdb_id = excluded.tvdb_id,
				tmdb_id = excluded.tmdb_id,
				seasons = excluded.seasons,
				title_override = excluded.title_override,
				updated_at = CURRENT_TIMESTAMP;
		`, anilistID, mediaType, req.TVDBID, req.TMDBID, seasons, req.TitleOverride)
		if overrideErr != nil {
			return overrideErr
		}

		res, updateErr := tx.ExecContext(ctx, `
			UPDATE review_queue
			SET status = 'RESOLVED',
			    resolved_id = ?,
			    resolved_season = ?,
			    resolved_at = CURRENT_TIMESTAMP
			WHERE id = ? AND status = 'PENDING';
		`, resolvedID, resolvedSeason, id)
		if updateErr != nil {
			return updateErr
		}
		rows, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return rowsErr
		}
		if rows == 0 {
			return sql.ErrNoRows
		}
		return nil
	})

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeJSONError(w, http.StatusNotFound, fmt.Sprintf("review queue item %d not found or already resolved", id))
			return
		}
		s.writeJSONError(w, http.StatusInternalServerError, "resolve review item failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "RESOLVED",
		"id":        id,
		"anilistId": anilistID,
	})
}

func (s *Server) handleIgnoreReview(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		s.writeJSONError(w, http.StatusBadRequest, "invalid review queue id")
		return
	}

	var req ignoreReviewRequest
	if r.Body != nil {
		if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil && !errors.Is(decErr, io.EOF) {
			s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "Ignored from review queue"
	}

	var anilistID int
	err = s.db.Write(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		var titleRomaji string
		queryErr := tx.QueryRowContext(ctx, `
			SELECT anilist_id, title_romaji
			FROM review_queue
			WHERE id = ? AND status = 'PENDING';
		`, id).Scan(&anilistID, &titleRomaji)
		if queryErr != nil {
			return queryErr
		}

		_, ignoreErr := tx.ExecContext(ctx, `
			INSERT INTO ignored_titles (anilist_id, title_romaji, reason)
			VALUES (?, ?, ?)
			ON CONFLICT(anilist_id) DO UPDATE SET
				reason = excluded.reason;
		`, anilistID, titleRomaji, reason)
		if ignoreErr != nil {
			return ignoreErr
		}

		res, updateErr := tx.ExecContext(ctx, `
			UPDATE review_queue
			SET status = 'RESOLVED',
			    resolved_at = CURRENT_TIMESTAMP
			WHERE id = ? AND status = 'PENDING';
		`, id)
		if updateErr != nil {
			return updateErr
		}
		rows, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return rowsErr
		}
		if rows == 0 {
			return sql.ErrNoRows
		}
		return nil
	})

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeJSONError(w, http.StatusNotFound, fmt.Sprintf("review queue item %d not found or already resolved", id))
			return
		}
		s.writeJSONError(w, http.StatusInternalServerError, "ignore review item failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "RESOLVED",
		"id":        id,
		"anilistId": anilistID,
	})
}

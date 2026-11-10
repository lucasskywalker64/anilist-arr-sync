package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

var (
	errTmdbRequiredForMovie  = errors.New("tmdbId is required for MOVIE")
	errTvdbRequiredForSeries = errors.New("tvdbId is required for SERIES")
	errSpecialsRequireSeries = errors.New("specials episodes require a SERIES override")
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
	PreparationRequired bool   `json:"preparationRequired"`
	MediaType           string `json:"mediaType"`
	TVDBID              *int   `json:"tvdbId"`
	TMDBID              *int   `json:"tmdbId"`
	Seasons             string `json:"seasons"`
	TitleOverride       string `json:"titleOverride"`
	EpisodeNumbers      []int  `json:"episodeNumbers"`
}

type ignoreReviewRequest struct {
	Reason string `json:"reason"`
}

// validateSeasons checks that seasons is a valid single positive integer or delimited list of positive integers.
func validateSeasons(seasons string) bool {
	if seasons == "" {
		return false
	}
	parts := strings.FieldsFunc(seasons, func(c rune) bool {
		return c == ',' || c == ' ' || c == ';'
	})
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		num, err := strconv.Atoi(p)
		if err != nil || num <= 0 {
			return false
		}
	}
	return true
}

// validateEpisodeOverride validates selections in one Sonarr season and requires them for Specials.
func validateEpisodeOverride(mediaType, seasons string, numbers []int) error {
	if seasons == "0" {
		if mediaType != "SERIES" || len(numbers) == 0 {
			return errors.New("specials (season 0) requires a SERIES override with at least one episode")
		}
	} else if len(numbers) > 0 {
		season, err := strconv.Atoi(seasons)
		if mediaType != "SERIES" || err != nil || season < 0 || strconv.Itoa(season) != seasons {
			return errors.New("episodeNumbers requires one Sonarr season")
		}
	} else {
		for _, part := range strings.FieldsFunc(seasons, func(c rune) bool { return c == ',' || c == ' ' || c == ';' }) {
			if number, err := strconv.Atoi(part); err == nil && number == 0 {
				return errors.New("specials overrides require season '0' and at least one episode")
			}
		}
	}
	seen := make(map[int]bool)
	for _, number := range numbers {
		if number <= 0 || seen[number] {
			return errors.New("episodeNumbers must be distinct positive integers")
		}
		seen[number] = true
	}
	return nil
}

// handleSpecialEpisodes supplies episode numbers and titles from the configured Sonarr library.
func (s *Server) handleSpecialEpisodes(w http.ResponseWriter, r *http.Request) {
	tvdbID, err := strconv.Atoi(r.PathValue("tvdbId"))
	if err != nil || tvdbID <= 0 {
		s.writeJSONError(w, http.StatusBadRequest, "invalid TVDB ID")
		return
	}
	cfg := s.Config()
	client, err := servarr.NewSonarrClient(cfg.SonarrURL, cfg.SonarrAPIKey)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Sonarr is not configured")
		return
	}
	series, err := client.LookupSeriesByTVDBID(r.Context(), tvdbID)
	if err != nil || series == nil || series.ID <= 0 {
		s.writeJSONError(w, http.StatusBadRequest, "series must exist in Sonarr to select Specials episodes")
		return
	}
	episodes, err := client.GetEpisodes(r.Context(), series.ID)
	if err != nil {
		s.writeJSONError(w, http.StatusBadGateway, "could not load Sonarr episodes")
		return
	}
	specials := make([]servarr.Episode, 0)
	for _, ep := range episodes {
		if ep.SeasonNumber == 0 {
			specials = append(specials, ep)
		}
	}
	sort.Slice(specials, func(i, j int) bool { return specials[i].EpisodeNumber < specials[j].EpisodeNumber })
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(specials)
}

// handleListReview returns all pending items waiting in review_queue.
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
		log.Printf("query review queue failed: %v", err)
		s.writeJSONError(w, http.StatusInternalServerError, "query review queue failed")
		return
	}

	if items == nil {
		items = []ReviewQueueItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(items)
}

// handleResolveReview confirms mapping for a queue item and inserts it into mapping_overrides.
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

	var tvdbVal, tmdbVal any
	var resolvedID int
	if req.TVDBID != nil && *req.TVDBID > 0 {
		tvdbVal = *req.TVDBID
		resolvedID = *req.TVDBID
	}
	if req.TMDBID != nil && *req.TMDBID > 0 {
		tmdbVal = *req.TMDBID
		if resolvedID == 0 {
			resolvedID = *req.TMDBID
		}
	}

	if tvdbVal == nil && tmdbVal == nil {
		s.writeJSONError(w, http.StatusBadRequest, "tvdbId or tmdbId is required")
		return
	}

	reqMedia := strings.ToUpper(strings.TrimSpace(req.MediaType))
	if reqMedia != "" && reqMedia != "MOVIE" && reqMedia != "SERIES" {
		s.writeJSONError(w, http.StatusBadRequest, "mediaType must be MOVIE or SERIES")
		return
	}
	if reqMedia == "MOVIE" && tmdbVal == nil {
		s.writeJSONError(w, http.StatusBadRequest, "tmdbId is required for MOVIE")
		return
	}
	if reqMedia == "SERIES" && tvdbVal == nil {
		s.writeJSONError(w, http.StatusBadRequest, "tvdbId is required for SERIES")
		return
	}
	switch reqMedia {
	case "MOVIE":
		tvdbVal = nil
		resolvedID = *req.TMDBID
	case "SERIES":
		tmdbVal = nil
	}

	seasons := strings.TrimSpace(req.Seasons)
	if seasons == "" {
		seasons = "1"
	}
	if seasons != "0" && !validateSeasons(seasons) {
		s.writeJSONError(w, http.StatusBadRequest, "invalid seasons format: must be positive integers")
		return
	}
	// An omitted media type is validated against the queue item inside the transaction.
	validationMedia := reqMedia
	if validationMedia == "" {
		validationMedia = "SERIES"
	}
	if err := validateEpisodeOverride(validationMedia, seasons, req.EpisodeNumbers); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	sort.Ints(req.EpisodeNumbers)
	if req.EpisodeNumbers == nil {
		req.EpisodeNumbers = []int{}
	}
	episodeNumbersJSON, _ := json.Marshal(req.EpisodeNumbers)

	var titleVal any
	if trimmed := strings.TrimSpace(req.TitleOverride); trimmed != "" {
		titleVal = trimmed
	}

	var anilistID int
	saveMapping := func(ctx context.Context, tx *sql.Tx) error {
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
		if mediaType == "MOVIE" {
			tvdbVal = nil
			if req.TMDBID != nil {
				resolvedID = *req.TMDBID
			}
		} else {
			tmdbVal = nil
		}
		if mediaType == "MOVIE" && tmdbVal == nil {
			return errTmdbRequiredForMovie
		}
		if mediaType == "SERIES" && tvdbVal == nil {
			return errTvdbRequiredForSeries
		}
		if mediaType == "MOVIE" && len(req.EpisodeNumbers) > 0 {
			return errSpecialsRequireSeries
		}

		parts := strings.FieldsFunc(seasons, func(c rune) bool {
			return c == ',' || c == ' ' || c == ';'
		})
		firstSeason, _ := strconv.Atoi(parts[0])
		resolvedSeason := sql.NullInt64{Int64: int64(firstSeason), Valid: true}

		_, overrideErr := tx.ExecContext(ctx, `
			INSERT INTO mapping_overrides (
				anilist_id, media_type, tvdb_id, tmdb_id, seasons, title_override, episode_numbers, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(anilist_id) DO UPDATE SET
				media_type = excluded.media_type,
				tvdb_id = excluded.tvdb_id,
				tmdb_id = excluded.tmdb_id,
				seasons = excluded.seasons,
				episode_numbers = excluded.episode_numbers,
				title_override = excluded.title_override,
				updated_at = CURRENT_TIMESTAMP;
		`, anilistID, mediaType, tvdbVal, tmdbVal, seasons, titleVal, string(episodeNumbersJSON))
		if overrideErr != nil {
			return overrideErr
		}

		targetService := "SONARR"
		var seasonsCol any = seasons
		if mediaType == "MOVIE" {
			targetService = "RADARR"
			seasonsCol = nil
		}
		if _, syncErr := tx.ExecContext(ctx, `
			UPDATE synced_media
			SET media_type = ?,
			    target_service = ?,
			    tvdb_id = ?,
			    tmdb_id = ?,
			    arr_title = ?,
			    seasons = ?,
			    monitored_status = CASE WHEN monitored_status = 'PENDING_REVIEW' THEN 'UNMONITORED' ELSE monitored_status END,
			    updated_at = CURRENT_TIMESTAMP
			WHERE anilist_id = ?;
		`, mediaType, targetService, tvdbVal, tmdbVal, titleVal, seasonsCol, anilistID); syncErr != nil {
			return syncErr
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
	}
	if s.orch != nil {
		tvdbID := 0
		if reqMedia != "MOVIE" && req.TVDBID != nil {
			tvdbID = *req.TVDBID
		}
		season, _ := strconv.Atoi(seasons)
		err = s.orch.ConfirmReviewMapping(r.Context(), id, tvdbID, season, req.EpisodeNumbers, req.PreparationRequired, saveMapping)
	} else {
		err = s.db.Write(r.Context(), saveMapping)
	}

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeJSONError(w, http.StatusNotFound, fmt.Sprintf("review queue item %d not found or already resolved", id))
			return
		}
		if errors.Is(err, errTmdbRequiredForMovie) || errors.Is(err, errTvdbRequiredForSeries) || errors.Is(err, errSpecialsRequireSeries) {
			s.writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Printf("resolve review item %d failed: %v", id, err)
		s.writeJSONError(w, http.StatusInternalServerError, "resolve review item failed")
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

// handleIgnoreReview records a title in ignored_titles and resolves the queue item.
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

		if _, syncErr := tx.ExecContext(ctx, `
			UPDATE synced_media
			SET monitored_status = 'IGNORED',
			    updated_at = CURRENT_TIMESTAMP
			WHERE anilist_id = ?;
		`, anilistID); syncErr != nil {
			return syncErr
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
		log.Printf("ignore review item %d failed: %v", id, err)
		s.writeJSONError(w, http.StatusInternalServerError, "ignore review item failed")
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

// IgnoredTitle represents a row in the ignored_titles table.
type IgnoredTitle struct {
	AniListID   int       `json:"anilistId"`
	TitleRomaji string    `json:"titleRomaji"`
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"createdAt"`
}

// handleListIgnored returns all titles in ignored_titles.
func (s *Server) handleListIgnored(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	var list []IgnoredTitle
	err := s.db.Read(r.Context(), func(ctx context.Context, q storage.Querier) error {
		rows, err := q.QueryContext(ctx, `
			SELECT anilist_id, title_romaji, COALESCE(reason, ''), created_at
			FROM ignored_titles
			ORDER BY created_at DESC, anilist_id ASC;
		`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var item IgnoredTitle
			if err := rows.Scan(&item.AniListID, &item.TitleRomaji, &item.Reason, &item.CreatedAt); err != nil {
				return err
			}
			list = append(list, item)
		}
		return rows.Err()
	})
	if err != nil {
		log.Printf("list ignored titles failed: %v", err)
		s.writeJSONError(w, http.StatusInternalServerError, "failed to query ignored titles")
		return
	}

	if list == nil {
		list = []IgnoredTitle{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(list)
}

// handleDeleteIgnored removes an entry from ignored_titles by anilistId.
func (s *Server) handleDeleteIgnored(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		s.writeJSONError(w, http.StatusBadRequest, "invalid anilist id")
		return
	}

	err = s.db.Write(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		res, execErr := tx.ExecContext(ctx, "DELETE FROM ignored_titles WHERE anilist_id = ?;", id)
		if execErr != nil {
			return execErr
		}
		rows, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return rowsErr
		}
		if rows == 0 {
			return sql.ErrNoRows
		}
		_, syncErr := tx.ExecContext(ctx, `
			UPDATE synced_media
			SET monitored_status = 'UNMONITORED',
			    updated_at = CURRENT_TIMESTAMP
			WHERE anilist_id = ? AND monitored_status = 'IGNORED';
		`, id)
		return syncErr
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeJSONError(w, http.StatusNotFound, fmt.Sprintf("ignored title with anilist_id %d not found", id))
			return
		}
		log.Printf("delete ignored title %d failed: %v", id, err)
		s.writeJSONError(w, http.StatusInternalServerError, "failed to delete ignored title")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "DELETED",
		"anilistId": id,
	})
}

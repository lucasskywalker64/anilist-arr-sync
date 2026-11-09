package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/resolver"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

// StagedActionType represents a planned Servarr operation.
type StagedActionType string

// Supported types of staged operations.
const (
	// ActionAddMovie represents a planned addition of a movie to Radarr.
	ActionAddMovie StagedActionType = "ADD_MOVIE"
	// ActionAddSeries represents a planned addition of a series to Sonarr.
	ActionAddSeries StagedActionType = "ADD_SERIES"
	// ActionMonitorSeason represents a planned monitoring toggle for a Sonarr season or episode slice.
	ActionMonitorSeason StagedActionType = "MONITOR_SEASON"
	// ActionUnmonitorSeason represents a planned unmonitoring toggle for a Sonarr season or episode slice.
	ActionUnmonitorSeason StagedActionType = "UNMONITOR_SEASON"
	// ActionUnmonitorMovie represents a planned unmonitoring toggle for a Radarr movie.
	ActionUnmonitorMovie StagedActionType = "UNMONITOR_MOVIE"
	// ActionDeleteFile represents a planned deletion of a movie file in Radarr or episode file(s) in Sonarr.
	ActionDeleteFile StagedActionType = "DELETE_FILE"
	// ActionUnmonitorEpisode represents a planned unmonitoring toggle for specific episodes in Sonarr.
	ActionUnmonitorEpisode StagedActionType = "UNMONITOR_EPISODE"
	// ActionDeleteEpisodeFile represents a planned deletion of episode file(s) for watched episodes in Sonarr.
	ActionDeleteEpisodeFile StagedActionType = "DELETE_EPISODE_FILE"
)

func (t StagedActionType) usesLocalIDs() bool {
	switch t {
	case ActionMonitorSeason, ActionUnmonitorSeason, ActionUnmonitorMovie, ActionDeleteFile, ActionUnmonitorEpisode, ActionDeleteEpisodeFile:
		return true
	default:
		return false
	}
}

// ErrStagedEndpointChanged indicates that an action's original endpoint cannot be verified.
var ErrStagedEndpointChanged = errors.New("staged action endpoint changed or is unknown. Reject it and run a new dry run")

// ErrDuplicateStagedAction indicates that a batch repeats an action ID.
var ErrDuplicateStagedAction = errors.New("duplicate staged action ID")

// StagedActionStatus represents the execution lifecycle of a staged action.
type StagedActionStatus string

// Supported lifecycle states for staged operations.
const (
	// StagedStatusPending indicates the action is awaiting user confirmation.
	StagedStatusPending StagedActionStatus = "PENDING"
	// StagedStatusApplied indicates the action has been successfully applied to the downstream service.
	StagedStatusApplied StagedActionStatus = "APPLIED"
	// StagedStatusRejected indicates the action was discarded without downstream execution.
	StagedStatusRejected StagedActionStatus = "REJECTED"
)

// StagedAction represents a row in the staged_sync_actions table.
type StagedAction struct {
	ID            int                `json:"id"`
	RunID         sql.NullInt64      `json:"runId"`
	ActionType    StagedActionType   `json:"actionType"`
	MediaType     string             `json:"mediaType"`
	Title         string             `json:"title"`
	TargetService string             `json:"targetService"`
	PayloadJSON   string             `json:"payloadJson"`
	Status        StagedActionStatus `json:"status"`
	ExecutedAt    *time.Time         `json:"executedAt,omitempty"`
	CreatedAt     time.Time          `json:"createdAt"`
}

// AddSeriesPayload retains watched progress when searching must follow pruning.
type AddSeriesPayload struct {
	servarr.AddSeriesRequest
	WatchedEntry *anilist.MediaListEntry `json:"watchedEntry,omitempty"`
	TargetSeason int                     `json:"targetSeason,omitempty"`
}

// MonitorSeasonPayload stores parameters for monitoring or unmonitoring a season or episode slice.
type MonitorSeasonPayload struct {
	SpecialsOverride *SpecialsOverrideScope `json:"specialsOverride,omitempty"`
	EpisodeNumbers   []int                  `json:"episodeNumbers,omitempty"`
	SeriesID         int                    `json:"seriesId"`
	TVDBID           int                    `json:"tvdbId"`
	SeasonNumber     int                    `json:"seasonNumber"`
	EpisodeIDs       []int                  `json:"episodeIds,omitempty"`
	AnilistID        int                    `json:"anilistId,omitempty"`
}

// UnmonitorMoviePayload stores parameters for unmonitoring a movie in Radarr.
type UnmonitorMoviePayload struct {
	MovieID int    `json:"movieId"`
	TMDBID  int    `json:"tmdbId"`
	Title   string `json:"title"`
}

// DeleteFilePayload stores parameters for deleting files in Sonarr or Radarr.
type DeleteFilePayload struct {
	SpecialsOverride   *SpecialsOverrideScope `json:"specialsOverride,omitempty"`
	EpisodeNumbers     []int                  `json:"episodeNumbers,omitempty"`
	MovieID            int                    `json:"movieId,omitempty"`
	MovieFileID        int                    `json:"movieFileId,omitempty"`
	SeriesID           int                    `json:"seriesId,omitempty"`
	EpisodeFileIDs     []int                  `json:"episodeFileIds,omitempty"`
	Title              string                 `json:"title,omitempty"`
	EpisodeIDs         []int                  `json:"episodeIds,omitempty"`
	RequireUnmonitored *bool                  `json:"requireUnmonitored,omitempty"`
}

// UnmonitorEpisodePayload stores parameters for unmonitoring episodes in Sonarr.
type UnmonitorEpisodePayload struct {
	SpecialsOverride *SpecialsOverrideScope `json:"specialsOverride,omitempty"`
	EpisodeNumbers   []int                  `json:"episodeNumbers,omitempty"`
	SeriesID         int                    `json:"seriesId"`
	EpisodeIDs       []int                  `json:"episodeIds"`
	Title            string                 `json:"title,omitempty"`
}

// DeleteEpisodeFilePayload stores parameters for deleting episode files in Sonarr.
type DeleteEpisodeFilePayload struct {
	SpecialsOverride   *SpecialsOverrideScope `json:"specialsOverride,omitempty"`
	EpisodeNumbers     []int                  `json:"episodeNumbers,omitempty"`
	SeriesID           int                    `json:"seriesId"`
	EpisodeFileIDs     []int                  `json:"episodeFileIds"`
	Title              string                 `json:"title,omitempty"`
	EpisodeIDs         []int                  `json:"episodeIds,omitempty"`
	RequireUnmonitored *bool                  `json:"requireUnmonitored,omitempty"`
}

// SpecialsOverrideScope pins the complete mapping for validation before staged execution.
type SpecialsOverrideScope struct {
	AniListID      int   `json:"anilistId"`
	EpisodeNumbers []int `json:"episodeNumbers"`
	EpisodeIDs     []int `json:"episodeIds"`
}

func (o *Orchestrator) servarrEndpoint(service string) string {
	switch service {
	case "SONARR":
		if o.sonarr != nil {
			return o.sonarr.BaseURL()
		}
	case "RADARR":
		if o.radarr != nil {
			return o.radarr.BaseURL()
		}
	}
	return ""
}

func stagedEndpointKey(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	// An IPv6 zone names an interface, so preserve its case.
	host, zone, hasZone := strings.Cut(parsed.Host, "%")
	parsed.Host = strings.ToLower(host)
	if hasZone {
		parsed.Host += "%" + zone
	}
	return parsed.String()
}

// stageAction persists a planned Servarr action and skips identical pending actions at the same endpoint.
func (o *Orchestrator) stageAction(
	ctx context.Context,
	actionType StagedActionType,
	mediaType string,
	title string,
	targetService string,
	payload any,
) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal staged action payload: %w", err)
	}
	var endpoint string
	if actionType.usesLocalIDs() {
		endpoint = stagedEndpointKey(o.servarrEndpoint(targetService))
	}

	return o.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT target_endpoint FROM staged_sync_actions
			WHERE action_type = ? AND target_service = ? AND payload_json = ? AND status = 'PENDING';
		`, string(actionType), targetService, string(payloadBytes))
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var storedEndpoint string
			if err := rows.Scan(&storedEndpoint); err != nil {
				return err
			}
			if stagedEndpointKey(storedEndpoint) == endpoint {
				return nil
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}

		_, execErr := tx.ExecContext(ctx, `
			INSERT INTO staged_sync_actions (
				action_type, media_type, title, target_service, target_endpoint, payload_json, status
			)
			VALUES (?, ?, ?, ?, ?, ?, 'PENDING');
		`, string(actionType), mediaType, title, targetService, endpoint, string(payloadBytes))
		return execErr
	})
}

// GetStagedActions retrieves staged actions matching the given status.
func (o *Orchestrator) GetStagedActions(ctx context.Context, status StagedActionStatus) ([]StagedAction, error) {
	var actions []StagedAction
	err := o.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		rows, err := q.QueryContext(ctx, `
			SELECT id, run_id, action_type, media_type, title, target_service, payload_json, status, executed_at, created_at
			FROM staged_sync_actions
			WHERE status = ?
			ORDER BY id ASC;
		`, string(status))
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var a StagedAction
			var executedAt sql.NullTime
			if scanErr := rows.Scan(
				&a.ID, &a.RunID, &a.ActionType, &a.MediaType, &a.Title,
				&a.TargetService, &a.PayloadJSON, &a.Status, &executedAt, &a.CreatedAt,
			); scanErr != nil {
				return scanErr
			}
			if executedAt.Valid {
				t := executedAt.Time
				a.ExecutedAt = &t
			}
			actions = append(actions, a)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get staged actions: %w", err)
	}
	return actions, nil
}

// ApplyStagedAction executes a single pending staged action against Sonarr or Radarr.
func (o *Orchestrator) ApplyStagedAction(ctx context.Context, id int) error {
	_, err := o.ApplyStagedActions(ctx, []int{id})
	return err
}

// ApplyStagedActions executes a batch in order, returning any IDs applied before an error.
func (o *Orchestrator) ApplyStagedActions(ctx context.Context, ids []int) ([]int, error) {
	applied := make([]int, 0, len(ids))
	if len(ids) == 0 {
		return applied, nil
	}
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			return applied, fmt.Errorf("%w %d", ErrDuplicateStagedAction, id)
		}
		seen[id] = struct{}{}
	}
	if !o.workMu.TryLock() {
		return applied, ErrSyncInProgress
	}
	defer o.workMu.Unlock()
	run := o.snapshot()
	for _, id := range ids {
		if err := run.applyStagedAction(ctx, id); err != nil {
			return applied, fmt.Errorf("apply staged action %d: %w", id, err)
		}
		applied = append(applied, id)
	}
	return applied, nil
}

func (o *Orchestrator) applyStagedAction(ctx context.Context, id int) error {
	var action StagedAction
	var endpoint string
	err := o.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		row := q.QueryRowContext(ctx, `
			SELECT id, run_id, action_type, media_type, title, target_service, payload_json, status, target_endpoint, created_at
			FROM staged_sync_actions
			WHERE id = ? AND status = 'PENDING';
		`, id)
		return row.Scan(
			&action.ID, &action.RunID, &action.ActionType, &action.MediaType, &action.Title,
			&action.TargetService, &action.PayloadJSON, &action.Status, &endpoint, &action.CreatedAt,
		)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("staged action %d not found or not pending", id)
		}
		return fmt.Errorf("query staged action %d: %w", id, err)
	}
	if action.ActionType.usesLocalIDs() && (endpoint == "" || stagedEndpointKey(endpoint) != stagedEndpointKey(o.servarrEndpoint(action.TargetService))) {
		return ErrStagedEndpointChanged
	}
	if action.TargetService == "SONARR" {
		var payload struct {
			SeriesID         int                    `json:"seriesId"`
			SpecialsOverride *SpecialsOverrideScope `json:"specialsOverride"`
		}
		if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("unmarshal Sonarr scope: %w", err)
		}
		if scope := payload.SpecialsOverride; scope != nil {
			if o.sonarr == nil {
				return errors.New("sonarr client is nil")
			}
			episodes, err := o.sonarr.GetEpisodes(ctx, payload.SeriesID)
			if err != nil {
				return err
			}
			ids, resolved := resolveOverrideEpisodes(episodes, 0, scope.EpisodeNumbers)
			if !resolved || len(ids) == 0 || !slices.Equal(ids, scope.EpisodeIDs) {
				report := &SyncReport{}
				o.reviewMissingEpisodes(ctx, anilist.MediaListEntry{Media: anilist.Media{ID: scope.AniListID, Title: anilist.MediaTitle{Romaji: action.Title}}}, report)
				if len(report.Errors) > 0 {
					return errors.New(strings.Join(report.Errors, "; "))
				}
				return errors.New("specials override episodes changed or are missing. Resolve the entry in review and run a new sync")
			}
		}
	}

	var syncUpdateFn func(ctx context.Context, tx *sql.Tx) error

	switch action.ActionType {
	case ActionAddMovie:
		var req servarr.AddMovieRequest
		if unmarshalErr := json.Unmarshal([]byte(action.PayloadJSON), &req); unmarshalErr != nil {
			return fmt.Errorf("unmarshal add movie payload: %w", unmarshalErr)
		}
		if o.radarr == nil {
			return errors.New("radarr client is nil")
		}
		if len(req.Tags) == 0 && o.cfg.TagName != "" {
			if tagID, tagErr := o.radarr.EnsureTag(ctx, o.cfg.TagName); tagErr == nil {
				req.Tags = []int{tagID}
			}
		}
		_, addErr := o.radarr.AddMovie(ctx, req)
		if addErr != nil {
			return fmt.Errorf("execute add movie: %w", addErr)
		}
		monitoredStatus := "UNMONITORED"
		if req.Monitored {
			monitoredStatus = "MONITORED"
		}
		syncUpdateFn = func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `
				UPDATE synced_media
				SET monitored_status = ?, updated_at = CURRENT_TIMESTAMP
				WHERE tmdb_id = ? AND monitored_status != 'IGNORED';
			`, monitoredStatus, req.TMDBID)
			return err
		}

	case ActionAddSeries:
		var payload AddSeriesPayload
		if unmarshalErr := json.Unmarshal([]byte(action.PayloadJSON), &payload); unmarshalErr != nil {
			return fmt.Errorf("unmarshal add series payload: %w", unmarshalErr)
		}
		req := payload.AddSeriesRequest
		if o.sonarr == nil {
			return errors.New("sonarr client is nil")
		}
		if len(req.Tags) == 0 && o.cfg.TagName != "" {
			if tagID, tagErr := o.sonarr.EnsureTag(ctx, o.cfg.TagName); tagErr == nil {
				req.Tags = []int{tagID}
			}
		}
		var existing *servarr.Series
		if payload.WatchedEntry != nil {
			var lookupErr error
			existing, lookupErr = o.sonarr.LookupSeriesByTVDBID(ctx, req.TVDBID)
			if lookupErr != nil && !errors.Is(lookupErr, servarr.ErrNotFound) {
				return fmt.Errorf("lookup staged series: %w", lookupErr)
			}
		}
		if existing == nil || existing.ID == 0 {
			if _, addErr := o.sonarr.AddSeries(ctx, req); addErr != nil {
				return fmt.Errorf("execute add series: %w", addErr)
			}
		}
		if payload.WatchedEntry != nil {
			report := &SyncReport{}
			outcome := o.dispatchSonarrWithDryRun(ctx, *payload.WatchedEntry, &resolver.Result{TargetID: req.TVDBID, TVDBSeason: payload.TargetSeason}, report, false, true)
			if len(report.Errors) > 0 {
				return fmt.Errorf("prune staged series before search: %s", strings.Join(report.Errors, "; "))
			}
			if outcome.pendingSonarrFollowup {
				return errors.New("waiting for Sonarr episode refresh; retry this staged action")
			}
		}
		var targetSeasonStr string
		for _, s := range req.Seasons {
			if s.Monitored {
				targetSeasonStr = strconv.Itoa(s.SeasonNumber)
				break
			}
		}
		syncUpdateFn = func(ctx context.Context, tx *sql.Tx) error {
			if targetSeasonStr != "" {
				_, err := tx.ExecContext(ctx, `
					UPDATE synced_media
					SET monitored_status = 'MONITORED', updated_at = CURRENT_TIMESTAMP
					WHERE tvdb_id = ?
					  AND monitored_status != 'IGNORED'
					  AND (seasons IS NULL OR seasons = '' OR seasons = ? OR (',' || REPLACE(seasons, ' ', '') || ',') LIKE ('%,' || ? || ',%'));
				`, req.TVDBID, targetSeasonStr, targetSeasonStr)
				return err
			}
			monitoredStatus := "UNMONITORED"
			if req.Monitored {
				monitoredStatus = "MONITORED"
			}
			_, err := tx.ExecContext(ctx, `
				UPDATE synced_media
				SET monitored_status = ?, updated_at = CURRENT_TIMESTAMP
				WHERE tvdb_id = ? AND monitored_status != 'IGNORED';
			`, monitoredStatus, req.TVDBID)
			return err
		}

	case ActionMonitorSeason:
		var payload MonitorSeasonPayload
		if unmarshalErr := json.Unmarshal([]byte(action.PayloadJSON), &payload); unmarshalErr != nil {
			return fmt.Errorf("unmarshal monitor season payload: %w", unmarshalErr)
		}
		if o.sonarr == nil {
			return errors.New("sonarr client is nil")
		}
		if len(payload.EpisodeIDs) > 0 {
			if payload.SeriesID > 0 {
				series, getErr := o.sonarr.GetSeriesByID(ctx, payload.SeriesID)
				if getErr != nil {
					return fmt.Errorf("get series %d: %w", payload.SeriesID, getErr)
				}
				if !series.Monitored {
					series.Monitored = true
					if _, updateErr := o.sonarr.UpdateSeries(ctx, series); updateErr != nil {
						return fmt.Errorf("update series %d monitored: %w", payload.SeriesID, updateErr)
					}
				}
			}
			if monErr := o.sonarr.MonitorEpisodes(ctx, payload.EpisodeIDs, true); monErr != nil {
				return fmt.Errorf("execute monitor episodes: %w", monErr)
			}
		} else {
			series, getErr := o.sonarr.GetSeriesByID(ctx, payload.SeriesID)
			if getErr != nil {
				return fmt.Errorf("get series %d: %w", payload.SeriesID, getErr)
			}
			series.Monitored = true
			found := false
			for i := range series.Seasons {
				if series.Seasons[i].SeasonNumber == payload.SeasonNumber {
					series.Seasons[i].Monitored = true
					found = true
					break
				}
			}
			if !found {
				series.Seasons = append(series.Seasons, servarr.Season{
					SeasonNumber: payload.SeasonNumber,
					Monitored:    true,
				})
			}
			if _, updateErr := o.sonarr.UpdateSeries(ctx, series); updateErr != nil {
				return fmt.Errorf("update series season monitoring: %w", updateErr)
			}
		}
		if o.cfg.SonarrSearchOnAdd && payload.SeriesID > 0 && payload.SeasonNumber >= 0 {
			if len(payload.EpisodeIDs) > 0 {
				_, _ = o.sonarr.ExecuteCommand(ctx, map[string]any{"name": "EpisodeSearch", "episodeIds": payload.EpisodeIDs})
			} else {
				_, _ = o.sonarr.SearchSeason(ctx, payload.SeriesID, payload.SeasonNumber)
			}
		}
		seasonStr := strconv.Itoa(payload.SeasonNumber)
		syncUpdateFn = func(ctx context.Context, tx *sql.Tx) error {
			if payload.AnilistID > 0 {
				_, err := tx.ExecContext(ctx, `
					UPDATE synced_media
					SET monitored_status = 'MONITORED', updated_at = CURRENT_TIMESTAMP
					WHERE anilist_id = ? AND monitored_status != 'IGNORED';
				`, payload.AnilistID)
				return err
			}
			_, err := tx.ExecContext(ctx, `
				UPDATE synced_media
				SET monitored_status = 'MONITORED', updated_at = CURRENT_TIMESTAMP
				WHERE tvdb_id = ?
				  AND monitored_status != 'IGNORED'
				  AND (seasons IS NULL OR seasons = '' OR seasons = ? OR (',' || REPLACE(seasons, ' ', '') || ',') LIKE ('%,' || ? || ',%'));
			`, payload.TVDBID, seasonStr, seasonStr)
			return err
		}

	case ActionUnmonitorSeason:
		var payload MonitorSeasonPayload
		if unmarshalErr := json.Unmarshal([]byte(action.PayloadJSON), &payload); unmarshalErr != nil {
			return fmt.Errorf("unmarshal unmonitor season payload: %w", unmarshalErr)
		}
		if o.sonarr == nil {
			return errors.New("sonarr client is nil")
		}
		if len(payload.EpisodeIDs) > 0 {
			if monErr := o.sonarr.MonitorEpisodes(ctx, payload.EpisodeIDs, false); monErr != nil {
				return fmt.Errorf("execute unmonitor episodes: %w", monErr)
			}
		} else {
			series, getErr := o.sonarr.GetSeriesByID(ctx, payload.SeriesID)
			if getErr != nil {
				return fmt.Errorf("get series %d: %w", payload.SeriesID, getErr)
			}
			for i := range series.Seasons {
				if series.Seasons[i].SeasonNumber == payload.SeasonNumber {
					series.Seasons[i].Monitored = false
					break
				}
			}
			if _, updateErr := o.sonarr.UpdateSeries(ctx, series); updateErr != nil {
				return fmt.Errorf("update series season monitoring: %w", updateErr)
			}
		}
		seasonStr := strconv.Itoa(payload.SeasonNumber)
		syncUpdateFn = func(ctx context.Context, tx *sql.Tx) error {
			if payload.AnilistID > 0 {
				_, err := tx.ExecContext(ctx, `
					UPDATE synced_media
					SET monitored_status = 'UNMONITORED', updated_at = CURRENT_TIMESTAMP
					WHERE anilist_id = ? AND monitored_status != 'IGNORED';
				`, payload.AnilistID)
				return err
			}
			_, err := tx.ExecContext(ctx, `
				UPDATE synced_media
				SET monitored_status = 'UNMONITORED', updated_at = CURRENT_TIMESTAMP
				WHERE tvdb_id = ?
				  AND monitored_status != 'IGNORED'
				  AND (seasons IS NULL OR seasons = '' OR seasons = ? OR (',' || REPLACE(seasons, ' ', '') || ',') LIKE ('%,' || ? || ',%'));
			`, payload.TVDBID, seasonStr, seasonStr)
			return err
		}

	case ActionUnmonitorMovie:
		var payload UnmonitorMoviePayload
		if unmarshalErr := json.Unmarshal([]byte(action.PayloadJSON), &payload); unmarshalErr != nil {
			return fmt.Errorf("unmarshal unmonitor movie payload: %w", unmarshalErr)
		}
		if o.radarr == nil {
			return errors.New("radarr client is nil")
		}
		movie, getErr := o.radarr.GetMovieByID(ctx, payload.MovieID)
		if getErr != nil {
			return fmt.Errorf("get movie %d: %w", payload.MovieID, getErr)
		}
		movie.Monitored = false
		if _, updateErr := o.radarr.UpdateMovie(ctx, movie); updateErr != nil {
			return fmt.Errorf("update movie monitoring: %w", updateErr)
		}
		syncUpdateFn = func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `
				UPDATE synced_media
				SET monitored_status = 'UNMONITORED', updated_at = CURRENT_TIMESTAMP
				WHERE tmdb_id = ? AND monitored_status != 'IGNORED';
			`, payload.TMDBID)
			return err
		}

	case ActionDeleteFile:
		var payload DeleteFilePayload
		if unmarshalErr := json.Unmarshal([]byte(action.PayloadJSON), &payload); unmarshalErr != nil {
			return fmt.Errorf("unmarshal delete file payload: %w", unmarshalErr)
		}
		if payload.RequireUnmonitored == nil {
			return errors.New("staged deletion predates safety checks; reject it and sync again")
		}
		switch action.TargetService {
		case "RADARR":
			if o.radarr == nil {
				return errors.New("radarr client is nil")
			}
			movie, err := o.radarr.GetMovieByID(ctx, payload.MovieID)
			if err != nil {
				return fmt.Errorf("validate movie deletion: %w", err)
			}
			managed, err := o.hasManagedTag(ctx, o.radarr.Client, movie.Tags)
			if err != nil {
				return fmt.Errorf("validate movie managed tag: %w", err)
			}
			if !managed {
				return errors.New("movie no longer has the managed tag")
			}
			if *payload.RequireUnmonitored && movie.Monitored {
				return errors.New("unmonitor movie before applying file deletion")
			}
			if !movie.HasFile {
				break
			}
			if movie.MovieFileID != payload.MovieFileID {
				return errors.New("movie file changed since staging")
			}
			if payload.MovieFileID > 0 {
				if err := o.radarr.DeleteMovieFile(ctx, payload.MovieFileID); err != nil && !errors.Is(err, servarr.ErrNotFound) {
					return fmt.Errorf("execute delete movie file: %w", err)
				}
			}
		case "SONARR":
			if o.sonarr == nil {
				return errors.New("sonarr client is nil")
			}
			fileIDs, err := o.validateEpisodeDeletion(ctx, payload.SeriesID, payload.EpisodeIDs, payload.EpisodeFileIDs, *payload.RequireUnmonitored)
			if err != nil {
				return err
			}
			for _, fileID := range fileIDs {
				if err := o.sonarr.DeleteEpisodeFile(ctx, fileID); err != nil && !errors.Is(err, servarr.ErrNotFound) {
					return fmt.Errorf("execute delete episode file %d: %w", fileID, err)
				}
			}
		}

	case ActionUnmonitorEpisode:
		var payload UnmonitorEpisodePayload
		if unmarshalErr := json.Unmarshal([]byte(action.PayloadJSON), &payload); unmarshalErr != nil {
			return fmt.Errorf("unmarshal unmonitor episode payload: %w", unmarshalErr)
		}
		if o.sonarr == nil {
			return errors.New("sonarr client is nil")
		}
		if len(payload.EpisodeIDs) > 0 {
			if monErr := o.sonarr.MonitorEpisodes(ctx, payload.EpisodeIDs, false); monErr != nil {
				return fmt.Errorf("execute unmonitor episodes: %w", monErr)
			}
		}

	case ActionDeleteEpisodeFile:
		var payload DeleteEpisodeFilePayload
		if unmarshalErr := json.Unmarshal([]byte(action.PayloadJSON), &payload); unmarshalErr != nil {
			return fmt.Errorf("unmarshal delete episode file payload: %w", unmarshalErr)
		}
		if payload.RequireUnmonitored == nil {
			return errors.New("staged deletion predates safety checks; reject it and sync again")
		}
		if o.sonarr == nil {
			return errors.New("sonarr client is nil")
		}
		fileIDs, err := o.validateEpisodeDeletion(ctx, payload.SeriesID, payload.EpisodeIDs, payload.EpisodeFileIDs, *payload.RequireUnmonitored)
		if err != nil {
			return err
		}
		for _, fid := range fileIDs {
			if delErr := o.sonarr.DeleteEpisodeFile(ctx, fid); delErr != nil && !errors.Is(delErr, servarr.ErrNotFound) {
				return fmt.Errorf("execute delete episode file %d: %w", fid, delErr)
			}
		}

	default:
		return fmt.Errorf("unknown action type: %s", action.ActionType)
	}

	return o.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, execErr := tx.ExecContext(ctx, `
			UPDATE staged_sync_actions
			SET status = 'APPLIED', executed_at = CURRENT_TIMESTAMP
			WHERE id = ? AND status = 'PENDING';
		`, id)
		if execErr != nil {
			return execErr
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("staged action %d changed state during apply", id)
		}
		if syncUpdateFn != nil {
			if updateErr := syncUpdateFn(ctx, tx); updateErr != nil {
				log.Printf("apply staged action %d: update synced_media failed: %v", id, updateErr)
			}
		}
		return nil
	})
}

func (o *Orchestrator) validateEpisodeDeletion(ctx context.Context, seriesID int, episodeIDs, fileIDs []int, requireUnmonitored bool) ([]int, error) {
	if len(episodeIDs) == 0 {
		return nil, errors.New("staged deletion has no episode scope; reject it and sync again")
	}
	series, err := o.sonarr.GetSeriesByID(ctx, seriesID)
	if err != nil {
		return nil, fmt.Errorf("validate deletion series: %w", err)
	}
	managed, err := o.hasManagedTag(ctx, o.sonarr.Client, series.Tags)
	if err != nil {
		return nil, fmt.Errorf("validate series managed tag: %w", err)
	}
	if !managed {
		return nil, errors.New("series no longer has the managed tag")
	}
	episodes, err := o.sonarr.GetEpisodes(ctx, seriesID)
	if err != nil {
		return nil, fmt.Errorf("validate deletion episodes: %w", err)
	}
	var presentFileIDs []int
	for _, ep := range episodes {
		if !containsInt(fileIDs, ep.EpisodeFileID) {
			continue
		}
		if !containsInt(episodeIDs, ep.ID) {
			return nil, fmt.Errorf("file %d includes an episode outside the approved scope", ep.EpisodeFileID)
		}
		if requireUnmonitored && ep.Monitored {
			return nil, errors.New("unmonitor episodes before applying file deletion")
		}
		if ep.HasFile && !containsInt(presentFileIDs, ep.EpisodeFileID) {
			presentFileIDs = append(presentFileIDs, ep.EpisodeFileID)
		}
	}
	return presentFileIDs, nil
}

// RejectStagedAction marks a pending staged action as rejected.
func (o *Orchestrator) RejectStagedAction(ctx context.Context, id int) error {
	_, err := o.RejectStagedActions(ctx, []int{id})
	return err
}

// RejectStagedActions rejects a batch atomically while reserving Servarr work.
func (o *Orchestrator) RejectStagedActions(ctx context.Context, ids []int) ([]int, error) {
	if !o.workMu.TryLock() {
		return nil, ErrSyncInProgress
	}
	defer o.workMu.Unlock()
	err := o.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, id := range ids {
			if err := rejectStagedAction(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return append([]int{}, ids...), nil
}

func rejectStagedAction(ctx context.Context, tx *sql.Tx, id int) error {
	res, execErr := tx.ExecContext(ctx, `
			UPDATE staged_sync_actions
			SET status = 'REJECTED'
			WHERE id = ? AND status = 'PENDING';
		`, id)
	if execErr != nil {
		return execErr
	}
	rows, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return rowsErr
	}
	if rows == 0 {
		return fmt.Errorf("staged action %d not found or not pending", id)
	}
	return nil
}

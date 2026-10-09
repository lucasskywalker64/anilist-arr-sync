package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
)

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

// MonitorSeasonPayload stores parameters for monitoring or unmonitoring a season or episode slice.
type MonitorSeasonPayload struct {
	SeriesID     int   `json:"seriesId"`
	TVDBID       int   `json:"tvdbId"`
	SeasonNumber int   `json:"seasonNumber"`
	EpisodeIDs   []int `json:"episodeIds,omitempty"`
}

// UnmonitorMoviePayload stores parameters for unmonitoring a movie in Radarr.
type UnmonitorMoviePayload struct {
	MovieID int    `json:"movieId"`
	TMDBID  int    `json:"tmdbId"`
	Title   string `json:"title"`
}

// stageAction persists a planned Servarr action into staged_sync_actions.
// It skips duplicate pending actions with identical type, target service, and payload.
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

	return o.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var exists int
		checkErr := tx.QueryRowContext(ctx, `
			SELECT 1 FROM staged_sync_actions
			WHERE action_type = ? AND target_service = ? AND payload_json = ? AND status = 'PENDING'
			LIMIT 1;
		`, string(actionType), targetService, string(payloadBytes)).Scan(&exists)
		if checkErr == nil {
			return nil
		} else if !errors.Is(checkErr, sql.ErrNoRows) {
			return checkErr
		}

		_, execErr := tx.ExecContext(ctx, `
			INSERT INTO staged_sync_actions (
				action_type, media_type, title, target_service, payload_json, status
			)
			VALUES (?, ?, ?, ?, ?, 'PENDING');
		`, string(actionType), mediaType, title, targetService, string(payloadBytes))
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
	var action StagedAction
	var executedAt sql.NullTime
	err := o.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		row := q.QueryRowContext(ctx, `
			SELECT id, run_id, action_type, media_type, title, target_service, payload_json, status, executed_at, created_at
			FROM staged_sync_actions
			WHERE id = ? AND status = 'PENDING';
		`, id)
		return row.Scan(
			&action.ID, &action.RunID, &action.ActionType, &action.MediaType, &action.Title,
			&action.TargetService, &action.PayloadJSON, &action.Status, &executedAt, &action.CreatedAt,
		)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("staged action %d not found or not pending", id)
		}
		return fmt.Errorf("query staged action %d: %w", id, err)
	}

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

	case ActionAddSeries:
		var req servarr.AddSeriesRequest
		if unmarshalErr := json.Unmarshal([]byte(action.PayloadJSON), &req); unmarshalErr != nil {
			return fmt.Errorf("unmarshal add series payload: %w", unmarshalErr)
		}
		if o.sonarr == nil {
			return errors.New("sonarr client is nil")
		}
		if len(req.Tags) == 0 && o.cfg.TagName != "" {
			if tagID, tagErr := o.sonarr.EnsureTag(ctx, o.cfg.TagName); tagErr == nil {
				req.Tags = []int{tagID}
			}
		}
		_, addErr := o.sonarr.AddSeries(ctx, req)
		if addErr != nil {
			return fmt.Errorf("execute add series: %w", addErr)
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
			if monErr := o.sonarr.MonitorEpisodes(ctx, payload.EpisodeIDs, true); monErr != nil {
				return fmt.Errorf("execute monitor episodes: %w", monErr)
			}
		} else {
			series, getErr := o.sonarr.GetSeriesByID(ctx, payload.SeriesID)
			if getErr != nil {
				return fmt.Errorf("get series %d: %w", payload.SeriesID, getErr)
			}
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
		if o.cfg.SonarrSearchOnAdd && payload.SeriesID > 0 && payload.SeasonNumber > 0 {
			_, _ = o.sonarr.SearchSeason(ctx, payload.SeriesID, payload.SeasonNumber)
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
		return nil
	})
}

// RejectStagedAction marks a pending staged action as rejected.
func (o *Orchestrator) RejectStagedAction(ctx context.Context, id int) error {
	return o.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
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
	})
}

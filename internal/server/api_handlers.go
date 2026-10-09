package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

// MappingOverride represents a row in mapping_overrides table.
type MappingOverride struct {
	AniListID     int       `json:"anilistId"`
	MediaType     string    `json:"mediaType"`
	TVDBID        *int      `json:"tvdbId,omitempty"`
	TMDBID        *int      `json:"tmdbId,omitempty"`
	Seasons       string    `json:"seasons"`
	TitleOverride *string   `json:"titleOverride,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type createOverrideRequest struct {
	AniListID     int    `json:"anilistId"`
	MediaType     string `json:"mediaType"`
	TVDBID        *int   `json:"tvdbId"`
	TMDBID        *int   `json:"tmdbId"`
	Seasons       string `json:"seasons"`
	TitleOverride string `json:"titleOverride"`
}

func (s *Server) handleListOverrides(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	var list []MappingOverride
	err := s.db.Read(r.Context(), func(ctx context.Context, q storage.Querier) error {
		rows, err := q.QueryContext(ctx, `
			SELECT anilist_id, media_type, tvdb_id, tmdb_id, seasons, title_override, created_at, updated_at
			FROM mapping_overrides
			ORDER BY anilist_id ASC;
		`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var o MappingOverride
			var tvdb, tmdb sql.NullInt64
			var title sql.NullString
			if scanErr := rows.Scan(
				&o.AniListID, &o.MediaType, &tvdb, &tmdb, &o.Seasons, &title, &o.CreatedAt, &o.UpdatedAt,
			); scanErr != nil {
				return scanErr
			}
			if tvdb.Valid {
				v := int(tvdb.Int64)
				o.TVDBID = &v
			}
			if tmdb.Valid {
				v := int(tmdb.Int64)
				o.TMDBID = &v
			}
			if title.Valid {
				o.TitleOverride = &title.String
			}
			list = append(list, o)
		}
		return rows.Err()
	})
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "query overrides failed: "+err.Error())
		return
	}

	if list == nil {
		list = []MappingOverride{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(list)
}

func (s *Server) handleCreateOverride(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	var req createOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.AniListID <= 0 {
		s.writeJSONError(w, http.StatusBadRequest, "anilistId must be positive integer")
		return
	}

	req.MediaType = strings.ToUpper(strings.TrimSpace(req.MediaType))
	if req.MediaType != "MOVIE" && req.MediaType != "SERIES" {
		s.writeJSONError(w, http.StatusBadRequest, "mediaType must be MOVIE or SERIES")
		return
	}

	seasons := strings.TrimSpace(req.Seasons)
	if seasons == "" {
		seasons = "1"
	}

	err := s.db.Write(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, execErr := tx.ExecContext(ctx, `
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
		`, req.AniListID, req.MediaType, req.TVDBID, req.TMDBID, seasons, req.TitleOverride)
		return execErr
	})
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "save override failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"anilistId": req.AniListID,
	})
}

func (s *Server) handleDeleteOverride(w http.ResponseWriter, r *http.Request) {
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
		_, execErr := tx.ExecContext(ctx, "DELETE FROM mapping_overrides WHERE anilist_id = ?;", id)
		return execErr
	})
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "delete override failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "deleted",
		"anilistId": id,
	})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	sanitized := *s.cfg
	if sanitized.APIKey != "" {
		sanitized.APIKey = "******"
	}
	if sanitized.RadarrAPIKey != "" {
		sanitized.RadarrAPIKey = "******"
	}
	if sanitized.SonarrAPIKey != "" {
		sanitized.SonarrAPIKey = "******"
	}

	// serialize to JSON map
	m := map[string]any{
		"bindAddress":            sanitized.BindAddress,
		"port":                   sanitized.Port,
		"urlBase":                sanitized.URLBase,
		"enableSsl":              sanitized.EnableSsl,
		"sslPort":                sanitized.SslPort,
		"authenticationMethod":   sanitized.AuthenticationMethod,
		"authenticationRequired": sanitized.AuthenticationRequired,
		"apiKey":                 sanitized.APIKey,
		"logLevel":               sanitized.LogLevel,
		"consoleLogLevel":        sanitized.ConsoleLogLevel,
		"instanceName":           sanitized.InstanceName,
		"databasePath":           sanitized.DatabasePath,
		"aniListUsername":        sanitized.AniListUsername,
		"radarrUrl":              sanitized.RadarrURL,
		"sonarrUrl":              sanitized.SonarrURL,
		"tagName":                sanitized.TagName,
		"syncIntervalHours":      sanitized.SyncIntervalHours,
		"dateToleranceDays":      sanitized.DateToleranceDays,
		"confidenceThreshold":    sanitized.ConfidenceThreshold,
		"unmonitorDropped":       sanitized.UnmonitorDropped,
		"firstRunDryRun":         sanitized.FirstRunDryRun,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(m)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Apply known fields
	for k, v := range updates {
		switch strings.ToLower(k) {
		case "instancename":
			if str, ok := v.(string); ok {
				s.cfg.InstanceName = str
			}
		case "syncintervalhours":
			if num, ok := v.(float64); ok && num > 0 {
				s.cfg.SyncIntervalHours = int(num)
			}
		case "datetolerancedays":
			if num, ok := v.(float64); ok && num >= 0 {
				s.cfg.DateToleranceDays = int(num)
			}
		case "confidencethreshold":
			if num, ok := v.(float64); ok && num >= 0 && num <= 1 {
				s.cfg.ConfidenceThreshold = num
			}
		case "unmonitordropped":
			if b, ok := v.(bool); ok {
				s.cfg.UnmonitorDropped = b
			}
		case "firstrundryrun":
			if b, ok := v.(bool); ok {
				s.cfg.FirstRunDryRun = b
			}
		case "tagname":
			if str, ok := v.(string); ok {
				s.cfg.TagName = str
			}
		case "anilistusername":
			if str, ok := v.(string); ok {
				s.cfg.AniListUsername = str
			}
		case "radarrurl":
			if str, ok := v.(string); ok {
				s.cfg.RadarrURL = str
			}
		case "sonarrurl":
			if str, ok := v.(string); ok {
				s.cfg.SonarrURL = str
			}
		case "radarrapikey":
			if str, ok := v.(string); ok && str != "******" {
				s.cfg.RadarrAPIKey = str
			}
		case "sonarrapikey":
			if str, ok := v.(string); ok && str != "******" {
				s.cfg.SonarrAPIKey = str
			}
		case "apikey":
			if str, ok := v.(string); ok && str != "******" {
				s.cfg.APIKey = str
			}
		}
	}

	if err := s.cfg.Validate(); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid configuration: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"message": "configuration updated",
	})
}

func (s *Server) handleStartSync(w http.ResponseWriter, _ *http.Request) {
	if s.orch == nil {
		s.writeJSONError(w, http.StatusBadRequest, "orchestrator not configured")
		return
	}

	s.syncMu.Lock()
	if s.isSyncing {
		s.syncMu.Unlock()
		s.writeJSONError(w, http.StatusConflict, "sync is already in progress")
		return
	}
	s.isSyncing = true
	s.syncMu.Unlock()

	go func() {
		defer func() {
			s.syncMu.Lock()
			s.isSyncing = false
			s.syncMu.Unlock()
		}()

		report, _ := s.orch.Sync(context.Background(), orchestrator.TriggerManualWeb)
		if report != nil {
			s.syncMu.Lock()
			s.lastSyncReport = report
			s.syncMu.Unlock()
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "started",
		"trigger": orchestrator.TriggerManualWeb,
	})
}

func (s *Server) handleGetSyncStatus(w http.ResponseWriter, _ *http.Request) {
	s.syncMu.Lock()
	running := s.isSyncing
	lastReport := s.lastSyncReport
	s.syncMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"running":    running,
		"lastReport": lastReport,
	})
}

// SyncHistoryRecord represents a historical synchronization run log.
type SyncHistoryRecord struct {
	ID                int       `json:"id"`
	RunTimestamp      time.Time `json:"runTimestamp"`
	DurationMs        int64     `json:"durationMs"`
	Status            string    `json:"status"`
	ItemsScanned      int       `json:"itemsScanned"`
	AddedRadarr       int       `json:"addedRadarr"`
	MonitoredSonarr   int       `json:"monitoredSonarr"`
	UnmonitoredSonarr int       `json:"unmonitoredSonarr"`
	UnmonitoredRadarr int       `json:"unmonitoredRadarr"`
	QueuedReview      int       `json:"queuedReview"`
	ErrorsJSON        *string   `json:"errorsJson,omitempty"`
	TriggerType       string    `json:"triggerType"`
}

func (s *Server) handleGetSyncHistory(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	var history []SyncHistoryRecord
	err := s.db.Read(r.Context(), func(ctx context.Context, q storage.Querier) error {
		rows, err := q.QueryContext(ctx, `
			SELECT id, run_timestamp, duration_ms, status, items_scanned, added_radarr,
			       monitored_sonarr, unmonitored_sonarr, unmonitored_radarr, queued_review,
			       errors_json, trigger_type
			FROM sync_history
			ORDER BY id DESC
			LIMIT 50;
		`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var rec SyncHistoryRecord
			var errs sql.NullString
			if scanErr := rows.Scan(
				&rec.ID, &rec.RunTimestamp, &rec.DurationMs, &rec.Status, &rec.ItemsScanned,
				&rec.AddedRadarr, &rec.MonitoredSonarr, &rec.UnmonitoredSonarr, &rec.UnmonitoredRadarr,
				&rec.QueuedReview, &errs, &rec.TriggerType,
			); scanErr != nil {
				return scanErr
			}
			if errs.Valid {
				rec.ErrorsJSON = &errs.String
			}
			history = append(history, rec)
		}
		return rows.Err()
	})
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "query sync history failed: "+err.Error())
		return
	}

	if history == nil {
		history = []SyncHistoryRecord{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(history)
}

func (s *Server) handleListStaged(w http.ResponseWriter, r *http.Request) {
	if s.orch != nil {
		actions, err := s.orch.GetStagedActions(r.Context(), orchestrator.StagedStatusPending)
		if err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, "query staged actions failed: "+err.Error())
			return
		}
		if actions == nil {
			actions = []orchestrator.StagedAction{}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(actions)
		return
	}

	if s.db == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "database not configured")
		return
	}

	var actions []map[string]any
	err := s.db.Read(r.Context(), func(ctx context.Context, q storage.Querier) error {
		rows, err := q.QueryContext(ctx, `
			SELECT id, run_id, action_type, media_type, title, target_service, payload_json, status, created_at
			FROM staged_sync_actions
			WHERE status = 'PENDING'
			ORDER BY id ASC;
		`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var id int
			var runID sql.NullInt64
			var actionType, mediaType, title, targetService, payload, status string
			var createdAt time.Time
			if scanErr := rows.Scan(&id, &runID, &actionType, &mediaType, &title, &targetService, &payload, &status, &createdAt); scanErr != nil {
				return scanErr
			}
			actions = append(actions, map[string]any{
				"id":            id,
				"actionType":    actionType,
				"mediaType":     mediaType,
				"title":         title,
				"targetService": targetService,
				"payloadJson":   payload,
				"status":        status,
				"createdAt":     createdAt,
			})
		}
		return rows.Err()
	})
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "query staged actions failed: "+err.Error())
		return
	}

	if actions == nil {
		actions = []map[string]any{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(actions)
}

type batchStagedRequest struct {
	IDs []int `json:"ids"`
}

func (s *Server) handleApplyStaged(w http.ResponseWriter, r *http.Request) {
	var req batchStagedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var applied []int
	for _, id := range req.IDs {
		if s.orch != nil {
			if err := s.orch.ApplyStagedAction(r.Context(), id); err != nil {
				s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("apply staged action %d failed: %v", id, err))
				return
			}
			applied = append(applied, id)
		} else if s.db != nil {
			err := s.db.Write(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
				_, execErr := tx.ExecContext(ctx, "UPDATE staged_sync_actions SET status = 'APPLIED', executed_at = CURRENT_TIMESTAMP WHERE id = ?;", id)
				return execErr
			})
			if err != nil {
				s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("apply staged action %d failed: %v", id, err))
				return
			}
			applied = append(applied, id)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"applied": applied,
	})
}

func (s *Server) handleRejectStaged(w http.ResponseWriter, r *http.Request) {
	var req batchStagedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var rejected []int
	for _, id := range req.IDs {
		if s.orch != nil {
			if err := s.orch.RejectStagedAction(r.Context(), id); err != nil {
				s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("reject staged action %d failed: %v", id, err))
				return
			}
			rejected = append(rejected, id)
		} else if s.db != nil {
			err := s.db.Write(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
				_, execErr := tx.ExecContext(ctx, "UPDATE staged_sync_actions SET status = 'REJECTED' WHERE id = ?;", id)
				return execErr
			})
			if err != nil {
				s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("reject staged action %d failed: %v", id, err))
				return
			}
			rejected = append(rejected, id)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"rejected": rejected,
	})
}

func (s *Server) handleListBackups(w http.ResponseWriter, _ *http.Request) {
	if s.backup == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]any{})
		return
	}

	backups, err := s.backup.List()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "list backups failed: "+err.Error())
		return
	}
	if backups == nil {
		backups = []backup.Info{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(backups)
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if s.backup == nil {
		s.writeJSONError(w, http.StatusBadRequest, "backup engine not configured")
		return
	}

	info, err := s.backup.Create(r.Context(), backup.TypeManual)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "create backup failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(info)
}

type restoreBackupRequest struct {
	Filename    string `json:"filename"`
	ArchivePath string `json:"archivePath"`
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if s.backup == nil {
		s.writeJSONError(w, http.StatusBadRequest, "backup engine not configured")
		return
	}

	var req restoreBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	archivePath := req.ArchivePath
	if archivePath == "" && req.Filename != "" {
		backups, err := s.backup.List()
		if err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, "failed to locate backup: "+err.Error())
			return
		}
		for _, b := range backups {
			if b.Filename == req.Filename || filepath.Base(b.Path) == req.Filename {
				archivePath = b.Path
				break
			}
		}
	}

	if archivePath == "" {
		s.writeJSONError(w, http.StatusBadRequest, "backup archive not found")
		return
	}

	if err := s.backup.Restore(r.Context(), archivePath); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "restore failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "restored",
		"message": "backup restored successfully",
	})
}

func (s *Server) handleGetUpdateStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"currentVersion": "1.0.0",
		"latestVersion":  "1.0.0",
		"canUpdate":      false,
	})
}

func (s *Server) handleApplyUpdate(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "up to date",
		"message": "no newer version available",
	})
}

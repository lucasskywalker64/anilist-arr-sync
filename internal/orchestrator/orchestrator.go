// Package orchestrator coordinates AniList watchlist synchronization with Sonarr
// and Radarr, supporting selective season monitoring, dry-run staged actions,
// and execution metrics tracking.
package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/domain/router"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/domain/season"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/resolver"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

// TriggerType indicates how a synchronization run was initiated.
type TriggerType string

// Supported trigger mechanisms for synchronization passes.
const (
	// TriggerScheduled indicates a scheduled background cron pass.
	TriggerScheduled TriggerType = "SCHEDULED"
	// TriggerManualWeb indicates an on-demand trigger initiated from the web dashboard.
	TriggerManualWeb TriggerType = "MANUAL_WEB"
	// TriggerCLI indicates a trigger executed via the command-line interface.
	TriggerCLI TriggerType = "CLI"
)

// SyncStatus indicates the overall result of a synchronization pass.
type SyncStatus string

// Supported outcome statuses for synchronization passes.
const (
	// SyncStatusSuccess indicates all entries processed without uncaught errors.
	SyncStatusSuccess SyncStatus = "SUCCESS"
	// SyncStatusPartial indicates some entries encountered errors while others succeeded.
	SyncStatusPartial SyncStatus = "PARTIAL"
	// SyncStatusFailed indicates the synchronization pass could not complete.
	SyncStatusFailed SyncStatus = "FAILED"
)

// SyncReport summarizes the results of a completed synchronization pass.
type SyncReport struct {
	RunID             int64       `json:"runId"`
	DurationMs        int64       `json:"durationMs"`
	Status            SyncStatus  `json:"status"`
	ItemsScanned      int         `json:"itemsScanned"`
	AddedRadarr       int         `json:"addedRadarr"`
	MonitoredSonarr   int         `json:"monitoredSonarr"`
	UnmonitoredSonarr int         `json:"unmonitoredSonarr"`
	UnmonitoredRadarr int         `json:"unmonitoredRadarr"`
	QueuedReview      int         `json:"queuedReview"`
	Errors            []string    `json:"errors"`
	Trigger           TriggerType `json:"trigger"`
	StartTime         time.Time   `json:"startTime"`
}

// Notifier defines the interface for dispatching notification events.
type Notifier interface {
	Dispatch(event notification.SyncEvent)
}

// Orchestrator coordinates the end-to-end synchronization workflow.
type Orchestrator struct {
	stateMu  sync.RWMutex
	workMu   sync.Mutex
	running  atomic.Bool
	db       *storage.DB
	cfg      *config.Config
	al       *anilist.Client
	sonarr   *servarr.SonarrClient
	radarr   *servarr.RadarrClient
	resolver *resolver.Resolver
	notifier Notifier
}

// ErrSyncInProgress indicates that a sync or staged apply already owns Servarr work.
var ErrSyncInProgress = errors.New("a sync or staged apply is already in progress")

// SetNotifier sets a notification dispatcher on the Orchestrator.
func (o *Orchestrator) SetNotifier(n Notifier) {
	o.stateMu.Lock()
	defer o.stateMu.Unlock()
	o.notifier = n
}

// UpdateConfig installs an immutable configuration snapshot for future runs.
func (o *Orchestrator) UpdateConfig(cfg config.Config) {
	o.stateMu.Lock()
	defer o.stateMu.Unlock()
	o.cfg = &cfg
}

// UpdateServarrConfig installs the configuration and its clients together for future work.
func (o *Orchestrator) UpdateServarrConfig(cfg config.Config, sonarr *servarr.SonarrClient, radarr *servarr.RadarrClient) {
	o.stateMu.Lock()
	defer o.stateMu.Unlock()
	o.cfg, o.sonarr, o.radarr = &cfg, sonarr, radarr
}

func (o *Orchestrator) snapshot() *Orchestrator {
	o.stateMu.RLock()
	defer o.stateMu.RUnlock()
	return &Orchestrator{
		db: o.db, cfg: o.cfg, al: o.al, sonarr: o.sonarr, radarr: o.radarr,
		resolver: o.resolver, notifier: o.notifier,
	}
}

// New creates an Orchestrator instance.
func New(
	db *storage.DB,
	cfg *config.Config,
	al *anilist.Client,
	sonarr *servarr.SonarrClient,
	radarr *servarr.RadarrClient,
) *Orchestrator {
	cfgCopy := *cfg
	return &Orchestrator{
		db:       db,
		cfg:      &cfgCopy,
		al:       al,
		sonarr:   sonarr,
		radarr:   radarr,
		resolver: resolver.New(db),
	}
}

// Sync executes the core synchronization loop following the four-step sequence.
func (o *Orchestrator) Sync(ctx context.Context, trigger TriggerType) (*SyncReport, error) {
	if !o.workMu.TryLock() {
		return nil, ErrSyncInProgress
	}
	o.running.Store(true)
	return o.runSync(ctx, trigger, false)
}

// SyncResult contains the outcome of an asynchronous synchronization run.
type SyncResult struct {
	Report *SyncReport
	Err    error
}

// StartSync reserves execution before returning, then runs asynchronously.
// The buffered result channel never blocks completion if the caller goes away.
func (o *Orchestrator) StartSync(ctx context.Context, trigger TriggerType) (<-chan SyncResult, error) {
	return o.startSync(ctx, trigger, false)
}

// StartDryRun starts a sync that stages changes regardless of the saved preference.
func (o *Orchestrator) StartDryRun(ctx context.Context, trigger TriggerType) (<-chan SyncResult, error) {
	return o.startSync(ctx, trigger, true)
}

func (o *Orchestrator) startSync(ctx context.Context, trigger TriggerType, dryRun bool) (<-chan SyncResult, error) {
	if !o.workMu.TryLock() {
		return nil, ErrSyncInProgress
	}
	o.running.Store(true)
	result := make(chan SyncResult, 1)
	go func() {
		defer close(result)
		report, err := o.runSync(ctx, trigger, dryRun)
		result <- SyncResult{Report: report, Err: err}
	}()
	return result, nil
}

// IsSyncing reports whether any trigger currently owns synchronization.
func (o *Orchestrator) IsSyncing() bool {
	return o.running.Load()
}

func (o *Orchestrator) runSync(ctx context.Context, trigger TriggerType, dryRun bool) (*SyncReport, error) {
	defer o.workMu.Unlock()
	defer o.running.Store(false)
	run := o.snapshot()
	if dryRun {
		cfg := *run.cfg
		cfg.FirstRunDryRun = true
		run.cfg = &cfg
	}
	return run.sync(ctx, trigger)
}

func (o *Orchestrator) sync(ctx context.Context, trigger TriggerType) (*SyncReport, error) {
	startTime := time.Now()
	report := &SyncReport{
		Trigger: trigger,
		Status:  SyncStatusSuccess,
		Errors:  make([]string, 0),
	}

	// Purge staged actions older than 7 days
	_, _ = o.PurgeExpiredStagedActions(ctx, 7*24*time.Hour)

	// Step 1: Load ignored AniList IDs into set
	ignoredSet := make(map[int]bool)
	err := o.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		rows, qErr := q.QueryContext(ctx, "SELECT anilist_id FROM ignored_titles;")
		if qErr != nil {
			return qErr
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var id int
			if scanErr := rows.Scan(&id); scanErr != nil {
				return scanErr
			}
			ignoredSet[id] = true
		}
		return rows.Err()
	})
	if err != nil {
		report.Status = SyncStatusFailed
		report.Errors = append(report.Errors, fmt.Sprintf("failed to load ignored titles: %v", err))
		o.finishSync(ctx, report, startTime)
		return report, fmt.Errorf("load ignored titles: %w", err)
	}

	// Reconcile synced_media status against user's complete AniList collection overview
	if recErr := o.reconcileWatchlistStatus(ctx); recErr != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("reconcile watchlist: %v", recErr))
	}

	// Fetch AniList watchlist
	seriesStatuses := anilist.ParseListStatuses(o.cfg.AniListTvStatus)
	movieStatuses := anilist.ParseListStatuses(o.cfg.AniListMovieStatus)
	filter := anilist.WatchlistFilter{
		Username:          o.cfg.AniListUsername,
		Statuses:          o.configuredStatuses(seriesStatuses, movieStatuses),
		IncludeUnreleased: o.cfg.AniListIncludeUnreleased,
	}

	entries, err := o.al.FetchWatchlist(ctx, filter)
	if err != nil {
		report.Status = SyncStatusFailed
		report.Errors = append(report.Errors, fmt.Sprintf("failed to fetch AniList watchlist: %v", err))
		o.finishSync(ctx, report, startTime)
		return report, fmt.Errorf("fetch anilist watchlist: %w", err)
	}

	report.ItemsScanned = len(entries)

	for _, entry := range entries {
		// Step 1: Discard non-video entries immediately using router.Route without querying database
		normalizedFormat := router.MediaFormat(strings.ToUpper(strings.TrimSpace(entry.Media.Format)))
		decision := router.Route(normalizedFormat)
		if decision == router.RouteDiscard {
			continue
		}

		mediaType := "SERIES"
		allowedStatuses := seriesStatuses
		if decision == router.RouteRadarr {
			mediaType = "MOVIE"
			allowedStatuses = movieStatuses
		}
		status := entry.Status
		if status == "" {
			status = anilist.StatusCurrent
		}
		selected := slices.Contains(allowedStatuses, status)
		if !selected && !o.isCleanupStatus(status) {
			continue
		}

		// Step 2: Skip titles present in ignored_titles table before resolution
		if ignoredSet[entry.Media.ID] {
			if err := o.upsertSyncedMedia(ctx, entry, mediaType, "NONE", nil, nil, "", "IGNORED", "", false); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("upsert synced media %d: %v", entry.Media.ID, err))
			}
			continue
		}

		// Step 3 & 4: Check established database mappings and divert unmapped entries
		var res *resolver.Result
		var resErr error
		if !selected || status == anilist.StatusDropped {
			res, resErr = o.resolver.ResolveForCleanup(ctx, entry.Media)
		} else {
			res, resErr = o.resolver.Resolve(ctx, entry.Media)
		}
		if resErr != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("resolve media %d: %v", entry.Media.ID, resErr))
			continue
		}

		if res.Diverted {
			report.QueuedReview++
			if err := o.upsertSyncedMedia(ctx, entry, mediaType, "NONE", nil, nil, "", "PENDING_REVIEW", "", false); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("upsert synced media %d: %v", entry.Media.ID, err))
			}
			if o.notifier != nil {
				title := entry.Media.Title.Romaji
				if title == "" {
					title = entry.Media.Title.UserPreferred
				}
				if title == "" {
					title = entry.Media.Title.English
				}
				o.notifier.Dispatch(notification.SyncEvent{
					Type:      notification.EventReviewRequired,
					Timestamp: time.Now().UTC(),
					Title:     title,
					MediaType: entry.Media.Format,
					AniListID: entry.Media.ID,
					Reason:    res.DivertReason,
					PosterURL: entry.Media.CoverImageURL(),
				})
			}
			continue
		}

		if !res.Resolved {
			continue
		}

		// Dispatch to Servarr service and record to synced_media
		switch res.TargetService {
		case "RADARR":
			outcome := o.dispatchRadarr(ctx, entry, res, report)
			if outcome.shouldUpsert {
				if err := o.upsertSyncedMedia(ctx, entry, "MOVIE", "RADARR", nil, &res.TargetID, res.TitleOverride, outcome.monitoredStatus, "", false); err != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("upsert synced media %d: %v", entry.Media.ID, err))
				}
			}
		case "SONARR":
			outcome := o.dispatchSonarr(ctx, entry, res, report)
			if outcome.shouldUpsert {
				seasonsStr := ""
				if outcome.season >= 0 {
					seasonsStr = strconv.Itoa(outcome.season)
				}
				if err := o.upsertSyncedMedia(ctx, entry, "SERIES", "SONARR", &res.TargetID, nil, res.TitleOverride, outcome.monitoredStatus, seasonsStr, outcome.pendingSonarrFollowup); err != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("upsert synced media %d: %v", entry.Media.ID, err))
				}
			}
		}
	}

	if len(report.Errors) > 0 {
		if report.ItemsScanned > 0 && len(report.Errors) < report.ItemsScanned {
			report.Status = SyncStatusPartial
		} else {
			report.Status = SyncStatusFailed
		}
	}

	o.finishSync(ctx, report, startTime)
	return report, nil
}

func (o *Orchestrator) finishSync(ctx context.Context, report *SyncReport, startTime time.Time) {
	o.recordHistory(ctx, report, startTime)

	if o.notifier != nil {
		if report.Status == SyncStatusFailed || len(report.Errors) > 0 {
			o.notifier.Dispatch(notification.SyncEvent{
				Type:      notification.EventSyncError,
				Timestamp: time.Now().UTC(),
				Error:     strings.Join(report.Errors, "; "),
			})
		}
		o.notifier.Dispatch(notification.SyncEvent{
			Type:              notification.EventSyncComplete,
			Timestamp:         time.Now().UTC(),
			DurationMs:        report.DurationMs,
			ItemsScanned:      report.ItemsScanned,
			AddedRadarr:       report.AddedRadarr,
			MonitoredSonarr:   report.MonitoredSonarr,
			UnmonitoredSonarr: report.UnmonitoredSonarr,
			UnmonitoredRadarr: report.UnmonitoredRadarr,
			QueuedReview:      report.QueuedReview,
		})
	}
}

type dispatchOutcome struct {
	monitoredStatus       string
	season                int
	shouldUpsert          bool
	pendingSonarrFollowup bool
}

func (o *Orchestrator) dispatchRadarr(ctx context.Context, entry anilist.MediaListEntry, res *resolver.Result, report *SyncReport) dispatchOutcome {
	if o.radarr == nil {
		return dispatchOutcome{shouldUpsert: false}
	}

	title := entry.Media.Title.Romaji
	if title == "" {
		title = entry.Media.Title.UserPreferred
	}
	if title == "" {
		title = entry.Media.Title.English
	}
	if res.TitleOverride != "" {
		title = res.TitleOverride
	}

	movie, lookupErr := o.radarr.LookupMovieByTMDBID(ctx, res.TargetID)
	if lookupErr != nil && !errors.Is(lookupErr, servarr.ErrNotFound) {
		report.Errors = append(report.Errors, fmt.Sprintf("lookup movie tmdb %d: %v", res.TargetID, lookupErr))
		return dispatchOutcome{shouldUpsert: false}
	}

	movieExists := lookupErr == nil && movie != nil && movie.ID > 0

	if movieExists {
		isDropped := entry.Status == anilist.StatusDropped
		isCompleted := entry.Status == anilist.StatusCompleted

		if isDropped || isCompleted {
			unmonitorEnabled := (isDropped && o.cfg.UnmonitorDropped) || (isCompleted && o.cfg.UnmonitorCompleted)
			deleteEnabled := (isDropped && o.cfg.DeleteDropped) || (isCompleted && o.cfg.DeleteCompleted)

			hasManagedTag, tagErr := o.hasManagedTag(ctx, o.radarr.Client, movie.Tags)
			if tagErr != nil && (unmonitorEnabled || deleteEnabled) {
				report.Errors = append(report.Errors, fmt.Sprintf("check movie %d managed tag: %v", movie.ID, tagErr))
				return dispatchOutcome{shouldUpsert: false}
			}

			if hasManagedTag {
				// Step 1: Unmonitor first if enabled
				if unmonitorEnabled && movie.Monitored {
					if o.cfg.FirstRunDryRun {
						if err := o.stageAction(ctx, ActionUnmonitorMovie, "MOVIE", title, "RADARR", UnmonitorMoviePayload{
							MovieID: movie.ID,
							TMDBID:  movie.TMDBID,
							Title:   movie.Title,
						}); err != nil {
							report.Errors = append(report.Errors, fmt.Sprintf("stage unmonitor movie %d: %v", movie.ID, err))
							return dispatchOutcome{shouldUpsert: false}
						}
					} else {
						movie.Monitored = false
						if _, updErr := o.radarr.UpdateMovie(ctx, movie); updErr != nil {
							report.Errors = append(report.Errors, fmt.Sprintf("unmonitor movie %d: %v", movie.ID, updErr))
							return dispatchOutcome{shouldUpsert: false}
						}
						report.UnmonitoredRadarr++
					}
				}

				// Step 2: Delete file if enabled (after unmonitoring)
				if deleteEnabled && movie.HasFile && movie.MovieFileID > 0 {
					if o.cfg.FirstRunDryRun {
						if err := o.stageAction(ctx, ActionDeleteFile, "MOVIE", title, "RADARR", DeleteFilePayload{
							MovieID:            movie.ID,
							MovieFileID:        movie.MovieFileID,
							Title:              movie.Title,
							RequireUnmonitored: &unmonitorEnabled,
						}); err != nil {
							report.Errors = append(report.Errors, fmt.Sprintf("stage delete movie file %d: %v", movie.MovieFileID, err))
							return dispatchOutcome{shouldUpsert: false}
						}
					} else {
						if delErr := o.radarr.DeleteMovieFile(ctx, movie.MovieFileID); delErr != nil && !errors.Is(delErr, servarr.ErrNotFound) {
							report.Errors = append(report.Errors, fmt.Sprintf("delete movie file %d: %v", movie.MovieFileID, delErr))
						}
					}
				}

				monStatus := "MONITORED"
				if !movie.Monitored || (unmonitorEnabled && !o.cfg.FirstRunDryRun) {
					monStatus = "UNMONITORED"
				}
				return dispatchOutcome{monitoredStatus: monStatus, shouldUpsert: true}
			}

			monStatus := "MONITORED"
			if !movie.Monitored {
				monStatus = "UNMONITORED"
			}
			return dispatchOutcome{monitoredStatus: monStatus, shouldUpsert: true}
		}

		monStatus := "MONITORED"
		if !movie.Monitored {
			monStatus = "UNMONITORED"
		}
		return dispatchOutcome{monitoredStatus: monStatus, shouldUpsert: true}
	}

	// Movie is not in Radarr library
	if entry.Status == anilist.StatusDropped || (entry.Status == anilist.StatusCompleted &&
		(o.cleanupOnly(entry) || o.cfg.UnmonitorCompleted || o.cfg.DeleteCompleted)) {
		return dispatchOutcome{shouldUpsert: false}
	}

	var tags []int
	if !o.cfg.FirstRunDryRun && o.cfg.TagName != "" {
		if tagID, tagErr := o.radarr.EnsureTag(ctx, o.cfg.TagName); tagErr == nil {
			tags = []int{tagID}
		}
	}

	year := 0
	if entry.Media.StartDate.Year != nil {
		year = *entry.Media.StartDate.Year
	}

	addReq := servarr.AddMovieRequest{
		Title:               title,
		TMDBID:              res.TargetID,
		Year:                year,
		QualityProfileID:    o.cfg.RadarrQualityProfileID,
		RootFolderPath:      o.cfg.RadarrRootFolderPath,
		Monitored:           o.cfg.RadarrMonitorOnAdd,
		MinimumAvailability: "announced",
		Tags:                tags,
		AddOptions: &servarr.AddMovieOptions{
			SearchForMovie: o.cfg.RadarrMonitorOnAdd && o.cfg.RadarrSearchOnAdd,
		},
	}

	if o.cfg.FirstRunDryRun {
		if err := o.stageAction(ctx, ActionAddMovie, "MOVIE", title, "RADARR", addReq); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("stage add movie %q: %v", title, err))
			return dispatchOutcome{shouldUpsert: false}
		}
		return dispatchOutcome{monitoredStatus: "UNMONITORED", shouldUpsert: true}
	}

	_, addErr := o.radarr.AddMovie(ctx, addReq)
	if addErr != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("add movie %q: %v", title, addErr))
		return dispatchOutcome{shouldUpsert: false}
	}

	report.AddedRadarr++
	if o.notifier != nil {
		o.notifier.Dispatch(notification.SyncEvent{
			Type:          notification.EventMediaAdded,
			Timestamp:     time.Now().UTC(),
			Title:         title,
			MediaType:     "MOVIE",
			TargetService: "Radarr",
			AniListID:     entry.Media.ID,
			PosterURL:     entry.Media.CoverImageURL(),
		})
	}
	monitoredStatus := "UNMONITORED"
	if addReq.Monitored {
		monitoredStatus = "MONITORED"
	}
	return dispatchOutcome{monitoredStatus: monitoredStatus, shouldUpsert: true}
}

func (o *Orchestrator) dispatchSonarr(ctx context.Context, entry anilist.MediaListEntry, res *resolver.Result, report *SyncReport) dispatchOutcome {
	var pending bool
	if o.db != nil && (entry.Status == "" || entry.Status == anilist.StatusCurrent) {
		err := o.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
			return q.QueryRowContext(ctx, `SELECT sonarr_followup_pending FROM synced_media WHERE anilist_id = ? AND tvdb_id = ? AND target_service = 'SONARR'`, entry.Media.ID, res.TargetID).Scan(&pending)
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			report.Errors = append(report.Errors, fmt.Sprintf("load Sonarr follow-up for %d: %v", entry.Media.ID, err))
			return dispatchOutcome{}
		}
	}
	outcome := o.dispatchSonarrWithDryRun(ctx, entry, res, report, o.cfg.FirstRunDryRun, pending && !o.cfg.FirstRunDryRun)
	if o.cfg.FirstRunDryRun && pending {
		outcome.pendingSonarrFollowup = true
	}
	return outcome
}

func (o *Orchestrator) dispatchSonarrWithDryRun(ctx context.Context, entry anilist.MediaListEntry, res *resolver.Result, report *SyncReport, dryRun, searchAfterPruning bool) (outcome dispatchOutcome) {
	if o.sonarr == nil {
		return dispatchOutcome{shouldUpsert: false}
	}

	title := entry.Media.Title.Romaji
	if title == "" {
		title = entry.Media.Title.UserPreferred
	}
	if title == "" {
		title = entry.Media.Title.English
	}
	if res.TitleOverride != "" {
		title = res.TitleOverride
	}

	targetSeason := res.TVDBSeason
	if targetSeason < 0 {
		targetSeason = 1
	}

	series, lookupErr := o.sonarr.LookupSeriesByTVDBID(ctx, res.TargetID)
	if lookupErr != nil && !errors.Is(lookupErr, servarr.ErrNotFound) {
		report.Errors = append(report.Errors, fmt.Sprintf("lookup series tvdb %d: %v", res.TargetID, lookupErr))
		return dispatchOutcome{shouldUpsert: false}
	}

	seriesExists := lookupErr == nil && series != nil && series.ID > 0

	if !seriesExists {
		if entry.Status == anilist.StatusDropped || (entry.Status == anilist.StatusCompleted &&
			(o.cleanupOnly(entry) || o.cfg.UnmonitorCompleted || o.cfg.DeleteCompleted)) {
			return dispatchOutcome{shouldUpsert: false}
		}
		monitorOnAdd := o.cfg.SonarrMonitorOnAdd
		pruneOnAdd := monitorOnAdd && o.cfg.UnmonitorWatched && entry.Progress > 0 &&
			(entry.Status == "" || entry.Status == anilist.StatusCurrent) && o.cfg.TagName != ""

		var seasons []servarr.Season
		if series != nil && len(series.Seasons) > 0 {
			targetFound := false
			for _, s := range series.Seasons {
				isTarget := s.SeasonNumber == targetSeason
				if isTarget {
					targetFound = true
				}
				seasons = append(seasons, servarr.Season{
					SeasonNumber: s.SeasonNumber,
					Monitored:    monitorOnAdd && isTarget,
				})
			}
			if !targetFound {
				seasons = append(seasons, servarr.Season{
					SeasonNumber: targetSeason,
					Monitored:    monitorOnAdd,
				})
			}
		} else {
			seasons = []servarr.Season{{SeasonNumber: targetSeason, Monitored: monitorOnAdd}}
		}

		var tags []int
		if !dryRun && o.cfg.TagName != "" {
			if tagID, tagErr := o.sonarr.EnsureTag(ctx, o.cfg.TagName); tagErr == nil {
				tags = []int{tagID}
			} else if pruneOnAdd {
				report.Errors = append(report.Errors, fmt.Sprintf("tag new series: %v", tagErr))
				return dispatchOutcome{shouldUpsert: false}
			}
		}

		addReq := servarr.AddSeriesRequest{
			Title:            title,
			TVDBID:           res.TargetID,
			QualityProfileID: o.cfg.SonarrQualityProfileID,
			RootFolderPath:   o.cfg.SonarrRootFolderPath,
			SeriesType:       o.cfg.SonarrSeriesType,
			Monitored:        monitorOnAdd,
			SeasonFolder:     true,
			Seasons:          seasons,
			Tags:             tags,
			AddOptions: &servarr.AddSeriesOptions{
				SearchForMissingEpisodes: monitorOnAdd && o.cfg.SonarrSearchOnAdd && !pruneOnAdd,
			},
		}
		if !monitorOnAdd {
			addReq.AddOptions.Monitor = "none"
		}

		if dryRun {
			payload := AddSeriesPayload{AddSeriesRequest: addReq}
			if pruneOnAdd {
				payload.WatchedEntry = &entry
				payload.TargetSeason = targetSeason
			}
			if err := o.stageAction(ctx, ActionAddSeries, "SERIES", title, "SONARR", payload); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("stage add series %q: %v", title, err))
				return dispatchOutcome{shouldUpsert: false}
			}
			return dispatchOutcome{monitoredStatus: "UNMONITORED", season: targetSeason, shouldUpsert: true}
		}

		var addErr error
		series, addErr = o.sonarr.AddSeries(ctx, addReq)
		if addErr != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("add series %q: %v", title, addErr))
			return dispatchOutcome{shouldUpsert: false}
		}

		monitoredStatus := "UNMONITORED"
		if monitorOnAdd {
			report.MonitoredSonarr++
			monitoredStatus = "MONITORED"
		}
		if o.notifier != nil {
			o.notifier.Dispatch(notification.SyncEvent{
				Type:          notification.EventMediaAdded,
				Timestamp:     time.Now().UTC(),
				Title:         title,
				MediaType:     "SERIES",
				TargetService: "Sonarr",
				AniListID:     entry.Media.ID,
				Season:        &targetSeason,
				PosterURL:     entry.Media.CoverImageURL(),
			})
		}
		if !pruneOnAdd {
			return dispatchOutcome{monitoredStatus: monitoredStatus, season: targetSeason, shouldUpsert: true}
		}
		searchAfterPruning = true
	}

	if searchAfterPruning {
		// Adding succeeded even if Sonarr has not refreshed episodes yet.
		defer func() {
			outcome.shouldUpsert = true
			outcome.season = targetSeason
			outcome.pendingSonarrFollowup = searchAfterPruning
			if outcome.monitoredStatus == "" {
				outcome.monitoredStatus = "UNMONITORED"
				if series.Monitored {
					outcome.monitoredStatus = "MONITORED"
				}
			}
		}()
	}

	// Series already exists in Sonarr library
	episodes, epErr := o.sonarr.GetEpisodes(ctx, series.ID)
	if epErr != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("get episodes series %d: %v", series.ID, epErr))
		return dispatchOutcome{shouldUpsert: false}
	}
	if searchAfterPruning && len(episodes) == 0 {
		return dispatchOutcome{}
	}

	var startDate time.Time
	if entry.Media.StartDate.Year != nil && *entry.Media.StartDate.Year > 0 {
		m := 1
		if entry.Media.StartDate.Month != nil && *entry.Media.StartDate.Month > 0 {
			m = *entry.Media.StartDate.Month
		}
		d := 1
		if entry.Media.StartDate.Day != nil && *entry.Media.StartDate.Day > 0 {
			d = *entry.Media.StartDate.Day
		}
		startDate = time.Date(*entry.Media.StartDate.Year, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	}

	titles := []string{entry.Media.Title.Romaji, entry.Media.Title.English, entry.Media.Title.UserPreferred}
	titles = append(titles, entry.Media.Synonyms...)

	domainEpisodes := make([]season.Episode, len(episodes))
	for i, ep := range episodes {
		pDate, _ := ep.PremiereDate()
		domainEpisodes[i] = season.Episode{
			SeasonNumber:  ep.SeasonNumber,
			EpisodeNumber: ep.EpisodeNumber,
			AirDate:       pDate,
			FinaleType:    ep.FinaleType,
		}
	}

	tolerance := time.Duration(o.cfg.DateToleranceDays) * 24 * time.Hour
	matchRes := season.MatchSeason(startDate, titles, domainEpisodes, tolerance)
	if targetSeason != 0 && matchRes.Season > 0 && !matchRes.Conflict {
		if matchRes.MatchedBy == season.MatchMethodBoth || matchRes.MatchedBy == season.MatchMethodAirDate {
			targetSeason = matchRes.Season
		} else if hasExplicitSeason(titles...) {
			targetSeason = matchRes.Season
		}
	}
	subSeasonNum := extractSubSeasonNumber(titles...)
	sliceEpisodeIDs, isSplitCour := FindSubSeasonEpisodeSlice(episodes, targetSeason, subSeasonNum, startDate, tolerance)
	hasManagedTag := false
	needsManagedTag := searchAfterPruning ||
		(entry.Status == anilist.StatusCompleted && (o.cfg.UnmonitorCompleted || o.cfg.DeleteCompleted)) ||
		(entry.Status == anilist.StatusDropped && (o.cfg.UnmonitorDropped || o.cfg.DeleteDropped)) ||
		((entry.Status == "" || entry.Status == anilist.StatusCurrent) && entry.Progress > 0 && (o.cfg.UnmonitorWatched || o.cfg.DeleteWatched))
	if needsManagedTag {
		var tagErr error
		hasManagedTag, tagErr = o.hasManagedTag(ctx, o.sonarr.Client, series.Tags)
		if tagErr != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("check series %d managed tag: %v", series.ID, tagErr))
			return dispatchOutcome{shouldUpsert: false}
		}
	}
	if hasManagedTag && !isSplitCour && (subSeasonNum > 0 || hasMidseasonBoundary(episodes, targetSeason)) &&
		((entry.Status == anilist.StatusCompleted && (o.cfg.UnmonitorCompleted || o.cfg.DeleteCompleted)) ||
			(entry.Status == anilist.StatusDropped && (o.cfg.UnmonitorDropped || o.cfg.DeleteDropped)) ||
			((entry.Status == "" || entry.Status == anilist.StatusCurrent) && entry.Progress > 0 && (o.cfg.UnmonitorWatched || o.cfg.DeleteWatched))) {
		report.Errors = append(report.Errors, fmt.Sprintf("unresolved cour boundary for series %d season %d", series.ID, targetSeason))
		return dispatchOutcome{shouldUpsert: false}
	}

	isDropped := entry.Status == anilist.StatusDropped
	isCompleted := entry.Status == anilist.StatusCompleted

	if isDropped || isCompleted {
		unmonitorEnabled := (isDropped && o.cfg.UnmonitorDropped) || (isCompleted && o.cfg.UnmonitorCompleted)
		deleteEnabled := (isDropped && o.cfg.DeleteDropped) || (isCompleted && o.cfg.DeleteCompleted)

		if hasManagedTag {
			// Step 1: Unmonitor if enabled
			if unmonitorEnabled {
				if isSplitCour && len(sliceEpisodeIDs) > 0 {
					anyMonitored := false
					for _, ep := range episodes {
						if containsInt(sliceEpisodeIDs, ep.ID) && ep.Monitored {
							anyMonitored = true
							break
						}
					}
					if anyMonitored {
						if dryRun {
							if err := o.stageAction(ctx, ActionUnmonitorSeason, "SERIES", title, "SONARR", MonitorSeasonPayload{
								SeriesID:     series.ID,
								TVDBID:       series.TVDBID,
								SeasonNumber: targetSeason,
								EpisodeIDs:   sliceEpisodeIDs,
								AnilistID:    entry.Media.ID,
							}); err != nil {
								report.Errors = append(report.Errors, fmt.Sprintf("stage unmonitor episodes series %d: %v", series.ID, err))
								return dispatchOutcome{shouldUpsert: false}
							}
						} else {
							if monErr := o.sonarr.MonitorEpisodes(ctx, sliceEpisodeIDs, false); monErr != nil {
								report.Errors = append(report.Errors, fmt.Sprintf("unmonitor episodes series %d: %v", series.ID, monErr))
								return dispatchOutcome{shouldUpsert: false}
							}
							report.UnmonitoredSonarr++
							for i := range episodes {
								if containsInt(sliceEpisodeIDs, episodes[i].ID) {
									episodes[i].Monitored = false
								}
							}
						}
					}
				} else {
					seasonMonitored := false
					for i := range series.Seasons {
						if series.Seasons[i].SeasonNumber == targetSeason && series.Seasons[i].Monitored {
							seasonMonitored = true
							if !dryRun {
								series.Seasons[i].Monitored = false
							}
							break
						}
					}
					if seasonMonitored {
						if dryRun {
							if err := o.stageAction(ctx, ActionUnmonitorSeason, "SERIES", title, "SONARR", MonitorSeasonPayload{
								SeriesID:     series.ID,
								TVDBID:       series.TVDBID,
								SeasonNumber: targetSeason,
								AnilistID:    entry.Media.ID,
							}); err != nil {
								report.Errors = append(report.Errors, fmt.Sprintf("stage unmonitor series %d season %d: %v", series.ID, targetSeason, err))
								return dispatchOutcome{shouldUpsert: false}
							}
						} else {
							if _, updErr := o.sonarr.UpdateSeries(ctx, series); updErr != nil {
								report.Errors = append(report.Errors, fmt.Sprintf("unmonitor series %d season %d: %v", series.ID, targetSeason, updErr))
								return dispatchOutcome{shouldUpsert: false}
							}
							report.UnmonitoredSonarr++
							for i := range episodes {
								if episodes[i].SeasonNumber == targetSeason {
									episodes[i].Monitored = false
								}
							}
						}
					}
				}
			}

			// Step 2: Delete files if enabled (executed after unmonitoring)
			if deleteEnabled {
				var targetEpisodeIDs []int
				if isSplitCour && len(sliceEpisodeIDs) > 0 {
					for _, ep := range episodes {
						if containsInt(sliceEpisodeIDs, ep.ID) {
							targetEpisodeIDs = append(targetEpisodeIDs, ep.ID)
						}
					}
				} else {
					for _, ep := range episodes {
						if ep.SeasonNumber == targetSeason {
							targetEpisodeIDs = append(targetEpisodeIDs, ep.ID)
						}
					}
				}

				targetFileIDs := scopedEpisodeFileIDs(episodes, targetEpisodeIDs)
				if len(targetFileIDs) > 0 {
					if dryRun {
						if err := o.stageAction(ctx, ActionDeleteFile, "SERIES", title, "SONARR", DeleteFilePayload{
							SeriesID:           series.ID,
							EpisodeFileIDs:     targetFileIDs,
							Title:              series.Title,
							EpisodeIDs:         targetEpisodeIDs,
							RequireUnmonitored: &unmonitorEnabled,
						}); err != nil {
							report.Errors = append(report.Errors, fmt.Sprintf("stage delete episode files series %d: %v", series.ID, err))
							return dispatchOutcome{shouldUpsert: false}
						}
					} else {
						for _, fid := range targetFileIDs {
							if delErr := o.sonarr.DeleteEpisodeFile(ctx, fid); delErr != nil && !errors.Is(delErr, servarr.ErrNotFound) {
								report.Errors = append(report.Errors, fmt.Sprintf("delete episode file %d: %v", fid, delErr))
							}
						}
					}
				}
			}

			// Untagged series do not undergo unmonitoring or deletion
		}

		// Determine monitoredStatus for synced_media
		isMon := false
		if isSplitCour && len(sliceEpisodeIDs) > 0 {
			for _, ep := range episodes {
				if containsInt(sliceEpisodeIDs, ep.ID) && ep.Monitored {
					isMon = true
					break
				}
			}
		} else {
			for _, s := range series.Seasons {
				if s.SeasonNumber == targetSeason && s.Monitored {
					isMon = true
					break
				}
			}
		}
		monStatus := "UNMONITORED"
		if isMon && series.Monitored {
			monStatus = "MONITORED"
		}
		return dispatchOutcome{monitoredStatus: monStatus, season: targetSeason, shouldUpsert: true}
	}

	// Watched episode unmonitoring and file deletion (pruning)
	var watchedCount int
	var candidateEpisodes []servarr.Episode
	for _, ep := range episodes {
		if (isSplitCour && containsInt(sliceEpisodeIDs, ep.ID)) || (!isSplitCour && ep.SeasonNumber == targetSeason) {
			candidateEpisodes = append(candidateEpisodes, ep)
		}
	}
	sort.Slice(candidateEpisodes, func(i, j int) bool {
		return candidateEpisodes[i].EpisodeNumber < candidateEpisodes[j].EpisodeNumber
	})
	if searchAfterPruning && len(candidateEpisodes) == 0 {
		return dispatchOutcome{}
	}
	if searchAfterPruning && !hasManagedTag {
		report.Errors = append(report.Errors, fmt.Sprintf("cannot prune new series %d before search: missing managed tag", series.ID))
		return dispatchOutcome{shouldUpsert: false}
	}
	if hasManagedTag && (entry.Status == "" || entry.Status == anilist.StatusCurrent) && entry.Progress > 0 && (o.cfg.UnmonitorWatched || o.cfg.DeleteWatched) {

		watchedCount = entry.Progress
		if watchedCount > len(candidateEpisodes) {
			watchedCount = len(candidateEpisodes)
		}

		if watchedCount > 0 {
			watchedEpisodes := candidateEpisodes[:watchedCount]

			// Step 1: Unmonitor watched episodes if enabled
			if o.cfg.UnmonitorWatched {
				var epIDsToUnmonitor []int
				for _, ep := range watchedEpisodes {
					if ep.Monitored {
						epIDsToUnmonitor = append(epIDsToUnmonitor, ep.ID)
					}
				}

				if len(epIDsToUnmonitor) > 0 {
					if dryRun {
						if err := o.stageAction(ctx, ActionUnmonitorEpisode, "SERIES", title, "SONARR", UnmonitorEpisodePayload{
							SeriesID:   series.ID,
							EpisodeIDs: epIDsToUnmonitor,
							Title:      series.Title,
						}); err != nil {
							report.Errors = append(report.Errors, fmt.Sprintf("stage unmonitor watched episodes series %d: %v", series.ID, err))
							return dispatchOutcome{shouldUpsert: false}
						}
					} else {
						if monErr := o.sonarr.MonitorEpisodes(ctx, epIDsToUnmonitor, false); monErr != nil {
							report.Errors = append(report.Errors, fmt.Sprintf("unmonitor watched episodes series %d: %v", series.ID, monErr))
							return dispatchOutcome{shouldUpsert: false}
						}
						report.UnmonitoredSonarr++
					}
				}
			}

			// Step 2: Delete episode files if enabled (after unmonitoring)
			if o.cfg.DeleteWatched {
				var watchedEpisodeIDs []int
				for _, ep := range watchedEpisodes {
					watchedEpisodeIDs = append(watchedEpisodeIDs, ep.ID)
				}
				fileIDsToDelete := scopedEpisodeFileIDs(episodes, watchedEpisodeIDs)

				if len(fileIDsToDelete) > 0 {
					if dryRun {
						if err := o.stageAction(ctx, ActionDeleteEpisodeFile, "SERIES", title, "SONARR", DeleteEpisodeFilePayload{
							SeriesID:           series.ID,
							EpisodeFileIDs:     fileIDsToDelete,
							Title:              series.Title,
							EpisodeIDs:         watchedEpisodeIDs,
							RequireUnmonitored: &o.cfg.UnmonitorWatched,
						}); err != nil {
							report.Errors = append(report.Errors, fmt.Sprintf("stage delete watched episode files series %d: %v", series.ID, err))
						}
					} else {
						for _, fid := range fileIDsToDelete {
							if delErr := o.sonarr.DeleteEpisodeFile(ctx, fid); delErr != nil && !errors.Is(delErr, servarr.ErrNotFound) {
								report.Errors = append(report.Errors, fmt.Sprintf("delete watched episode file %d: %v", fid, delErr))
							}
						}
					}
				}
			}
		}
	}

	if searchAfterPruning {
		if o.cfg.SonarrSearchOnAdd {
			if err := o.searchUnwatchedEpisodes(ctx, candidateEpisodes, watchedCount); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("search new series %d: %v", series.ID, err))
				return dispatchOutcome{shouldUpsert: false}
			}
		}
		searchAfterPruning = false
		return dispatchOutcome{monitoredStatus: "MONITORED", season: targetSeason, shouldUpsert: true}
	}

	// Additive: selectively monitor target season / sub-season
	if (isSplitCour && len(sliceEpisodeIDs) > 0) || (o.cfg.UnmonitorWatched && watchedCount > 0) {
		var episodesToMonitor []int
		for i, ep := range candidateEpisodes {
			if o.cfg.UnmonitorWatched && i < watchedCount {
				continue
			}
			if !ep.Monitored {
				episodesToMonitor = append(episodesToMonitor, ep.ID)
			}
		}

		if len(episodesToMonitor) > 0 {
			if dryRun {
				if err := o.stageAction(ctx, ActionMonitorSeason, "SERIES", title, "SONARR", MonitorSeasonPayload{
					SeriesID:     series.ID,
					TVDBID:       series.TVDBID,
					SeasonNumber: targetSeason,
					EpisodeIDs:   episodesToMonitor,
					AnilistID:    entry.Media.ID,
				}); err != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("stage monitor episodes series %d: %v", series.ID, err))
					return dispatchOutcome{shouldUpsert: false}
				}
				return dispatchOutcome{monitoredStatus: "UNMONITORED", season: targetSeason, shouldUpsert: true}
			}
			if !series.Monitored {
				series.Monitored = true
				if _, updErr := o.sonarr.UpdateSeries(ctx, series); updErr != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("update series %d: %v", series.ID, updErr))
					return dispatchOutcome{shouldUpsert: false}
				}
			}
			if monErr := o.sonarr.MonitorEpisodes(ctx, episodesToMonitor, true); monErr != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("monitor episodes series %d: %v", series.ID, monErr))
				return dispatchOutcome{shouldUpsert: false}
			}
			report.MonitoredSonarr++
			if o.notifier != nil {
				o.notifier.Dispatch(notification.SyncEvent{
					Type:          notification.EventMediaAdded,
					Timestamp:     time.Now().UTC(),
					Title:         title,
					MediaType:     "SERIES",
					TargetService: "Sonarr",
					AniListID:     entry.Media.ID,
					Season:        &targetSeason,
					Episodes:      sliceEpisodeIDs,
					PosterURL:     entry.Media.CoverImageURL(),
				})
			}
			if o.cfg.SonarrSearchOnAdd {
				if o.cfg.UnmonitorWatched && watchedCount > 0 {
					if err := o.searchUnwatchedEpisodes(ctx, candidateEpisodes, watchedCount); err != nil {
						report.Errors = append(report.Errors, fmt.Sprintf("search unwatched episodes: %v", err))
					}
				} else {
					_, _ = o.sonarr.SearchSeason(ctx, series.ID, targetSeason)
				}
			}
			return dispatchOutcome{monitoredStatus: "MONITORED", season: targetSeason, shouldUpsert: true}
		}
		monStatus := "UNMONITORED"
		if series.Monitored {
			monStatus = "MONITORED"
		}
		return dispatchOutcome{monitoredStatus: monStatus, season: targetSeason, shouldUpsert: true}
	}

	// Entire season selective monitoring
	seasonNeedsMonitor := true
	for i := range series.Seasons {
		if series.Seasons[i].SeasonNumber == targetSeason {
			if series.Seasons[i].Monitored {
				seasonNeedsMonitor = false
			} else {
				series.Seasons[i].Monitored = true
			}
			break
		}
	}

	if seasonNeedsMonitor {
		if dryRun {
			if err := o.stageAction(ctx, ActionMonitorSeason, "SERIES", title, "SONARR", MonitorSeasonPayload{
				SeriesID:     series.ID,
				TVDBID:       series.TVDBID,
				SeasonNumber: targetSeason,
				AnilistID:    entry.Media.ID,
			}); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("stage monitor series %d season %d: %v", series.ID, targetSeason, err))
				return dispatchOutcome{shouldUpsert: false}
			}
			return dispatchOutcome{monitoredStatus: "UNMONITORED", season: targetSeason, shouldUpsert: true}
		}
		series.Monitored = true
		found := false
		for i := range series.Seasons {
			if series.Seasons[i].SeasonNumber == targetSeason {
				series.Seasons[i].Monitored = true
				found = true
				break
			}
		}
		if !found {
			series.Seasons = append(series.Seasons, servarr.Season{
				SeasonNumber: targetSeason,
				Monitored:    true,
			})
		}
		if _, updErr := o.sonarr.UpdateSeries(ctx, series); updErr != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("update series %d season %d: %v", series.ID, targetSeason, updErr))
			return dispatchOutcome{shouldUpsert: false}
		}
		report.MonitoredSonarr++
		if o.notifier != nil {
			o.notifier.Dispatch(notification.SyncEvent{
				Type:          notification.EventMediaAdded,
				Timestamp:     time.Now().UTC(),
				Title:         title,
				MediaType:     "SERIES",
				TargetService: "Sonarr",
				AniListID:     entry.Media.ID,
				Season:        &targetSeason,
				PosterURL:     entry.Media.CoverImageURL(),
			})
		}
		if o.cfg.SonarrSearchOnAdd {
			_, _ = o.sonarr.SearchSeason(ctx, series.ID, targetSeason)
		}
		return dispatchOutcome{monitoredStatus: "MONITORED", season: targetSeason, shouldUpsert: true}
	}

	monStatus := "UNMONITORED"
	if series.Monitored {
		monStatus = "MONITORED"
	}
	return dispatchOutcome{monitoredStatus: monStatus, season: targetSeason, shouldUpsert: true}
}

func (o *Orchestrator) searchUnwatchedEpisodes(ctx context.Context, episodes []servarr.Episode, watchedCount int) error {
	var ids []int
	for i, ep := range episodes {
		if i >= watchedCount && !ep.HasFile {
			ids = append(ids, ep.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := o.sonarr.ExecuteCommand(ctx, map[string]any{"name": "EpisodeSearch", "episodeIds": ids})
	return err
}

func containsInt(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

func (o *Orchestrator) hasManagedTag(ctx context.Context, client *servarr.Client, mediaTags []int) (bool, error) {
	if o.cfg.TagName == "" {
		return false, nil
	}
	tags, err := client.GetTags(ctx)
	if err != nil {
		return false, err
	}
	for _, tag := range tags {
		if tag.ID > 0 && strings.EqualFold(strings.TrimSpace(tag.Label), strings.TrimSpace(o.cfg.TagName)) {
			return containsInt(mediaTags, tag.ID), nil
		}
	}
	return false, nil
}

// cleanupOnly distinguishes historical cleanup inputs from explicitly configured imports.
func (o *Orchestrator) cleanupOnly(entry anilist.MediaListEntry) bool {
	if entry.Status != anilist.StatusCompleted && entry.Status != anilist.StatusDropped {
		return false
	}
	configured := o.cfg.AniListTvStatus
	if router.Route(router.MediaFormat(strings.ToUpper(strings.TrimSpace(entry.Media.Format)))) == router.RouteRadarr {
		configured = o.cfg.AniListMovieStatus
	}
	return !slices.Contains(anilist.ParseListStatuses(configured), entry.Status)
}

var subSeasonPattern = regexp.MustCompile(`(?i)\b(?:part|cour)\s+(\d+)\b`)

func extractSubSeasonNumber(titles ...string) int {
	for _, title := range titles {
		clean := strings.TrimSpace(title)
		if clean == "" {
			continue
		}
		matches := subSeasonPattern.FindStringSubmatch(clean)
		if len(matches) >= 2 {
			if val, err := strconv.Atoi(matches[1]); err == nil && val > 0 {
				return val
			}
		}
	}
	return 0
}

var explicitSeasonPattern = regexp.MustCompile(`(?i)\b(?:season\s+(\d+)|(\d+)(?:st|nd|rd|th)\s+season)\b`)

func hasExplicitSeason(titles ...string) bool {
	for _, t := range titles {
		if explicitSeasonPattern.MatchString(t) {
			return true
		}
	}
	return false
}

func (o *Orchestrator) reconcileWatchlistStatus(ctx context.Context) error {
	if o.al == nil || o.cfg.AniListUsername == "" || o.db == nil {
		return nil
	}

	overview, err := o.al.FetchUserMediaListOverview(ctx, o.cfg.AniListUsername)
	if err != nil {
		return fmt.Errorf("fetch anilist overview: %w", err)
	}

	userMap := make(map[int]anilist.MediaListStatus, len(overview))
	for _, it := range overview {
		userMap[it.MediaID] = it.Status
	}

	return o.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, qErr := tx.QueryContext(ctx, "SELECT anilist_id, anilist_status FROM synced_media;")
		if qErr != nil {
			return qErr
		}
		defer func() {
			_ = rows.Close()
		}()

		type updateRow struct {
			anilistID int
			newStatus string
		}
		var updates []updateRow

		for rows.Next() {
			var id int
			var currentStatus string
			if scanErr := rows.Scan(&id, &currentStatus); scanErr != nil {
				return scanErr
			}

			if newSt, ok := userMap[id]; ok {
				newStStr := string(newSt)
				if newStStr != currentStatus {
					updates = append(updates, updateRow{anilistID: id, newStatus: newStStr})
				}
			} else {
				// Title removed entirely from user's AniList profile
				if currentStatus != "REMOVED" {
					updates = append(updates, updateRow{anilistID: id, newStatus: "REMOVED"})
				}
			}
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return rowsErr
		}

		for _, u := range updates {
			_, execErr := tx.ExecContext(ctx, `
				UPDATE synced_media
				SET anilist_status = ?, updated_at = CURRENT_TIMESTAMP
				WHERE anilist_id = ?;
			`, u.newStatus, u.anilistID)
			if execErr != nil {
				return execErr
			}
		}

		return nil
	})
}

func (o *Orchestrator) isCleanupStatus(status anilist.MediaListStatus) bool {
	return (status == anilist.StatusDropped && (o.cfg.UnmonitorDropped || o.cfg.DeleteDropped)) ||
		(status == anilist.StatusCompleted && (o.cfg.UnmonitorCompleted || o.cfg.DeleteCompleted))
}

func (o *Orchestrator) configuredStatuses(series, movies []anilist.MediaListStatus) []anilist.MediaListStatus {
	statuses := slices.Concat(series, movies)
	for _, status := range []anilist.MediaListStatus{anilist.StatusDropped, anilist.StatusCompleted} {
		if o.isCleanupStatus(status) {
			statuses = append(statuses, status)
		}
	}
	slices.Sort(statuses)
	return slices.Compact(statuses)
}

func (o *Orchestrator) recordHistory(ctx context.Context, report *SyncReport, start time.Time) {
	duration := time.Since(start)
	report.DurationMs = duration.Milliseconds()
	report.StartTime = start

	var errsJSON *string
	if len(report.Errors) > 0 {
		if b, err := json.Marshal(report.Errors); err == nil {
			str := string(b)
			errsJSON = &str
		}
	}

	_ = o.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO sync_history (
				run_timestamp, duration_ms, status, items_scanned, added_radarr,
				monitored_sonarr, unmonitored_sonarr, unmonitored_radarr,
				queued_review, errors_json, trigger_type
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`, start.UTC(), report.DurationMs, string(report.Status), report.ItemsScanned,
			report.AddedRadarr, report.MonitoredSonarr, report.UnmonitoredSonarr,
			report.UnmonitoredRadarr, report.QueuedReview, errsJSON, string(report.Trigger))
		if err != nil {
			return err
		}
		if id, idErr := res.LastInsertId(); idErr == nil {
			report.RunID = id
		}
		return nil
	})
}

func (o *Orchestrator) upsertSyncedMedia(
	ctx context.Context,
	entry anilist.MediaListEntry,
	mediaType string,
	targetService string,
	tvdbID *int,
	tmdbID *int,
	arrTitle string,
	monitoredStatus string,
	seasons string,
	pendingSonarrFollowup bool,
) error {
	if o.db == nil {
		return nil
	}
	return o.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		poster := entry.Media.CoverImageURL()
		var eng *string
		if entry.Media.Title.English != "" {
			eng = &entry.Media.Title.English
		}
		romaji := entry.Media.Title.Romaji
		if romaji == "" {
			romaji = entry.Media.Title.UserPreferred
		}
		if romaji == "" {
			romaji = entry.Media.Title.English
		}
		var year *int
		if entry.Media.StartDate.Year != nil {
			year = entry.Media.StartDate.Year
		}
		var arrTitleVal *string
		if arrTitle != "" {
			arrTitleVal = &arrTitle
		}
		var seasonsVal *string
		if seasons != "" {
			seasonsVal = &seasons
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO synced_media (
				anilist_id, media_type, format, title_romaji, title_english,
				release_year, poster_url, anilist_status, target_service,
				tvdb_id, tmdb_id, arr_title, monitored_status, seasons, sonarr_followup_pending,
				last_synced_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT(anilist_id) DO UPDATE SET
				media_type = excluded.media_type,
				format = excluded.format,
				title_romaji = excluded.title_romaji,
				title_english = excluded.title_english,
				release_year = excluded.release_year,
				poster_url = excluded.poster_url,
				anilist_status = excluded.anilist_status,
				target_service = CASE WHEN excluded.monitored_status = 'IGNORED' THEN synced_media.target_service ELSE excluded.target_service END,
				tvdb_id = CASE WHEN excluded.monitored_status = 'IGNORED' THEN synced_media.tvdb_id ELSE excluded.tvdb_id END,
				tmdb_id = CASE WHEN excluded.monitored_status = 'IGNORED' THEN synced_media.tmdb_id ELSE excluded.tmdb_id END,
				arr_title = CASE WHEN excluded.monitored_status = 'IGNORED' THEN synced_media.arr_title ELSE excluded.arr_title END,
				monitored_status = excluded.monitored_status,
				seasons = CASE WHEN excluded.monitored_status = 'IGNORED' THEN synced_media.seasons ELSE excluded.seasons END,
				sonarr_followup_pending = excluded.sonarr_followup_pending,
				last_synced_at = CURRENT_TIMESTAMP,
				updated_at = CURRENT_TIMESTAMP;
		`, entry.Media.ID, mediaType, entry.Media.Format, romaji, eng,
			year, poster, string(entry.Status), targetService,
			tvdbID, tmdbID, arrTitleVal, monitoredStatus, seasonsVal, pendingSonarrFollowup,
		)
		return err
	})
}

// PurgeExpiredStagedActions removes pending staged actions older than maxAge.
func (o *Orchestrator) PurgeExpiredStagedActions(ctx context.Context, maxAge time.Duration) (int64, error) {
	if maxAge <= 0 {
		maxAge = 7 * 24 * time.Hour
	}
	cutoff := time.Now().Add(-maxAge).UTC().Format("2006-01-02 15:04:05")

	var rowsAffected int64
	err := o.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, execErr := tx.ExecContext(ctx, `
			DELETE FROM staged_sync_actions
			WHERE status = 'PENDING' AND created_at < ?;
		`, cutoff)
		if execErr != nil {
			return execErr
		}
		var affErr error
		rowsAffected, affErr = res.RowsAffected()
		return affErr
	})
	if err != nil {
		return 0, fmt.Errorf("purge expired staged actions: %w", err)
	}
	return rowsAffected, nil
}

// SyncRecord represents an entry in the sync_history table.
type SyncRecord struct {
	ID                int64       `json:"id"`
	RunTimestamp      time.Time   `json:"runTimestamp"`
	DurationMs        int64       `json:"durationMs"`
	Status            SyncStatus  `json:"status"`
	ItemsScanned      int         `json:"itemsScanned"`
	AddedRadarr       int         `json:"addedRadarr"`
	MonitoredSonarr   int         `json:"monitoredSonarr"`
	UnmonitoredSonarr int         `json:"unmonitoredSonarr"`
	UnmonitoredRadarr int         `json:"unmonitoredRadarr"`
	QueuedReview      int         `json:"queuedReview"`
	Errors            []string    `json:"errors"`
	Trigger           TriggerType `json:"triggerType"`
}

// GetSyncHistory retrieves historical synchronization runs up to limit.
func (o *Orchestrator) GetSyncHistory(ctx context.Context, limit int) ([]SyncRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	var records []SyncRecord
	err := o.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		rows, err := q.QueryContext(ctx, `
			SELECT id, run_timestamp, duration_ms, status, items_scanned, added_radarr,
				   monitored_sonarr, unmonitored_sonarr, unmonitored_radarr,
				   queued_review, errors_json, trigger_type
			FROM sync_history
			ORDER BY id DESC
			LIMIT ?;
		`, limit)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var r SyncRecord
			var errsJSON sql.NullString
			if scanErr := rows.Scan(
				&r.ID, &r.RunTimestamp, &r.DurationMs, &r.Status,
				&r.ItemsScanned, &r.AddedRadarr, &r.MonitoredSonarr,
				&r.UnmonitoredSonarr, &r.UnmonitoredRadarr, &r.QueuedReview,
				&errsJSON, &r.Trigger,
			); scanErr != nil {
				return scanErr
			}
			if errsJSON.Valid && errsJSON.String != "" {
				_ = json.Unmarshal([]byte(errsJSON.String), &r.Errors)
			}
			records = append(records, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get sync history: %w", err)
	}
	return records, nil
}

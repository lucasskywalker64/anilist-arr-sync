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
	"strconv"
	"strings"
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
}

// Notifier defines the interface for dispatching notification events.
type Notifier interface {
	Dispatch(event notification.SyncEvent)
}

// Orchestrator coordinates the end-to-end synchronization workflow.
type Orchestrator struct {
	db       *storage.DB
	cfg      *config.Config
	al       *anilist.Client
	sonarr   *servarr.SonarrClient
	radarr   *servarr.RadarrClient
	resolver *resolver.Resolver
	notifier Notifier
}

// SetNotifier sets a notification dispatcher on the Orchestrator.
func (o *Orchestrator) SetNotifier(n Notifier) {
	o.notifier = n
}

// New creates an Orchestrator instance.
func New(
	db *storage.DB,
	cfg *config.Config,
	al *anilist.Client,
	sonarr *servarr.SonarrClient,
	radarr *servarr.RadarrClient,
) *Orchestrator {
	return &Orchestrator{
		db:       db,
		cfg:      cfg,
		al:       al,
		sonarr:   sonarr,
		radarr:   radarr,
		resolver: resolver.New(db),
	}
}

// Sync executes the core synchronization loop following the four-step sequence.
func (o *Orchestrator) Sync(ctx context.Context, trigger TriggerType) (*SyncReport, error) {
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
		o.recordHistory(ctx, report, startTime)
		return report, fmt.Errorf("load ignored titles: %w", err)
	}

	// Fetch AniList watchlist
	statuses := o.configuredStatuses()
	filter := anilist.WatchlistFilter{
		Username:          o.cfg.AniListUsername,
		Statuses:          statuses,
		IncludeUnreleased: o.cfg.AniListIncludeUnreleased,
	}

	entries, err := o.al.FetchWatchlist(ctx, filter)
	if err != nil {
		report.Status = SyncStatusFailed
		report.Errors = append(report.Errors, fmt.Sprintf("failed to fetch AniList watchlist: %v", err))
		o.recordHistory(ctx, report, startTime)
		return report, fmt.Errorf("fetch anilist watchlist: %w", err)
	}

	report.ItemsScanned = len(entries)

	for _, entry := range entries {
		// Step 1: Skip titles present in ignored_titles table before resolution
		if ignoredSet[entry.Media.ID] {
			continue
		}

		// Step 2: Discard non-video entries immediately using router.Route without querying database
		normalizedFormat := router.MediaFormat(strings.ToUpper(strings.TrimSpace(entry.Media.Format)))
		decision := router.Route(normalizedFormat)
		if decision == router.RouteDiscard {
			continue
		}

		// Step 3 & 4: Check established database mappings and divert unmapped entries
		res, resErr := o.resolver.Resolve(ctx, entry.Media)
		if resErr != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("resolve media %d: %v", entry.Media.ID, resErr))
			continue
		}

		if res.Diverted {
			report.QueuedReview++
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

		// Dispatch to Servarr service
		switch res.TargetService {
		case "RADARR":
			o.dispatchRadarr(ctx, entry, res, report)
		case "SONARR":
			o.dispatchSonarr(ctx, entry, res, report)
		}
	}

	if len(report.Errors) > 0 {
		if report.ItemsScanned > 0 && len(report.Errors) < report.ItemsScanned {
			report.Status = SyncStatusPartial
		} else {
			report.Status = SyncStatusFailed
		}
	}

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

	return report, nil
}

func (o *Orchestrator) dispatchRadarr(ctx context.Context, entry anilist.MediaListEntry, res *resolver.Result, report *SyncReport) {
	if o.radarr == nil {
		return
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
		return
	}

	movieExists := lookupErr == nil && movie != nil && movie.ID > 0

	if movieExists {
		if entry.Status == anilist.StatusDropped {
			if o.cfg.UnmonitorDropped {
				var managedTagID int
				if o.cfg.TagName != "" {
					managedTagID, _ = o.radarr.EnsureTag(ctx, o.cfg.TagName)
				}
				if managedTagID > 0 && containsInt(movie.Tags, managedTagID) && movie.Monitored {
					if o.cfg.FirstRunDryRun {
						if err := o.stageAction(ctx, ActionUnmonitorMovie, "MOVIE", title, "RADARR", UnmonitorMoviePayload{
							MovieID: movie.ID,
							TMDBID:  movie.TMDBID,
							Title:   movie.Title,
						}); err != nil {
							report.Errors = append(report.Errors, fmt.Sprintf("stage unmonitor movie %d: %v", movie.ID, err))
						}
					} else {
						movie.Monitored = false
						if _, updErr := o.radarr.UpdateMovie(ctx, movie); updErr == nil {
							report.UnmonitoredRadarr++
						} else {
							report.Errors = append(report.Errors, fmt.Sprintf("unmonitor movie %d: %v", movie.ID, updErr))
						}
					}
				}
			}
		}
		return
	}

	// Movie is not in Radarr library
	if entry.Status == anilist.StatusDropped {
		return
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
		Monitored:           true,
		MinimumAvailability: "announced",
		Tags:                tags,
		AddOptions: &servarr.AddMovieOptions{
			SearchForMovie: o.cfg.RadarrSearchOnAdd,
		},
	}

	if o.cfg.FirstRunDryRun {
		if err := o.stageAction(ctx, ActionAddMovie, "MOVIE", title, "RADARR", addReq); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("stage add movie %q: %v", title, err))
		}
		return
	}

	_, addErr := o.radarr.AddMovie(ctx, addReq)
	if addErr != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("add movie %q: %v", title, addErr))
		return
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
}

func (o *Orchestrator) dispatchSonarr(ctx context.Context, entry anilist.MediaListEntry, res *resolver.Result, report *SyncReport) {
	if o.sonarr == nil {
		return
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
	if targetSeason <= 0 {
		targetSeason = 1
	}

	series, lookupErr := o.sonarr.LookupSeriesByTVDBID(ctx, res.TargetID)
	if lookupErr != nil && !errors.Is(lookupErr, servarr.ErrNotFound) {
		report.Errors = append(report.Errors, fmt.Sprintf("lookup series tvdb %d: %v", res.TargetID, lookupErr))
		return
	}

	seriesExists := lookupErr == nil && series != nil && series.ID > 0

	if !seriesExists {
		if entry.Status == anilist.StatusDropped {
			return
		}

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
					Monitored:    isTarget,
				})
			}
			if !targetFound {
				seasons = append(seasons, servarr.Season{
					SeasonNumber: targetSeason,
					Monitored:    true,
				})
			}
		} else {
			seasons = []servarr.Season{{SeasonNumber: targetSeason, Monitored: true}}
		}

		var tags []int
		if !o.cfg.FirstRunDryRun && o.cfg.TagName != "" {
			if tagID, tagErr := o.sonarr.EnsureTag(ctx, o.cfg.TagName); tagErr == nil {
				tags = []int{tagID}
			}
		}

		addReq := servarr.AddSeriesRequest{
			Title:            title,
			TVDBID:           res.TargetID,
			QualityProfileID: o.cfg.SonarrQualityProfileID,
			RootFolderPath:   o.cfg.SonarrRootFolderPath,
			SeriesType:       o.cfg.SonarrSeriesType,
			Monitored:        true,
			SeasonFolder:     true,
			Seasons:          seasons,
			Tags:             tags,
			AddOptions: &servarr.AddSeriesOptions{
				SearchForMissingEpisodes: o.cfg.SonarrSearchOnAdd,
			},
		}

		if o.cfg.FirstRunDryRun {
			if err := o.stageAction(ctx, ActionAddSeries, "SERIES", title, "SONARR", addReq); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("stage add series %q: %v", title, err))
			}
			return
		}

		_, addErr := o.sonarr.AddSeries(ctx, addReq)
		if addErr != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("add series %q: %v", title, addErr))
			return
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
		return
	}

	// Series already exists in Sonarr library
	episodes, epErr := o.sonarr.GetEpisodes(ctx, series.ID)
	if epErr != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("get episodes series %d: %v", series.ID, epErr))
		return
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
	if matchRes.Season > 0 && !matchRes.Conflict {
		if matchRes.MatchedBy == season.MatchMethodBoth || matchRes.MatchedBy == season.MatchMethodAirDate {
			targetSeason = matchRes.Season
		} else if hasExplicitSeason(titles...) {
			targetSeason = matchRes.Season
		}
	}
	if targetSeason <= 0 {
		targetSeason = 1
	}

	subSeasonNum := extractSubSeasonNumber(titles...)
	sliceEpisodeIDs, isSplitCour := FindSubSeasonEpisodeSlice(episodes, targetSeason, subSeasonNum, startDate, tolerance)

	var managedTagID int
	if o.cfg.TagName != "" {
		managedTagID, _ = o.sonarr.EnsureTag(ctx, o.cfg.TagName)
	}
	hasManagedTag := managedTagID > 0 && containsInt(series.Tags, managedTagID)

	if entry.Status == anilist.StatusDropped {
		if o.cfg.UnmonitorDropped && hasManagedTag {
			if isSplitCour && len(sliceEpisodeIDs) > 0 {
				anyMonitored := false
				for _, ep := range episodes {
					if containsInt(sliceEpisodeIDs, ep.ID) && ep.Monitored {
						anyMonitored = true
						break
					}
				}
				if anyMonitored {
					if o.cfg.FirstRunDryRun {
						if err := o.stageAction(ctx, ActionUnmonitorSeason, "SERIES", title, "SONARR", MonitorSeasonPayload{
							SeriesID:     series.ID,
							TVDBID:       series.TVDBID,
							SeasonNumber: targetSeason,
							EpisodeIDs:   sliceEpisodeIDs,
						}); err != nil {
							report.Errors = append(report.Errors, fmt.Sprintf("stage unmonitor episodes series %d: %v", series.ID, err))
						}
					} else {
						if monErr := o.sonarr.MonitorEpisodes(ctx, sliceEpisodeIDs, false); monErr == nil {
							report.UnmonitoredSonarr++
						} else {
							report.Errors = append(report.Errors, fmt.Sprintf("unmonitor episodes series %d: %v", series.ID, monErr))
						}
					}
				}
			} else {
				seasonMonitored := false
				for i := range series.Seasons {
					if series.Seasons[i].SeasonNumber == targetSeason && series.Seasons[i].Monitored {
						seasonMonitored = true
						series.Seasons[i].Monitored = false
						break
					}
				}
				if seasonMonitored {
					if o.cfg.FirstRunDryRun {
						if err := o.stageAction(ctx, ActionUnmonitorSeason, "SERIES", title, "SONARR", MonitorSeasonPayload{
							SeriesID:     series.ID,
							TVDBID:       series.TVDBID,
							SeasonNumber: targetSeason,
						}); err != nil {
							report.Errors = append(report.Errors, fmt.Sprintf("stage unmonitor series %d season %d: %v", series.ID, targetSeason, err))
						}
					} else {
						if _, updErr := o.sonarr.UpdateSeries(ctx, series); updErr == nil {
							report.UnmonitoredSonarr++
						} else {
							report.Errors = append(report.Errors, fmt.Sprintf("unmonitor series %d season %d: %v", series.ID, targetSeason, updErr))
						}
					}
				}
			}
		}
		return
	}

	// Additive: selectively monitor target season / sub-season
	if isSplitCour && len(sliceEpisodeIDs) > 0 {
		allMonitored := true
		for _, ep := range episodes {
			if containsInt(sliceEpisodeIDs, ep.ID) && !ep.Monitored {
				allMonitored = false
				break
			}
		}
		if !allMonitored {
			if o.cfg.FirstRunDryRun {
				if err := o.stageAction(ctx, ActionMonitorSeason, "SERIES", title, "SONARR", MonitorSeasonPayload{
					SeriesID:     series.ID,
					TVDBID:       series.TVDBID,
					SeasonNumber: targetSeason,
					EpisodeIDs:   sliceEpisodeIDs,
				}); err != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("stage monitor episodes series %d: %v", series.ID, err))
				}
			} else {
				if monErr := o.sonarr.MonitorEpisodes(ctx, sliceEpisodeIDs, true); monErr == nil {
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
						_, _ = o.sonarr.SearchSeason(ctx, series.ID, targetSeason)
					}
				} else {
					report.Errors = append(report.Errors, fmt.Sprintf("monitor episodes series %d: %v", series.ID, monErr))
				}
			}
		}
		return
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
		if o.cfg.FirstRunDryRun {
			if err := o.stageAction(ctx, ActionMonitorSeason, "SERIES", title, "SONARR", MonitorSeasonPayload{
				SeriesID:     series.ID,
				TVDBID:       series.TVDBID,
				SeasonNumber: targetSeason,
			}); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("stage monitor series %d season %d: %v", series.ID, targetSeason, err))
			}
		} else {
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
			if _, updErr := o.sonarr.UpdateSeries(ctx, series); updErr == nil {
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
			} else {
				report.Errors = append(report.Errors, fmt.Sprintf("update series %d season %d: %v", series.ID, targetSeason, updErr))
			}
		}
	}
}

func containsInt(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
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

func (o *Orchestrator) configuredStatuses() []anilist.MediaListStatus {
	statusMap := make(map[anilist.MediaListStatus]bool)
	if o.cfg.AniListTvStatus != "" {
		statusMap[anilist.MediaListStatus(strings.ToUpper(strings.TrimSpace(o.cfg.AniListTvStatus)))] = true
	}
	if o.cfg.AniListMovieStatus != "" {
		statusMap[anilist.MediaListStatus(strings.ToUpper(strings.TrimSpace(o.cfg.AniListMovieStatus)))] = true
	}
	if o.cfg.UnmonitorDropped {
		statusMap[anilist.StatusDropped] = true
	}
	if len(statusMap) == 0 {
		statusMap[anilist.StatusCurrent] = true
	}

	var list []anilist.MediaListStatus
	for s := range statusMap {
		list = append(list, s)
	}
	return list
}

func (o *Orchestrator) recordHistory(ctx context.Context, report *SyncReport, start time.Time) {
	duration := time.Since(start)
	report.DurationMs = duration.Milliseconds()

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
				duration_ms, status, items_scanned, added_radarr,
				monitored_sonarr, unmonitored_sonarr, unmonitored_radarr,
				queued_review, errors_json, trigger_type
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`, report.DurationMs, string(report.Status), report.ItemsScanned,
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

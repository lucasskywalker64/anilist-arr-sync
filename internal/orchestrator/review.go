package orchestrator

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

// ReviewPreparation is the episode picker state for a pending review.
type ReviewPreparation struct {
	Status   string            `json:"status"`
	Message  string            `json:"message,omitempty"`
	Series   *servarr.Series   `json:"series,omitempty"`
	Episodes []servarr.Episode `json:"episodes,omitempty"`
	Created  bool              `json:"created"`
}

// ErrReviewPreparationChanged means the selection must be checked before confirmation.
var ErrReviewPreparationChanged = errors.New("review preparation changed")

type reviewLease struct {
	id, tvdbID, seriesID, tagID   int
	endpoint, token, baseline     string
	created, cancelled, confirmed bool
	addStartedAt                  sql.NullTime
	refreshCompleted              bool
	refreshID                     int
}

func (o *Orchestrator) reviewLease(ctx context.Context, id int) (reviewLease, error) {
	p := reviewLease{id: id}
	err := o.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, `SELECT endpoint,tvdb_id,series_id,tag_id,token,baseline,created,cancelled,confirmed,add_started_at,refresh_completed,refresh_id FROM review_preparations WHERE review_id=?`, id).Scan(&p.endpoint, &p.tvdbID, &p.seriesID, &p.tagID, &p.token, &p.baseline, &p.created, &p.cancelled, &p.confirmed, &p.addStartedAt, &p.refreshCompleted, &p.refreshID)
	})
	return p, err
}

func (o *Orchestrator) writeReview(ctx context.Context, query string, args ...any) error {
	return o.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error { _, err := tx.ExecContext(ctx, query, args...); return err })
}

// PrepareReview reuses a library series or creates an unmonitored, owned preparation.
func (o *Orchestrator) PrepareReview(ctx context.Context, id, tvdbID int) (*ReviewPreparation, error) {
	if !o.workMu.TryLock() {
		return nil, ErrSyncInProgress
	}
	defer o.workMu.Unlock()
	run := o.snapshot()
	if run.sonarr == nil {
		return nil, errors.New("sonarr is not configured")
	}
	if p, err := run.reviewLease(ctx, id); err == nil {
		if p.tvdbID != tvdbID || p.cancelled || p.confirmed {
			return nil, errors.New("cancel the current preparation before changing its target")
		}
		return run.pollReview(ctx, p)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var pending int
	if err := run.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_queue WHERE id=? AND status='PENDING'`, id).Scan(&pending)
	}); err != nil {
		return nil, err
	}
	if pending != 1 || tvdbID <= 0 {
		return nil, errors.New("pending review and positive TVDB ID are required")
	}
	series, err := run.sonarr.LookupSeriesByTVDBID(ctx, tvdbID)
	if err != nil {
		return nil, err
	}
	p := reviewLease{id: id, tvdbID: tvdbID, endpoint: run.sonarr.BaseURL(), token: rand.Text(), created: series.ID == 0, seriesID: series.ID}
	if err = run.writeReview(ctx, `INSERT INTO review_preparations(review_id,endpoint,tvdb_id,series_id,token,created) VALUES(?,?,?,?,?,?)`, id, p.endpoint, tvdbID, p.seriesID, p.token, p.created); err != nil {
		return nil, errors.New("this series is already being prepared by another review")
	}
	if p.created {
		if run.cfg.SonarrQualityProfileID <= 0 || run.cfg.SonarrRootFolderPath == "" {
			_ = run.writeReview(ctx, `DELETE FROM review_preparations WHERE review_id=?`, id)
			return nil, errors.New("configure a Sonarr quality profile and root folder before loading episodes")
		}
		// The ownership marker is persisted before the add, so an uncertain HTTP response can be recovered.
		p.tagID, err = run.sonarr.EnsureTag(ctx, p.tagLabel())
		if err != nil {
			return nil, err
		}
		if err = run.writeReview(ctx, `UPDATE review_preparations SET tag_id=? WHERE review_id=?`, p.tagID, id); err != nil {
			return nil, err
		}
		tags := []int{p.tagID}
		if run.cfg.TagName != "" {
			tag, tagErr := run.sonarr.EnsureTag(ctx, run.cfg.TagName)
			if tagErr != nil {
				return nil, tagErr
			}
			tags = append(tags, tag)
		}
		seasons := slices.Clone(series.Seasons)
		for i := range seasons {
			seasons[i].Monitored = false
		}
		if err = run.writeReview(ctx, `UPDATE review_preparations SET add_started_at=CURRENT_TIMESTAMP WHERE review_id=?`, id); err != nil {
			return nil, err
		}
		series, err = run.sonarr.AddSeries(ctx, servarr.AddSeriesRequest{TVDBID: tvdbID, Title: series.Title, RootFolderPath: run.cfg.SonarrRootFolderPath, QualityProfileID: run.cfg.SonarrQualityProfileID, SeriesType: run.cfg.SonarrSeriesType, SeasonFolder: true, Monitored: false, MonitorNewItems: "none", Seasons: seasons, Tags: tags, AddOptions: &servarr.AddSeriesOptions{Monitor: "none"}})
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err != nil {
			var apiErr *servarr.APIError
			if errors.Is(err, servarr.ErrUnauthorized) || errors.Is(err, servarr.ErrNotFound) || (errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest) {
				return nil, errors.Join(err, run.releaseReview(saveCtx, p, false))
			}
			return nil, fmt.Errorf("add outcome is pending verification; cancel or retry this preparation: %w", err)
		}
		p.seriesID = series.ID
		p.baseline = reviewFingerprint(series)
		if err = run.writeReview(saveCtx, `UPDATE review_preparations SET series_id=?,baseline=? WHERE review_id=?`, p.seriesID, p.baseline, id); err != nil {
			return nil, err
		}
	}
	return run.pollReview(ctx, p)
}

func (p reviewLease) tagLabel() string { return "anilist-review-" + strings.ToLower(p.token) }

func reviewFingerprint(s *servarr.Series) string {
	tags := slices.Clone(s.Tags)
	slices.Sort(tags)
	value, _ := json.Marshal(struct {
		Path           string
		Profile        int
		Type           string
		NewItems       string
		SeasonFolder   bool
		SceneNumbering bool
		Added          time.Time
		Tags           []int
	}{s.Path, s.QualityProfileID, s.SeriesType, s.MonitorNewItems, s.SeasonFolder, s.UseSceneNumbering, s.Added, tags})
	return string(value)
}

func (o *Orchestrator) pollReview(ctx context.Context, p reviewLease) (*ReviewPreparation, error) {
	if p.cancelled && !p.created {
		return &ReviewPreparation{Status: "CANCELLED"}, o.releaseReview(ctx, p, false)
	}
	if o.sonarr == nil || p.endpoint != o.sonarr.BaseURL() {
		return nil, errors.New("sonarr connection changed; restore the original connection to finish this preparation")
	}
	result := &ReviewPreparation{Status: "WAITING", Message: "Loading Sonarr episode metadata", Created: p.created}
	if p.seriesID == 0 {
		series, err := o.sonarr.LookupSeriesByTVDBID(ctx, p.tvdbID)
		if errors.Is(err, servarr.ErrNotFound) || (err == nil && series.ID == 0) {
			if p.cancelled {
				if p.addStartedAt.Valid {
					result.Status = "CANCELLING"
					result.Message = "Verifying whether Sonarr accepted the add"
					return result, nil
				}
				result.Status = "CANCELLED"
				return result, o.releaseReview(ctx, p, false)
			}
			result.Message = "Waiting to verify whether Sonarr accepted the add"
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		if !slices.Contains(series.Tags, p.tagID) || p.tagID == 0 {
			result.Status = "RETAINED"
			result.Message = "A series already exists in Sonarr and was left in place. Load episodes again to reuse it."
			return result, o.releaseReview(ctx, p, false)
		}
		p.seriesID = series.ID
		// Recovery proves ownership through the tag, but cannot reconstruct the original settings.
		if err = o.writeReview(ctx, `UPDATE review_preparations SET series_id=? WHERE review_id=?`, p.seriesID, p.id); err != nil {
			return nil, err
		}
	}
	series, err := o.sonarr.GetSeriesByID(ctx, p.seriesID)
	if errors.Is(err, servarr.ErrNotFound) {
		result.Status = "CANCELLED"
		return result, o.releaseReview(ctx, p, false)
	}
	if err != nil {
		return nil, err
	}
	if series.TVDBID != p.tvdbID {
		return nil, errors.New("the Sonarr series identity changed; preparation cannot be applied or removed")
	}
	result.Series = series
	if p.confirmed {
		return result, o.releaseReview(ctx, p, true)
	}
	episodes, err := o.sonarr.GetEpisodes(ctx, p.seriesID)
	if err != nil {
		return nil, err
	}
	running, failed, complete := false, false, p.refreshCompleted
	refreshUnknown := false
	if !complete && (p.created || series.AddOptions != nil) {
		status, err := o.reviewRefreshStatus(ctx, p)
		if err != nil {
			return nil, err
		}
		switch status {
		case "":
			refreshUnknown = true
		case "queued", "started":
			running = true
		case "failed", "aborted", "cancelled", "orphaned":
			failed = true
		case "completed":
			complete = true
		}
	} else if !p.created {
		complete = true
	}
	if p.created && complete && !p.refreshCompleted {
		if err = o.writeReview(ctx, `UPDATE review_preparations SET refresh_completed=1 WHERE review_id=?`, p.id); err != nil {
			return nil, err
		}
	}
	unmonitored := !series.Monitored
	for _, season := range series.Seasons {
		unmonitored = unmonitored && !season.Monitored
	}
	for _, ep := range episodes {
		unmonitored = unmonitored && !ep.Monitored
	}
	if p.cancelled {
		result.Status = "CANCELLING"
		result.Message = "Waiting for the initial refresh before removing the temporary series"
		if running || (!complete && !failed && series.AddOptions != nil) {
			return result, nil
		}
		adopted := false
		if err = o.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
			var count int
			err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM mapping_overrides WHERE media_type='SERIES' AND tvdb_id=?`, p.tvdbID).Scan(&count)
			adopted = count > 0
			return err
		}); err != nil {
			return nil, err
		}
		for _, ep := range episodes {
			adopted = adopted || ep.HasFile
		}
		owner, err := o.reviewOwnsTag(ctx, p)
		if err != nil {
			return nil, err
		}
		if refreshUnknown || adopted || !unmonitored || reviewFingerprint(series) != p.baseline || !owner || !slices.Contains(series.Tags, p.tagID) {
			result.Status = "RETAINED"
			result.Message = "Series changed or was adopted; it was left in Sonarr"
			if refreshUnknown {
				result.Message = "Refresh completion could not be verified; series left in Sonarr. Load episodes again to reuse it."
			}
			if err = o.writeReview(ctx, `UPDATE review_preparations SET confirmed=1 WHERE review_id=?`, p.id); err != nil {
				return nil, err
			}
			return result, o.releaseReview(ctx, p, true)
		}
		if err = o.sonarr.DeleteSeries(ctx, p.seriesID); err != nil {
			return nil, err
		}
		result.Status = "CANCELLED"
		result.Message = "Temporary series removed; files were preserved"
		return result, o.releaseReview(ctx, p, false)
	}
	if refreshUnknown {
		result.Status = "ERROR"
		result.Message = "Sonarr's refresh history is unavailable. Cancel preparation and load episodes again. If metadata is still incomplete, refresh the series in Sonarr first."
		return result, nil
	}
	if failed && !running {
		result.Status = "ERROR"
		result.Message = "Sonarr's initial refresh failed; retry or cancel"
		return result, nil
	}
	if running || !complete || series.AddOptions != nil || (p.created && !unmonitored) {
		return result, nil
	}
	if len(episodes) == 0 {
		result.Status = "ERROR"
		result.Message = "Sonarr finished without episode data; retry or cancel"
		return result, nil
	}
	result.Status = "READY"
	result.Message = ""
	result.Episodes = episodes
	return result, nil
}

func (o *Orchestrator) reviewRefreshStatus(ctx context.Context, p reviewLease) (string, error) {
	if p.refreshID > 0 {
		cmd, err := o.sonarr.GetCommand(ctx, p.refreshID)
		if err == nil {
			return cmd.Status, nil
		}
		if !errors.Is(err, servarr.ErrNotFound) {
			return "", err
		}
	}
	commands, err := o.sonarr.GetCommands(ctx)
	if err != nil {
		return "", err
	}
	for _, cmd := range commands {
		if cmd.Name == "RefreshSeries" && cmd.Body.IsNewSeries && slices.Contains(cmd.Body.SeriesIDs, p.seriesID) {
			if err = o.writeReview(ctx, `UPDATE review_preparations SET refresh_id=? WHERE review_id=?`, cmd.ID, p.id); err != nil {
				return "", err
			}
			return cmd.Status, nil
		}
	}
	return "", nil
}

func (o *Orchestrator) releaseReview(ctx context.Context, p reviewLease, keep bool) error {
	owner, err := o.reviewOwnsTag(ctx, p)
	if err != nil {
		return err
	}
	if owner {
		if keep && p.seriesID > 0 {
			series, err := o.sonarr.GetSeriesByID(ctx, p.seriesID)
			if err != nil && !errors.Is(err, servarr.ErrNotFound) {
				return err
			}
			if err == nil && slices.Contains(series.Tags, p.tagID) {
				if err = o.sonarr.RemoveSeriesTag(ctx, p.seriesID, p.tagID); err != nil {
					return err
				}
			}
		}
		if err := o.sonarr.DeleteTag(ctx, p.tagID); err != nil {
			return err
		}
	}
	return o.writeReview(ctx, `DELETE FROM review_preparations WHERE review_id=?`, p.id)
}

func (o *Orchestrator) reviewOwnsTag(ctx context.Context, p reviewLease) (bool, error) {
	if !p.created || p.tagID <= 0 {
		return false, nil
	}
	tags, err := o.sonarr.GetTags(ctx)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(tags, func(tag servarr.Tag) bool { return tag.ID == p.tagID && strings.EqualFold(tag.Label, p.tagLabel()) }), nil
}

// PollReview updates a preparation and renews its browser-independent lease.
func (o *Orchestrator) PollReview(ctx context.Context, id int) (*ReviewPreparation, error) {
	if !o.workMu.TryLock() {
		return nil, ErrSyncInProgress
	}
	defer o.workMu.Unlock()
	run := o.snapshot()
	p, err := run.reviewLease(ctx, id)
	if err != nil {
		return nil, err
	}
	if err = run.writeReview(ctx, `UPDATE review_preparations SET updated_at=CURRENT_TIMESTAMP WHERE review_id=?`, id); err != nil {
		return nil, err
	}
	return run.pollReview(ctx, p)
}

// CancelReview durably requests cleanup; an in-flight refresh is allowed to finish first.
func (o *Orchestrator) CancelReview(ctx context.Context, id int) (*ReviewPreparation, error) {
	if err := o.writeReview(ctx, `UPDATE review_preparations SET cancelled=1 WHERE review_id=? AND confirmed=0`, id); err != nil {
		return nil, err
	}
	if !o.workMu.TryLock() {
		return &ReviewPreparation{Status: "CANCELLING"}, nil
	}
	defer o.workMu.Unlock()
	run := o.snapshot()
	p, err := run.reviewLease(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return &ReviewPreparation{Status: "CANCELLED"}, nil
	}
	if err != nil {
		return nil, err
	}
	if !p.created {
		return &ReviewPreparation{Status: "CANCELLED"}, run.releaseReview(ctx, p, false)
	}
	if p.confirmed {
		return &ReviewPreparation{Status: "RETAINED", Message: "Mapping confirmed; series kept in Sonarr"}, nil
	}
	return run.pollReview(ctx, p)
}

// ConfirmReviewMapping atomically saves a mapping and releases preparation ownership.
func (o *Orchestrator) ConfirmReviewMapping(ctx context.Context, id, tvdbID, season int, numbers []int, required bool, save func(context.Context, *sql.Tx) error) error {
	if !o.workMu.TryLock() {
		return ErrSyncInProgress
	}
	defer o.workMu.Unlock()
	run := o.snapshot()
	p, err := run.reviewLease(ctx, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	prepared := err == nil
	if required && !prepared {
		return fmt.Errorf("%w: load episodes again before confirming", ErrReviewPreparationChanged)
	}
	if prepared {
		if p.cancelled || p.confirmed || p.tvdbID != tvdbID {
			return fmt.Errorf("%w: target changed or was cancelled", ErrReviewPreparationChanged)
		}
		result, err := run.pollReview(ctx, p)
		if err != nil {
			return err
		}
		if result.Status != "READY" {
			return fmt.Errorf("%w: wait for episode preparation before confirming", ErrReviewPreparationChanged)
		}
		validSeason := false
		for _, value := range result.Series.Seasons {
			validSeason = validSeason || value.SeasonNumber == season
		}
		if !validSeason {
			return fmt.Errorf("%w: selected season is unavailable", ErrReviewPreparationChanged)
		}
		if len(numbers) > 0 {
			if _, ok := resolveOverrideEpisodes(result.Episodes, season, numbers); !ok {
				return fmt.Errorf("%w: selected episodes are unavailable", ErrReviewPreparationChanged)
			}
		}
	}
	err = run.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if prepared {
			res, err := tx.ExecContext(ctx, `UPDATE review_preparations SET confirmed=1 WHERE review_id=? AND cancelled=0`, id)
			if err != nil {
				return err
			}
			if count, err := res.RowsAffected(); err != nil {
				return err
			} else if count != 1 {
				return fmt.Errorf("%w: preparation was cancelled", ErrReviewPreparationChanged)
			}
		}
		return save(ctx, tx)
	})
	if err != nil {
		return err
	}
	if prepared {
		p.confirmed = true
		_ = run.releaseReview(ctx, p, true)
	} // Failed tag cleanup remains durable for maintenance.
	return nil
}

// MaintainReviewPreparations retries cancellation/confirmation cleanup and expires abandoned reviews.
func (o *Orchestrator) MaintainReviewPreparations(ctx context.Context) error {
	if !o.workMu.TryLock() {
		return ErrSyncInProgress
	}
	defer o.workMu.Unlock()
	run := o.snapshot()
	if err := run.writeReview(ctx, `UPDATE review_preparations SET cancelled=1 WHERE confirmed=0 AND updated_at < datetime('now','-30 minutes')`); err != nil {
		return err
	}
	var ids []int
	if err := run.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		rows, err := q.QueryContext(ctx, `SELECT review_id FROM review_preparations WHERE cancelled=1 OR confirmed=1`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int
			if err = rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	}); err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		p, err := run.reviewLease(ctx, id)
		if err == nil {
			_, err = run.pollReview(ctx, p)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (o *Orchestrator) reviewBlocksSeries(ctx context.Context, seriesID, tvdbID int) (bool, error) {
	var count int
	err := o.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_preparations WHERE endpoint=? AND confirmed=0 AND ((series_id>0 AND series_id=?) OR tvdb_id=?)`, o.servarrEndpoint("SONARR"), seriesID, tvdbID).Scan(&count)
	})
	return count > 0, err
}

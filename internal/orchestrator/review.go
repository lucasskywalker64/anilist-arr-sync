package orchestrator

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

type reviewLease struct {
	id, tvdbID, seriesID, tagID   int
	endpoint, token, baseline     string
	created, cancelled, confirmed bool
}

func (o *Orchestrator) reviewLease(ctx context.Context, id int) (reviewLease, error) {
	p := reviewLease{id: id}
	err := o.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, `SELECT endpoint,tvdb_id,series_id,tag_id,token,baseline,created,cancelled,confirmed FROM review_preparations WHERE review_id=?`, id).Scan(&p.endpoint, &p.tvdbID, &p.seriesID, &p.tagID, &p.token, &p.baseline, &p.created, &p.cancelled, &p.confirmed)
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
		p.tagID, err = run.sonarr.EnsureTag(ctx, "anilist-review-"+p.token)
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
		series, err = run.sonarr.AddSeries(ctx, servarr.AddSeriesRequest{TVDBID: tvdbID, Title: series.Title, RootFolderPath: run.cfg.SonarrRootFolderPath, QualityProfileID: run.cfg.SonarrQualityProfileID, SeriesType: run.cfg.SonarrSeriesType, SeasonFolder: true, Monitored: false, MonitorNewItems: "none", Seasons: seasons, Tags: tags, AddOptions: &servarr.AddSeriesOptions{Monitor: "none"}})
		if err != nil {
			return nil, fmt.Errorf("add outcome is pending verification; cancel or retry this preparation: %w", err)
		}
		p.seriesID = series.ID
		p.baseline = reviewFingerprint(series)
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err = run.writeReview(saveCtx, `UPDATE review_preparations SET series_id=?,baseline=? WHERE review_id=?`, p.seriesID, p.baseline, id); err != nil {
			return nil, err
		}
	}
	return run.pollReview(ctx, p)
}

func reviewFingerprint(s *servarr.Series) string {
	tags := slices.Clone(s.Tags)
	slices.Sort(tags)
	value, _ := json.Marshal(struct {
		Path    string
		Profile int
		Type    string
		Tags    []int
	}{s.Path, s.QualityProfileID, s.SeriesType, tags})
	return string(value)
}

func (o *Orchestrator) pollReview(ctx context.Context, p reviewLease) (*ReviewPreparation, error) {
	if o.sonarr == nil || p.endpoint != o.sonarr.BaseURL() {
		return nil, errors.New("sonarr connection changed; restore the original connection to finish this preparation")
	}
	result := &ReviewPreparation{Status: "WAITING", Message: "Loading Sonarr episode metadata", Created: p.created}
	if p.seriesID == 0 {
		series, err := o.sonarr.LookupSeriesByTVDBID(ctx, p.tvdbID)
		if errors.Is(err, servarr.ErrNotFound) || (err == nil && series.ID == 0) {
			if p.cancelled {
				return result, o.releaseReview(ctx, p, false)
			}
			return nil, errors.New("the series was not added; cancel this preparation and try again")
		}
		if err != nil {
			return nil, err
		}
		if !slices.Contains(series.Tags, p.tagID) || p.tagID == 0 {
			return nil, errors.New("add outcome is uncertain; existing series was not claimed or removed")
		}
		p.seriesID = series.ID
		p.baseline = reviewFingerprint(series)
		if err = o.writeReview(ctx, `UPDATE review_preparations SET series_id=?,baseline=? WHERE review_id=?`, p.seriesID, p.baseline, p.id); err != nil {
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
	result.Series = series
	if p.confirmed {
		return result, o.releaseReview(ctx, p, true)
	}
	episodes, err := o.sonarr.GetEpisodes(ctx, p.seriesID)
	if err != nil {
		return nil, err
	}
	running, failed, complete := false, false, false
	if p.created || series.AddOptions != nil {
		commands, err := o.sonarr.GetCommands(ctx)
		if err != nil {
			return nil, err
		}
		for _, cmd := range commands {
			if cmd.Name == "RefreshSeries" && cmd.Body.IsNewSeries && slices.Contains(cmd.Body.SeriesIDs, p.seriesID) {
				switch cmd.Status {
				case "queued", "started":
					running = true
				case "failed", "aborted":
					failed = true
				case "completed":
					complete = true
				}
			}
		}
	} else {
		complete = true
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
		if adopted || !unmonitored || reviewFingerprint(series) != p.baseline || !slices.Contains(series.Tags, p.tagID) {
			result.Status = "RETAINED"
			result.Message = "Series changed or was adopted; it was left in Sonarr"
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

func (o *Orchestrator) releaseReview(ctx context.Context, p reviewLease, keep bool) error {
	if p.created && p.tagID > 0 {
		if keep && p.seriesID > 0 {
			series, err := o.sonarr.GetSeriesByID(ctx, p.seriesID)
			if err != nil && !errors.Is(err, servarr.ErrNotFound) {
				return err
			}
			if err == nil && slices.Contains(series.Tags, p.tagID) {
				series.Tags = slices.DeleteFunc(series.Tags, func(id int) bool { return id == p.tagID })
				if _, err = o.sonarr.UpdateSeries(ctx, series); err != nil {
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
	if !o.workMu.TryLock() {
		return nil, ErrSyncInProgress
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
	if err = run.writeReview(ctx, `UPDATE review_preparations SET cancelled=1 WHERE review_id=?`, id); err != nil {
		return nil, err
	}
	p.cancelled = true
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
		return errors.New("preparation expired; load episodes again before confirming")
	}
	if prepared {
		if p.cancelled || p.confirmed || p.tvdbID != tvdbID {
			return errors.New("preparation target changed or was cancelled")
		}
		result, err := run.pollReview(ctx, p)
		if err != nil {
			return err
		}
		if result.Status != "READY" {
			return errors.New("wait for episode preparation before confirming")
		}
		validSeason := false
		for _, value := range result.Series.Seasons {
			validSeason = validSeason || value.SeasonNumber == season
		}
		if !validSeason {
			return errors.New("selected season is not in this series")
		}
		if len(numbers) > 0 {
			if _, ok := resolveOverrideEpisodes(result.Episodes, season, numbers); !ok {
				return errors.New("selected episodes are not in this season")
			}
		}
	}
	err = run.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := save(ctx, tx); err != nil {
			return err
		}
		if prepared {
			_, err := tx.ExecContext(ctx, `UPDATE review_preparations SET confirmed=1 WHERE review_id=?`, id)
			return err
		}
		return nil
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

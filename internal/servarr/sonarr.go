package servarr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Season represents season-level metadata and monitoring in Sonarr.
type Season struct {
	SeasonNumber int  `json:"seasonNumber"`
	Monitored    bool `json:"monitored"`
}

// AddSeriesOptions defines optional parameters when adding a series to Sonarr.
type AddSeriesOptions struct {
	SearchForMissingEpisodes     bool   `json:"searchForMissingEpisodes"`
	SearchForCutoffUnmetEpisodes bool   `json:"searchForCutoffUnmetEpisodes,omitempty"`
	Monitor                      string `json:"monitor,omitempty"`
}

// AddSeriesRequest contains the parameters required to add a new series to Sonarr.
type AddSeriesRequest struct {
	Title            string            `json:"title"`
	TVDBID           int               `json:"tvdbId"`
	QualityProfileID int               `json:"qualityProfileId"`
	RootFolderPath   string            `json:"rootFolderPath"`
	SeriesType       string            `json:"seriesType,omitempty"`
	Monitored        bool              `json:"monitored"`
	SeasonFolder     bool              `json:"seasonFolder"`
	Seasons          []Season          `json:"seasons,omitempty"`
	Tags             []int             `json:"tags,omitempty"`
	AddOptions       *AddSeriesOptions `json:"addOptions,omitempty"`
}

// Series represents a Sonarr series entry.
type Series struct {
	ID               int               `json:"id,omitempty"`
	Title            string            `json:"title"`
	SortTitle        string            `json:"sortTitle,omitempty"`
	Status           string            `json:"status,omitempty"`
	Overview         string            `json:"overview,omitempty"`
	Monitored        bool              `json:"monitored"`
	QualityProfileID int               `json:"qualityProfileId,omitempty"`
	RootFolderPath   string            `json:"rootFolderPath,omitempty"`
	Path             string            `json:"path,omitempty"`
	TVDBID           int               `json:"tvdbId"`
	TitleSlug        string            `json:"titleSlug,omitempty"`
	SeriesType       string            `json:"seriesType,omitempty"`
	SeasonFolder     bool              `json:"seasonFolder"`
	Seasons          []Season          `json:"seasons,omitempty"`
	Tags             []int             `json:"tags,omitempty"`
	AddOptions       *AddSeriesOptions `json:"addOptions,omitempty"`
}

// SonarrClient is a typed client for the Sonarr REST v3 API.
type SonarrClient struct {
	*Client
}

// NewSonarrClient creates a new Sonarr REST v3 client.
func NewSonarrClient(baseURL, apiKey string, opts ...Option) (*SonarrClient, error) {
	base, err := NewClient(baseURL, apiKey, opts...)
	if err != nil {
		return nil, err
	}
	return &SonarrClient{Client: base}, nil
}

// LookupSeriesByTVDBID looks up series metadata from Sonarr by its TVDB ID.
func (c *SonarrClient) LookupSeriesByTVDBID(ctx context.Context, tvdbID int) (*Series, error) {
	term := fmt.Sprintf("tvdb:%d", tvdbID)
	endpoint := fmt.Sprintf("/api/v3/series/lookup?term=%s", url.QueryEscape(term))
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var results []Series
	_, err = c.do(req, &results)
	if err != nil {
		return nil, fmt.Errorf("lookup series tvdb id %d: %w", tvdbID, err)
	}

	for i := range results {
		if results[i].TVDBID == tvdbID {
			series := results[i]
			return &series, nil
		}
	}

	return nil, ErrNotFound
}

// AddSeries adds a new series to Sonarr.
func (c *SonarrClient) AddSeries(ctx context.Context, req AddSeriesRequest) (*Series, error) {
	httpReq, err := c.newRequest(ctx, http.MethodPost, "/api/v3/series", req)
	if err != nil {
		return nil, err
	}

	var created Series
	_, err = c.do(httpReq, &created)
	if err != nil {
		return nil, fmt.Errorf("add series %q (tvdb %d): %w", req.Title, req.TVDBID, err)
	}

	return &created, nil
}

// Episode represents an episode record returned by Sonarr.
type Episode struct {
	ID            int       `json:"id"`
	SeriesID      int       `json:"seriesId"`
	SeasonNumber  int       `json:"seasonNumber"`
	EpisodeNumber int       `json:"episodeNumber"`
	Title         string    `json:"title"`
	AirDate       string    `json:"airDate"`
	AirDateUtc    time.Time `json:"airDateUtc"`
	Monitored     bool      `json:"monitored"`
	FinaleType    string    `json:"finaleType"`
}

// PremiereDate returns the parsed air date of the episode.
// It prioritizes AirDateUtc, falling back to parsing the AirDate date string ("2006-01-02").
func (e Episode) PremiereDate() (time.Time, bool) {
	if !e.AirDateUtc.IsZero() {
		return e.AirDateUtc, true
	}
	if strings.TrimSpace(e.AirDate) != "" {
		t, err := time.Parse("2006-01-02", e.AirDate)
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// GetEpisodes queries all episodes for a given series from Sonarr.
func (c *SonarrClient) GetEpisodes(ctx context.Context, seriesID int) ([]Episode, error) {
	endpoint := fmt.Sprintf("/api/v3/episode?seriesId=%d", seriesID)
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var episodes []Episode
	_, err = c.do(req, &episodes)
	if err != nil {
		return nil, fmt.Errorf("get episodes for series %d: %w", seriesID, err)
	}

	return episodes, nil
}

// GetSeriesByID retrieves a series by its internal Sonarr ID.
func (c *SonarrClient) GetSeriesByID(ctx context.Context, id int) (*Series, error) {
	endpoint := fmt.Sprintf("/api/v3/series/%d", id)
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var series Series
	_, err = c.do(req, &series)
	if err != nil {
		return nil, fmt.Errorf("get series %d: %w", id, err)
	}

	return &series, nil
}

// UpdateSeries updates an existing series in Sonarr.
// If Sonarr returns an HTTP 409 Conflict, it refetches the latest series model,
// reapplies season monitoring and series settings, and retries the update.
func (c *SonarrClient) UpdateSeries(ctx context.Context, series *Series) (*Series, error) {
	if series == nil {
		return nil, errors.New("series cannot be nil")
	}

	const maxRetries = 3
	current := *series

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := c.newRequest(ctx, http.MethodPut, "/api/v3/series", current)
		if err != nil {
			return nil, err
		}

		var updated Series
		_, err = c.do(req, &updated)
		if err == nil {
			return &updated, nil
		}

		if errors.Is(err, ErrConflict) {
			if attempt == maxRetries {
				return nil, fmt.Errorf("update series %d: %w", current.ID, ErrConflict)
			}

			refreshed, getErr := c.GetSeriesByID(ctx, current.ID)
			if getErr != nil {
				return nil, fmt.Errorf("refresh series %d after 409 conflict: %w", current.ID, getErr)
			}

			// Overlay desired season monitoring onto refreshed model
			seasonMonitored := make(map[int]bool, len(series.Seasons))
			for _, s := range series.Seasons {
				seasonMonitored[s.SeasonNumber] = s.Monitored
			}
			for i := range refreshed.Seasons {
				if monitored, ok := seasonMonitored[refreshed.Seasons[i].SeasonNumber]; ok {
					refreshed.Seasons[i].Monitored = monitored
				}
			}

			refreshed.Monitored = series.Monitored
			if len(series.Tags) > 0 {
				refreshed.Tags = series.Tags
			}
			if series.QualityProfileID > 0 {
				refreshed.QualityProfileID = series.QualityProfileID
			}
			if series.RootFolderPath != "" {
				refreshed.RootFolderPath = series.RootFolderPath
			}
			if series.SeriesType != "" {
				refreshed.SeriesType = series.SeriesType
			}
			refreshed.SeasonFolder = series.SeasonFolder

			current = *refreshed
			continue
		}

		return nil, fmt.Errorf("update series %d: %w", current.ID, err)
	}

	return nil, fmt.Errorf("update series %d: %w", current.ID, ErrConflict)
}

// SearchSeason triggers an automatic search in Sonarr for a specific season of a series.
func (c *SonarrClient) SearchSeason(ctx context.Context, seriesID int, seasonNumber int) (*Command, error) {
	payload := map[string]any{
		"name":         "SeasonSearch",
		"seriesId":     seriesID,
		"seasonNumber": seasonNumber,
	}

	return c.ExecuteCommand(ctx, payload)
}

// MonitorEpisodes updates the monitored state for a specific list of episode IDs.
func (c *SonarrClient) MonitorEpisodes(ctx context.Context, episodeIDs []int, monitored bool) error {
	if len(episodeIDs) == 0 {
		return errors.New("at least one episode id is required to monitor")
	}

	payload := map[string]any{
		"episodeIds": episodeIDs,
		"monitored":  monitored,
	}

	req, err := c.newRequest(ctx, http.MethodPut, "/api/v3/episode/monitor", payload)
	if err != nil {
		return err
	}

	_, err = c.do(req, nil)
	if err != nil {
		return fmt.Errorf("monitor episodes: %w", err)
	}

	return nil
}

package servarr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Movie represents a Radarr movie entry.
type Movie struct {
	ID                  int              `json:"id,omitempty"`
	Title               string           `json:"title"`
	OriginalTitle       string           `json:"originalTitle,omitempty"`
	CleanTitle          string           `json:"cleanTitle,omitempty"`
	SortTitle           string           `json:"sortTitle,omitempty"`
	Status              string           `json:"status,omitempty"`
	Overview            string           `json:"overview,omitempty"`
	Monitored           bool             `json:"monitored"`
	MinimumAvailability string           `json:"minimumAvailability,omitempty"`
	QualityProfileID    int              `json:"qualityProfileId,omitempty"`
	RootFolderPath      string           `json:"rootFolderPath,omitempty"`
	Path                string           `json:"path,omitempty"`
	TMDBID              int              `json:"tmdbId"`
	IMDBID              string           `json:"imdbId,omitempty"`
	TitleSlug           string           `json:"titleSlug,omitempty"`
	Year                int              `json:"year,omitempty"`
	Tags                []int            `json:"tags,omitempty"`
	HasFile             bool             `json:"hasFile"`
	MovieFileID         int              `json:"movieFileId"`
	AddOptions          *AddMovieOptions `json:"addOptions,omitempty"`
}

// AddMovieOptions defines optional parameters when adding a movie to Radarr.
type AddMovieOptions struct {
	SearchForMovie bool   `json:"searchForMovie"`
	Monitor        string `json:"monitor,omitempty"`
}

// AddMovieRequest contains the parameters required to add a new movie to Radarr.
type AddMovieRequest struct {
	Title               string           `json:"title"`
	TMDBID              int              `json:"tmdbId"`
	Year                int              `json:"year,omitempty"`
	QualityProfileID    int              `json:"qualityProfileId"`
	RootFolderPath      string           `json:"rootFolderPath"`
	Monitored           bool             `json:"monitored"`
	MinimumAvailability string           `json:"minimumAvailability,omitempty"`
	Tags                []int            `json:"tags,omitempty"`
	AddOptions          *AddMovieOptions `json:"addOptions,omitempty"`
}

// RadarrClient is a typed client for the Radarr REST v3 API.
type RadarrClient struct {
	*Client
}

// NewRadarrClient creates a new Radarr REST v3 client.
func NewRadarrClient(baseURL, apiKey string, opts ...Option) (*RadarrClient, error) {
	base, err := NewClient(baseURL, apiKey, opts...)
	if err != nil {
		return nil, err
	}
	return &RadarrClient{Client: base}, nil
}

// LookupMovieByTMDBID retrieves a library movie, including its tags and file ID.
func (c *RadarrClient) LookupMovieByTMDBID(ctx context.Context, tmdbID int) (*Movie, error) {
	endpoint := fmt.Sprintf("/api/v3/movie?tmdbId=%d", tmdbID)
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var movies []Movie
	_, err = c.do(req, &movies)
	if err != nil {
		return nil, fmt.Errorf("lookup library movie tmdb id %d: %w", tmdbID, err)
	}

	for i := range movies {
		if movies[i].TMDBID == tmdbID {
			movie := movies[i]
			return &movie, nil
		}
	}

	return nil, ErrNotFound
}

// AddMovie adds a new movie to Radarr.
func (c *RadarrClient) AddMovie(ctx context.Context, req AddMovieRequest) (*Movie, error) {
	httpReq, err := c.newRequest(ctx, http.MethodPost, "/api/v3/movie", req)
	if err != nil {
		return nil, err
	}

	var created Movie
	_, err = c.do(httpReq, &created)
	if err != nil {
		return nil, fmt.Errorf("add movie %q (tmdb %d): %w", req.Title, req.TMDBID, err)
	}

	return &created, nil
}

// SearchMovies triggers an automatic search in Radarr for the specified movie IDs.
func (c *RadarrClient) SearchMovies(ctx context.Context, movieIDs ...int) (*Command, error) {
	if len(movieIDs) == 0 {
		return nil, errors.New("at least one movie id is required to search")
	}

	payload := map[string]any{
		"name":     "MoviesSearch",
		"movieIds": movieIDs,
	}

	return c.ExecuteCommand(ctx, payload)
}

// GetMovieByID retrieves a movie by its internal Radarr ID.
func (c *RadarrClient) GetMovieByID(ctx context.Context, id int) (*Movie, error) {
	endpoint := fmt.Sprintf("/api/v3/movie/%d", id)
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var movie Movie
	_, err = c.do(req, &movie)
	if err != nil {
		return nil, fmt.Errorf("get movie %d: %w", id, err)
	}

	return &movie, nil
}

// UpdateMovie updates an existing movie in Radarr.
func (c *RadarrClient) UpdateMovie(ctx context.Context, movie *Movie) (*Movie, error) {
	if movie == nil {
		return nil, errors.New("movie cannot be nil")
	}

	req, err := c.newRequest(ctx, http.MethodPut, "/api/v3/movie", movie)
	if err != nil {
		return nil, err
	}

	var updated Movie
	_, err = c.do(req, &updated)
	if err != nil {
		return nil, fmt.Errorf("update movie %d: %w", movie.ID, err)
	}

	return &updated, nil
}

// DeleteMovieFile deletes a movie file from Radarr.
// HTTP 404 responses are treated as idempotent successes.
func (c *RadarrClient) DeleteMovieFile(ctx context.Context, movieFileID int) error {
	endpoint := fmt.Sprintf("/api/v3/moviefile/%d", movieFileID)
	req, err := c.newRequest(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}

	_, err = c.do(req, nil)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return fmt.Errorf("delete movie file %d: %w", movieFileID, err)
	}

	return nil
}

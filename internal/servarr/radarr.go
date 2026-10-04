package servarr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

// LookupMovieByTMDBID looks up movie metadata from Radarr by its TMDb ID.
func (c *RadarrClient) LookupMovieByTMDBID(ctx context.Context, tmdbID int) (*Movie, error) {
	endpoint := fmt.Sprintf("/api/v3/movie/lookup/tmdb?tmdbId=%d", tmdbID)
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var movie Movie
	_, err = c.do(req, &movie)
	if err != nil {
		// If lookup/tmdb returns 404, fallback to /api/v3/movie/lookup?term=tmdb:{id}
		if errors.Is(err, ErrNotFound) {
			return c.lookupMovieByTerm(ctx, tmdbID)
		}
		return nil, fmt.Errorf("lookup movie tmdb id %d: %w", tmdbID, err)
	}

	if movie.TMDBID == 0 && movie.ID == 0 {
		return nil, ErrNotFound
	}

	return &movie, nil
}

func (c *RadarrClient) lookupMovieByTerm(ctx context.Context, tmdbID int) (*Movie, error) {
	term := fmt.Sprintf("tmdb:%d", tmdbID)
	endpoint := fmt.Sprintf("/api/v3/movie/lookup?term=%s", url.QueryEscape(term))
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var movies []Movie
	_, err = c.do(req, &movies)
	if err != nil {
		return nil, fmt.Errorf("lookup movie term %q: %w", term, err)
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

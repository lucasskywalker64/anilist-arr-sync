// Package anilist provides a client for querying the public AniList GraphQL API,
// supporting watchlist retrieval, rate limiting, and exponential backoff retry handling.
package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// MediaListStatus represents the status of a media item in a user's AniList watchlist.
type MediaListStatus string

const (
	// StatusCurrent represents media actively being watched.
	StatusCurrent MediaListStatus = "CURRENT"
	// StatusPlanning represents media planned to be watched.
	StatusPlanning MediaListStatus = "PLANNING"
	// StatusCompleted represents media that has been completed.
	StatusCompleted MediaListStatus = "COMPLETED"
	// StatusDropped represents media that has been dropped.
	StatusDropped MediaListStatus = "DROPPED"
	// StatusPaused represents media that is paused.
	StatusPaused MediaListStatus = "PAUSED"
	// StatusRepeating represents media being rewatched.
	StatusRepeating MediaListStatus = "REPEATING"
)

// MediaStatus represents the release status of a media item on AniList.
type MediaStatus string

const (
	// MediaStatusFinished indicates media release has finished.
	MediaStatusFinished MediaStatus = "FINISHED"
	// MediaStatusReleasing indicates media is currently broadcasting.
	MediaStatusReleasing MediaStatus = "RELEASING"
	// MediaStatusNotYetReleased indicates media has not yet been released.
	MediaStatusNotYetReleased MediaStatus = "NOT_YET_RELEASED"
	// MediaStatusCancelled indicates media was cancelled.
	MediaStatusCancelled MediaStatus = "CANCELLED"
	// MediaStatusHiatus indicates media release is on hiatus.
	MediaStatusHiatus MediaStatus = "HIATUS"
)

// Date represents a partial or complete calendar date returned by AniList.
type Date struct {
	Year  *int `json:"year"`
	Month *int `json:"month"`
	Day   *int `json:"day"`
}

// MediaTitle represents the localized and romanized titles of a media item.
type MediaTitle struct {
	Romaji        string `json:"romaji"`
	English       string `json:"english"`
	Native        string `json:"native"`
	UserPreferred string `json:"userPreferred"`
}

// Media represents an anime media entity on AniList.
type Media struct {
	ID        int         `json:"id"`
	IDMal     *int        `json:"idMal"`
	Title     MediaTitle  `json:"title"`
	Format    string      `json:"format"`
	Status    MediaStatus `json:"status"`
	StartDate Date        `json:"startDate"`
	Episodes  *int        `json:"episodes"`
	Synonyms  []string    `json:"synonyms"`
}

// MediaListEntry represents an individual entry in a user's AniList media collection.
type MediaListEntry struct {
	ID     int             `json:"id"`
	Status MediaListStatus `json:"status"`
	Media  Media           `json:"media"`
}

// WatchlistFilter defines the parameters used to filter user watchlist queries.
type WatchlistFilter struct {
	Username          string
	Statuses          []MediaListStatus
	IncludeUnreleased bool
}

const (
	defaultEndpoint          = "https://graphql.anilist.co"
	defaultUserAgent         = "AniList-Arr-Sync/1.0"
	defaultRequestsPerMinute = 80
	defaultBackoffBase       = 1 * time.Second
	defaultMaxRetries        = 5
	defaultHTTPTimeout       = 30 * time.Second
)

type limiter struct {
	mu           sync.Mutex
	interval     time.Duration
	lastExecuted time.Time
}

func newLimiter(requestsPerMinute int) *limiter {
	if requestsPerMinute <= 0 {
		requestsPerMinute = defaultRequestsPerMinute
	}
	return &limiter{
		interval: time.Minute / time.Duration(requestsPerMinute),
	}
}

func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	var targetTime time.Time
	if l.lastExecuted.IsZero() || now.After(l.lastExecuted.Add(l.interval)) {
		targetTime = now
	} else {
		targetTime = l.lastExecuted.Add(l.interval)
	}
	l.lastExecuted = targetTime
	waitDuration := time.Until(targetTime)
	l.mu.Unlock()

	if waitDuration > 0 {
		timer := time.NewTimer(waitDuration)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	} else if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// Client is a GraphQL client for interacting with the public AniList API.
type Client struct {
	endpoint    string
	userAgent   string
	httpClient  *http.Client
	rateLimiter *limiter
	backoffBase time.Duration
	maxRetries  int
}

// ClientOption configures an AniList client.
type ClientOption func(*Client)

// WithEndpoint configures a custom GraphQL endpoint URL.
func WithEndpoint(endpoint string) ClientOption {
	return func(c *Client) {
		if endpoint != "" {
			c.endpoint = endpoint
		}
	}
}

// WithHTTPClient configures a custom HTTP client for network requests.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithRateLimit configures outbound request throttling capped at requestsPerMinute.
func WithRateLimit(requestsPerMinute int) ClientOption {
	return func(c *Client) {
		if requestsPerMinute > 0 {
			c.rateLimiter = newLimiter(requestsPerMinute)
		}
	}
}

// WithBackoffBase sets the base duration for exponential backoff on HTTP 429 responses.
func WithBackoffBase(duration time.Duration) ClientOption {
	return func(c *Client) {
		if duration > 0 {
			c.backoffBase = duration
		}
	}
}

// WithMaxRetries sets the maximum number of retry attempts upon encountering HTTP 429.
func WithMaxRetries(retries int) ClientOption {
	return func(c *Client) {
		if retries >= 0 {
			c.maxRetries = retries
		}
	}
}

// NewClient initializes an AniList client with documented defaults.
func NewClient(endpoint string, opts ...ClientOption) *Client {
	c := &Client{
		endpoint:    defaultEndpoint,
		userAgent:   defaultUserAgent,
		httpClient:  &http.Client{Timeout: defaultHTTPTimeout},
		rateLimiter: newLimiter(defaultRequestsPerMinute),
		backoffBase: defaultBackoffBase,
		maxRetries:  defaultMaxRetries,
	}
	if endpoint != "" {
		c.endpoint = endpoint
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) parseRetryAfter(header string, attempt int) time.Duration {
	if header != "" {
		if sec, err := strconv.Atoi(header); err == nil && sec >= 0 {
			return time.Duration(sec) * time.Second
		}
		if t, err := http.ParseTime(header); err == nil {
			d := time.Until(t)
			if d > 0 {
				return d
			}
			return 0
		}
	}
	multiplier := 1 << attempt
	return c.backoffBase * time.Duration(multiplier)
}

const watchlistQuery = `
query ($userName: String, $type: MediaType, $statusIn: [MediaListStatus], $chunk: Int) {
  MediaListCollection(userName: $userName, type: $type, status_in: $statusIn, chunk: $chunk) {
    hasNextChunk
    lists {
      name
      status
      entries {
        id
        status
        media {
          id
          idMal
          title {
            romaji
            english
            native
            userPreferred
          }
          format
          status
          startDate {
            year
            month
            day
          }
          episodes
          synonyms
        }
      }
    }
  }
}
`

type watchlistVariables struct {
	UserName string            `json:"userName"`
	Type     string            `json:"type"`
	StatusIn []MediaListStatus `json:"statusIn,omitempty"`
	Chunk    int               `json:"chunk"`
}

type graphQLRequest struct {
	Query     string             `json:"query"`
	Variables watchlistVariables `json:"variables"`
}

type graphQLResponse struct {
	Data struct {
		MediaListCollection *struct {
			HasNextChunk bool `json:"hasNextChunk"`
			Lists        []struct {
				Name    string           `json:"name"`
				Status  MediaListStatus  `json:"status"`
				Entries []MediaListEntry `json:"entries"`
			} `json:"lists"`
		} `json:"MediaListCollection"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// FetchWatchlist queries AniList for the user's media collection applying the provided filters.
func (c *Client) FetchWatchlist(ctx context.Context, filter WatchlistFilter) ([]MediaListEntry, error) {
	if filter.Username == "" {
		return nil, errors.New("username is required")
	}

	var allEntries []MediaListEntry
	seenMediaIDs := make(map[int]bool)
	chunk := 1

	for {
		vars := watchlistVariables{
			UserName: filter.Username,
			Type:     "ANIME",
			StatusIn: filter.Statuses,
			Chunk:    chunk,
		}

		reqBody := graphQLRequest{
			Query:     watchlistQuery,
			Variables: vars,
		}

		encoded, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("marshal graphql request: %w", err)
		}

		var resp *http.Response
		for attempt := 0; attempt <= c.maxRetries; attempt++ {
			if err := c.rateLimiter.wait(ctx); err != nil {
				return nil, fmt.Errorf("rate limiter wait: %w", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(encoded))
			if err != nil {
				return nil, fmt.Errorf("create http request: %w", err)
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", c.userAgent)

			resp, err = c.httpClient.Do(req)
			if err != nil {
				return nil, fmt.Errorf("execute http request: %w", err)
			}

			if resp.StatusCode == http.StatusTooManyRequests {
				retryAfterHeader := resp.Header.Get("Retry-After")
				_ = resp.Body.Close()
				if attempt == c.maxRetries {
					return nil, fmt.Errorf("rate limit exceeded (HTTP 429): max retries (%d) reached", c.maxRetries)
				}

				waitDuration := c.parseRetryAfter(retryAfterHeader, attempt)
				timer := time.NewTimer(waitDuration)

				select {
				case <-timer.C:
					timer.Stop()
					continue
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				}
			}
			break
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		var gqlResp graphQLResponse
		err = json.NewDecoder(resp.Body).Decode(&gqlResp)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode graphql response: %w", err)
		}

		if len(gqlResp.Errors) > 0 {
			errs := make([]error, len(gqlResp.Errors))
			for i, e := range gqlResp.Errors {
				errs[i] = errors.New(e.Message)
			}
			return nil, fmt.Errorf("graphql error: %w", errors.Join(errs...))
		}

		if gqlResp.Data.MediaListCollection == nil {
			break
		}

		for _, l := range gqlResp.Data.MediaListCollection.Lists {
			for _, entry := range l.Entries {
				if seenMediaIDs[entry.Media.ID] {
					continue
				}
				if !filter.IncludeUnreleased && entry.Media.Status == MediaStatusNotYetReleased {
					continue
				}
				seenMediaIDs[entry.Media.ID] = true
				allEntries = append(allEntries, entry)
			}
		}

		if !gqlResp.Data.MediaListCollection.HasNextChunk {
			break
		}
		chunk++
	}

	return allEntries, nil
}

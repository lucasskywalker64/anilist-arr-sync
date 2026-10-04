// Package servarr provides typed HTTP clients for Servarr REST v3 services
// including Sonarr and Radarr, supporting authentication, tag management,
// command execution, and conflict resolution.
package servarr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrNotFound indicates the requested resource does not exist.
	ErrNotFound = errors.New("servarr resource not found")
	// ErrConflict indicates an update conflict occurred (HTTP 409).
	ErrConflict = errors.New("servarr update conflict")
	// ErrUnauthorized indicates authentication failed (HTTP 401).
	ErrUnauthorized = errors.New("servarr unauthorized")
)

// APIError represents an error returned by the Servarr REST v3 API.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("servarr api error: status %d: %s", e.StatusCode, e.Message)
}

// Tag represents a Servarr tag entry.
type Tag struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// Command represents a dispatched Servarr command.
type Command struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	CommandName string    `json:"commandName"`
	Status      string    `json:"status"`
	Queued      time.Time `json:"queued"`
	Started     time.Time `json:"started"`
	Ended       time.Time `json:"ended"`
}

// Client represents a base HTTP client for Servarr REST v3 services.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	timeout    *time.Duration
}

// Option configures a Client instance.
type Option func(*Client)

// WithHTTPClient configures a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithTimeout configures a custom timeout for the HTTP client.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.timeout = &timeout
	}
}

// NewClient creates and validates a base Servarr client.
func NewClient(rawURL, apiKey string, opts ...Option) (*Client, error) {
	trimmedURL := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if trimmedURL == "" {
		return nil, errors.New("servarr base url cannot be empty")
	}

	parsed, err := url.Parse(trimmedURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid servarr base url %q", rawURL)
	}

	c := &Client{
		baseURL: trimmedURL,
		apiKey:  strings.TrimSpace(apiKey),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.timeout != nil {
		hc := *c.httpClient
		hc.Timeout = *c.timeout
		c.httpClient = &hc
	}

	return c, nil
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) newRequest(ctx context.Context, method, endpoint string, body any) (*http.Request, error) {
	relPath := strings.TrimPrefix(endpoint, "/")
	fullURL := fmt.Sprintf("%s/%s", c.baseURL, relPath)

	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	return req, nil
}

func (c *Client) do(req *http.Request, target any) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if target != nil && resp.StatusCode != http.StatusNoContent {
			if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
				return resp, fmt.Errorf("decode response json: %w", err)
			}
		}
		return resp, nil
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := strings.TrimSpace(string(bodyBytes))

	switch resp.StatusCode {
	case http.StatusNotFound:
		return resp, ErrNotFound
	case http.StatusConflict:
		return resp, ErrConflict
	case http.StatusUnauthorized:
		return resp, ErrUnauthorized
	default:
		return resp, &APIError{
			StatusCode: resp.StatusCode,
			Message:    bodyStr,
		}
	}
}

// GetTags retrieves all tags defined in the Servarr instance.
func (c *Client) GetTags(ctx context.Context) ([]Tag, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v3/tag", nil)
	if err != nil {
		return nil, err
	}

	var tags []Tag
	_, err = c.do(req, &tags)
	if err != nil {
		return nil, fmt.Errorf("get tags: %w", err)
	}
	return tags, nil
}

// CreateTag creates a new tag with the specified label.
func (c *Client) CreateTag(ctx context.Context, label string) (*Tag, error) {
	payload := map[string]string{"label": label}
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v3/tag", payload)
	if err != nil {
		return nil, err
	}

	var tag Tag
	_, err = c.do(req, &tag)
	if err != nil {
		return nil, fmt.Errorf("create tag: %w", err)
	}
	return &tag, nil
}

// EnsureTag finds an existing tag by label (case-insensitive) or creates it if not found.
func (c *Client) EnsureTag(ctx context.Context, label string) (int, error) {
	tags, err := c.GetTags(ctx)
	if err != nil {
		return 0, err
	}

	cleanLabel := strings.ToLower(strings.TrimSpace(label))
	for _, t := range tags {
		if strings.ToLower(strings.TrimSpace(t.Label)) == cleanLabel {
			return t.ID, nil
		}
	}

	created, err := c.CreateTag(ctx, label)
	if err != nil {
		return 0, err
	}
	return created.ID, nil
}

// ExecuteCommand dispatches an administrative or search command via POST /api/v3/command.
func (c *Client) ExecuteCommand(ctx context.Context, body any) (*Command, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v3/command", body)
	if err != nil {
		return nil, err
	}

	var cmd Command
	_, err = c.do(req, &cmd)
	if err != nil {
		return nil, fmt.Errorf("execute command: %w", err)
	}
	return &cmd, nil
}

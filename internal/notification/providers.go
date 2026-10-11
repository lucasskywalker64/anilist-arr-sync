package notification

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

const defaultRequestTimeout = 10 * time.Second

// DiscordConfig holds parsed connection parameters for Discord webhooks.
type DiscordConfig struct {
	URL string `json:"url"`
}

// WebhookConfig holds parsed connection parameters for generic webhooks.
type WebhookConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// DiscordProvider sends formatted event embeds to Discord webhook endpoints.
type DiscordProvider struct {
	webhookURL string
	client     *http.Client
}

// NewDiscordProvider validates the webhook URL and constructs a DiscordProvider.
func NewDiscordProvider(rawURL string, client *http.Client) (*DiscordProvider, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, errors.New("discord webhook URL is required")
	}
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid discord webhook URL: %q", rawURL)
	}

	c := client
	if c == nil {
		c = &http.Client{Timeout: defaultRequestTimeout}
	}

	return &DiscordProvider{
		webhookURL: trimmed,
		client:     c,
	}, nil
}

// Name returns the provider identifier.
func (d *DiscordProvider) Name() string {
	return "DISCORD"
}

// Test sends a lightweight verification embed to the Discord webhook.
func (d *DiscordProvider) Test(ctx context.Context) error {
	testEvent := SyncEvent{
		Type:      EventSyncComplete,
		Timestamp: time.Now().UTC(),
		Title:     "AniList Arr Sync: Webhook connection verified successfully.",
	}
	return d.Send(ctx, testEvent)
}

// Send formats the event and posts it to the Discord webhook with a 10-second timeout.
func (d *DiscordProvider) Send(ctx context.Context, event SyncEvent) error {
	payload, err := BuildDiscordPayload(event)
	if err != nil {
		return fmt.Errorf("failed to build discord payload: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal discord payload: %w", err)
	}

	return postJSON(ctx, d.client, d.webhookURL, body, nil)
}

// WebhookProvider sends structured JSON events to generic webhook destinations.
type WebhookProvider struct {
	webhookURL string
	headers    map[string]string
	client     *http.Client
}

// NewWebhookProvider validates the URL and constructs a generic WebhookProvider.
func NewWebhookProvider(rawURL string, headers map[string]string, client *http.Client) (*WebhookProvider, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, errors.New("webhook URL is required")
	}
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid webhook URL: %q", rawURL)
	}

	c := client
	if c == nil {
		c = &http.Client{Timeout: defaultRequestTimeout}
	}

	return &WebhookProvider{
		webhookURL: trimmed,
		headers:    headers,
		client:     c,
	}, nil
}

// Name returns the provider identifier.
func (w *WebhookProvider) Name() string {
	return "WEBHOOK"
}

// Test dispatches a verification payload to the generic webhook destination.
func (w *WebhookProvider) Test(ctx context.Context) error {
	testEvent := SyncEvent{
		Type:      EventSyncComplete,
		Timestamp: time.Now().UTC(),
		Title:     "AniList Arr Sync: Webhook connection verified successfully.",
	}
	return w.Send(ctx, testEvent)
}

// Send formats the generic JSON payload and posts it with a 10-second timeout.
func (w *WebhookProvider) Send(ctx context.Context, event SyncEvent) error {
	payload, err := BuildWebhookPayload(event)
	if err != nil {
		return fmt.Errorf("failed to build webhook payload: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	return postJSON(ctx, w.client, w.webhookURL, body, w.headers)
}

// NewProviderFromConnection instantiates the appropriate Provider
// according to the Connection database record.
func NewProviderFromConnection(conn Connection, client *http.Client) (Provider, error) {
	switch strings.ToUpper(conn.Provider) {
	case "DISCORD":
		var cfg DiscordConfig
		if err := json.Unmarshal([]byte(conn.ConfigJSON), &cfg); err != nil {
			return nil, fmt.Errorf("invalid discord config JSON: %w", err)
		}
		return NewDiscordProvider(cfg.URL, client)

	case "WEBHOOK":
		var cfg WebhookConfig
		if err := json.Unmarshal([]byte(conn.ConfigJSON), &cfg); err != nil {
			return nil, fmt.Errorf("invalid generic webhook config JSON: %w", err)
		}
		return NewWebhookProvider(cfg.URL, cfg.Headers, client)

	default:
		return nil, fmt.Errorf("unsupported notification provider %q", conn.Provider)
	}
}

func postJSON(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	body []byte,
	headers map[string]string,
) error {
	reqCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AniList-Arr-Sync-Notifier/1.0")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respSnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("webhook endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respSnippet)))
	}

	return nil
}

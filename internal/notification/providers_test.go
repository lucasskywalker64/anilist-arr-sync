package notification_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
)

func TestDiscordProvider_Send(t *testing.T) {
	var receivedBody []byte
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	provider, err := notification.NewDiscordProvider(server.URL, server.Client())
	if err != nil {
		t.Fatalf("unexpected error creating Discord provider: %v", err)
	}

	event := notification.SyncEvent{
		Type:          notification.EventMediaAdded,
		Title:         "Demon Slayer",
		MediaType:     "SERIES",
		TargetService: "Sonarr",
		PosterURL:     "https://example.com/poster.jpg",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := provider.Send(ctx, event); err != nil {
		t.Fatalf("unexpected error sending Discord event: %v", err)
	}

	if receivedContentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", receivedContentType)
	}

	var payload notification.DiscordPayload
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal received body: %v", err)
	}
	if len(payload.Embeds) != 1 {
		t.Fatalf("expected 1 embed, got %d", len(payload.Embeds))
	}
	if payload.Embeds[0].Title != "Media Added: Demon Slayer" {
		t.Fatalf("unexpected embed title: %q", payload.Embeds[0].Title)
	}
}

func TestDiscordProvider_Test(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	provider, err := notification.NewDiscordProvider(server.URL, server.Client())
	if err != nil {
		t.Fatalf("unexpected error creating Discord provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := provider.Test(ctx); err != nil {
		t.Fatalf("unexpected error testing Discord provider: %v", err)
	}
	if !called.Load() {
		t.Fatal("expected server to be called during Test")
	}
}

func TestDiscordProvider_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message": "Invalid Webhook Token"}`))
	}))
	defer server.Close()

	provider, err := notification.NewDiscordProvider(server.URL, server.Client())
	if err != nil {
		t.Fatalf("unexpected error creating Discord provider: %v", err)
	}

	err = provider.Test(context.Background())
	if err == nil {
		t.Fatal("expected error on 400 Bad Request, got nil")
	}
}

func TestWebhookProvider_Send(t *testing.T) {
	var receivedBody []byte
	var receivedAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	headers := map[string]string{
		"Authorization": "Bearer test-secret-token",
	}
	provider, err := notification.NewWebhookProvider(server.URL, headers, server.Client())
	if err != nil {
		t.Fatalf("unexpected error creating Webhook provider: %v", err)
	}

	event := notification.SyncEvent{
		Type:      notification.EventSyncError,
		Error:     "disk full",
		Timestamp: time.Date(2026, 10, 11, 3, 0, 0, 0, time.UTC),
	}

	if err := provider.Send(context.Background(), event); err != nil {
		t.Fatalf("unexpected error sending Webhook event: %v", err)
	}

	if receivedAuthHeader != "Bearer test-secret-token" {
		t.Fatalf("expected Authorization header Bearer test-secret-token, got %q", receivedAuthHeader)
	}

	var payload notification.GenericWebhookPayload
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal received body: %v", err)
	}
	if payload.Event != notification.EventSyncError {
		t.Fatalf("expected event OnSyncError, got %q", payload.Event)
	}
	if payload.Data.Error != "disk full" {
		t.Fatalf("expected data error disk full, got %q", payload.Data.Error)
	}
}

func TestWebhookProvider_Test(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider, err := notification.NewWebhookProvider(server.URL, nil, server.Client())
	if err != nil {
		t.Fatalf("unexpected error creating Webhook provider: %v", err)
	}

	if err := provider.Test(context.Background()); err != nil {
		t.Fatalf("unexpected error during Webhook Test: %v", err)
	}
	if !called.Load() {
		t.Fatal("expected server to be called during Test")
	}
}

func TestNewProviderFromConnection(t *testing.T) {
	t.Run("Valid Discord connection", func(t *testing.T) {
		conn := notification.Connection{
			Name:       "My Discord",
			Provider:   "DISCORD",
			ConfigJSON: `{"url": "https://discord.com/api/webhooks/123/xyz"}`,
		}
		p, err := notification.NewProviderFromConnection(conn, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name() != "DISCORD" {
			t.Fatalf("expected name DISCORD, got %q", p.Name())
		}
	})

	t.Run("Valid generic Webhook connection", func(t *testing.T) {
		conn := notification.Connection{
			Name:       "My Webhook",
			Provider:   "WEBHOOK",
			ConfigJSON: `{"url": "https://example.com/hook", "headers": {"X-Custom": "val"}}`,
		}
		p, err := notification.NewProviderFromConnection(conn, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name() != "WEBHOOK" {
			t.Fatalf("expected name WEBHOOK, got %q", p.Name())
		}
	})

	t.Run("Unsupported provider", func(t *testing.T) {
		conn := notification.Connection{
			Name:       "Slack",
			Provider:   "SLACK",
			ConfigJSON: `{"url": "https://example.com"}`,
		}
		_, err := notification.NewProviderFromConnection(conn, nil)
		if err == nil {
			t.Fatal("expected error for unsupported provider, got nil")
		}
	})
}

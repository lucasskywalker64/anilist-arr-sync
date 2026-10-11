package notification_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
)

func TestDispatcher_Dispatch_Asynchronous(t *testing.T) {
	db := setupTestDB(t)
	store := notification.NewStore(db)

	var discordCalls atomic.Int32
	var webhookCalls atomic.Int32

	discordServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		discordCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discordServer.Close()

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		webhookCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	ctx := context.Background()

	// Connection 1: Discord enabled for OnMediaAdded
	_, err := store.Insert(ctx, notification.Connection{
		Name:       "Discord Alert",
		Provider:   "DISCORD",
		ConfigJSON: fmt.Sprintf(`{"url":%q}`, discordServer.URL),
		OnAdded:    true,
		OnReview:   false,
		OnError:    false,
		OnComplete: false,
		OnUpdate:   false,
	})
	if err != nil {
		t.Fatalf("failed to insert discord connection: %v", err)
	}

	// Connection 2: Webhook disabled for OnMediaAdded (only OnComplete)
	_, err = store.Insert(ctx, notification.Connection{
		Name:       "Webhook Complete Only",
		Provider:   "WEBHOOK",
		ConfigJSON: fmt.Sprintf(`{"url":%q}`, webhookServer.URL),
		OnAdded:    false,
		OnReview:   false,
		OnError:    false,
		OnComplete: true,
		OnUpdate:   false,
	})
	if err != nil {
		t.Fatalf("failed to insert webhook connection: %v", err)
	}

	dispatcher := notification.NewDispatcher(store, http.DefaultClient)

	event := notification.SyncEvent{
		Type:          notification.EventMediaAdded,
		Title:         "Attack on Titan",
		MediaType:     "SERIES",
		TargetService: "Sonarr",
	}

	// Dispatch is non-blocking
	start := time.Now()
	dispatcher.Dispatch(event)
	duration := time.Since(start)
	if duration > 100*time.Millisecond {
		t.Fatalf("Dispatch took %v, expected non-blocking execution", duration)
	}

	// Wait for background delivery to complete
	dispatcher.Wait()

	if discordCalls.Load() != 1 {
		t.Fatalf("expected 1 discord call, got %d", discordCalls.Load())
	}
	if webhookCalls.Load() != 0 {
		t.Fatalf("expected 0 webhook calls for disabled event, got %d", webhookCalls.Load())
	}
}

func TestDispatcher_TestConnection(t *testing.T) {
	db := setupTestDB(t)
	store := notification.NewStore(db)

	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			called.Store(true)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	dispatcher := notification.NewDispatcher(store, server.Client())

	conn := notification.Connection{
		Name:       "Test Alert",
		Provider:   "DISCORD",
		ConfigJSON: fmt.Sprintf(`{"url":%q}`, server.URL),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := dispatcher.TestConnection(ctx, conn); err != nil {
		t.Fatalf("unexpected error in TestConnection: %v", err)
	}
	if !called.Load() {
		t.Fatal("expected test notification to reach the server")
	}
}

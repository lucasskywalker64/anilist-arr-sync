package notification_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
)

func TestBuildWebhookPayload(t *testing.T) {
	now := time.Date(2026, 10, 11, 3, 0, 0, 0, time.UTC)
	event := notification.SyncEvent{
		Type:          notification.EventMediaAdded,
		Timestamp:     now,
		Title:         "Attack on Titan",
		MediaType:     "SERIES",
		TargetService: "Sonarr",
		AniListID:     16498,
	}

	payload, err := notification.BuildWebhookPayload(event)
	if err != nil {
		t.Fatalf("unexpected error building webhook payload: %v", err)
	}

	if payload.Event != notification.EventMediaAdded {
		t.Fatalf("expected event %q, got %q", notification.EventMediaAdded, payload.Event)
	}
	if !payload.Timestamp.Equal(now) {
		t.Fatalf("expected timestamp %v, got %v", now, payload.Timestamp)
	}
	if payload.Data.Title != "Attack on Titan" {
		t.Fatalf("expected data title %q, got %q", "Attack on Titan", payload.Data.Title)
	}

	// Verify valid JSON marshaling
	bytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal webhook payload to JSON: %v", err)
	}
	if len(bytes) == 0 {
		t.Fatal("expected non-empty JSON bytes")
	}
}

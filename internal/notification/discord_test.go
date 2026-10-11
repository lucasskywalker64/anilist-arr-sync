package notification_test

import (
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
)

func TestResolvePosterURL(t *testing.T) {
	tests := []struct {
		name        string
		posterURL   string
		color       string
		expectedURL string
	}{
		{
			name:        "Poster URL provided directly",
			posterURL:   "https://s4.anilist.co/file/anilistcdn/media/anime/cover/large/bx1-abc.jpg",
			color:       "#e4a15d",
			expectedURL: "https://s4.anilist.co/file/anilistcdn/media/anime/cover/large/bx1-abc.jpg",
		},
		{
			name:        "Empty poster URL with valid hex color with hash",
			posterURL:   "",
			color:       "#e4a15d",
			expectedURL: "https://dummyimage.com/400x600/e4a15d/e4a15d.png",
		},
		{
			name:        "Empty poster URL with valid hex color without hash",
			posterURL:   "",
			color:       "336699",
			expectedURL: "https://dummyimage.com/400x600/336699/336699.png",
		},
		{
			name:        "Empty poster URL and invalid hex color",
			posterURL:   "",
			color:       "not-a-color",
			expectedURL: "https://dummyimage.com/400x600/2b2d42/2b2d42.png",
		},
		{
			name:        "Empty poster URL and empty color",
			posterURL:   "",
			color:       "",
			expectedURL: "https://dummyimage.com/400x600/2b2d42/2b2d42.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := notification.ResolvePosterURL(tt.posterURL, tt.color)
			if actual != tt.expectedURL {
				t.Fatalf("expected ResolvePosterURL(%q, %q) = %q, got %q", tt.posterURL, tt.color, tt.expectedURL, actual)
			}
		})
	}
}

func TestBuildDiscordPayload_Events(t *testing.T) {
	now := time.Date(2026, 10, 11, 3, 0, 0, 0, time.UTC)
	seasonNum := 2

	t.Run("OnMediaAdded", func(t *testing.T) {
		event := notification.SyncEvent{
			Type:          notification.EventMediaAdded,
			Timestamp:     now,
			Title:         "Attack on Titan",
			MediaType:     "SERIES",
			TargetService: "Sonarr",
			AniListID:     16498,
			Season:        &seasonNum,
			Episodes:      []int{1, 2, 3},
			PosterURL:     "https://example.com/poster.jpg",
		}

		payload, err := notification.BuildDiscordPayload(event)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(payload.Embeds) != 1 {
			t.Fatalf("expected 1 embed, got %d", len(payload.Embeds))
		}
		embed := payload.Embeds[0]
		if embed.Title == "" {
			t.Fatal("expected non-empty title")
		}
		if embed.Thumbnail == nil || embed.Thumbnail.URL != "https://example.com/poster.jpg" {
			t.Fatalf("expected thumbnail URL https://example.com/poster.jpg, got %+v", embed.Thumbnail)
		}
		if embed.Color == 0 {
			t.Fatal("expected non-zero embed color")
		}
	})

	t.Run("OnReviewRequired", func(t *testing.T) {
		event := notification.SyncEvent{
			Type:      notification.EventReviewRequired,
			Timestamp: now,
			Title:     "Unknown OVA",
			MediaType: "SPECIAL",
			AniListID: 99999,
			Reason:    "SPECIAL_WITHOUT_MAPPING",
		}

		payload, err := notification.BuildDiscordPayload(event)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(payload.Embeds) != 1 {
			t.Fatalf("expected 1 embed, got %d", len(payload.Embeds))
		}
		embed := payload.Embeds[0]
		if embed.Thumbnail == nil || embed.Thumbnail.URL != "https://dummyimage.com/400x600/2b2d42/2b2d42.png" {
			t.Fatalf("expected default thumbnail URL, got %+v", embed.Thumbnail)
		}
	})

	t.Run("OnSyncError", func(t *testing.T) {
		event := notification.SyncEvent{
			Type:      notification.EventSyncError,
			Timestamp: now,
			Error:     "connection refused to Sonarr",
		}

		payload, err := notification.BuildDiscordPayload(event)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(payload.Embeds) != 1 {
			t.Fatalf("expected 1 embed, got %d", len(payload.Embeds))
		}
		embed := payload.Embeds[0]
		if embed.Description == "" && len(embed.Fields) == 0 {
			t.Fatal("expected error details in description or fields")
		}
	})

	t.Run("OnSyncComplete", func(t *testing.T) {
		event := notification.SyncEvent{
			Type:            notification.EventSyncComplete,
			Timestamp:       now,
			ItemsScanned:    50,
			AddedRadarr:     2,
			MonitoredSonarr: 4,
			QueuedReview:    1,
			DurationMs:      1240,
		}

		payload, err := notification.BuildDiscordPayload(event)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(payload.Embeds) != 1 {
			t.Fatalf("expected 1 embed, got %d", len(payload.Embeds))
		}
	})

	t.Run("OnUpdateAvailable", func(t *testing.T) {
		event := notification.SyncEvent{
			Type:           notification.EventUpdateAvailable,
			Timestamp:      now,
			CurrentVersion: "1.0.0",
			NewVersion:     "1.1.0",
			ChangelogURL:   "https://github.com/lucasskywalker64/anilist-arr-sync/releases/v1.1.0",
		}

		payload, err := notification.BuildDiscordPayload(event)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(payload.Embeds) != 1 {
			t.Fatalf("expected 1 embed, got %d", len(payload.Embeds))
		}
	})
}

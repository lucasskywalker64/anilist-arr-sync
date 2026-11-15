package notification_test

import (
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
)

func TestConnectionMatchesEvent(t *testing.T) {
	tests := []struct {
		name      string
		conn      notification.Connection
		eventType notification.EventType
		expected  bool
	}{
		{
			name: "OnMediaAdded enabled",
			conn: notification.Connection{
				OnAdded: true,
			},
			eventType: notification.EventMediaAdded,
			expected:  true,
		},
		{
			name: "OnMediaAdded disabled",
			conn: notification.Connection{
				OnAdded: false,
			},
			eventType: notification.EventMediaAdded,
			expected:  false,
		},
		{
			name: "OnReviewRequired enabled",
			conn: notification.Connection{
				OnReview: true,
			},
			eventType: notification.EventReviewRequired,
			expected:  true,
		},
		{
			name: "OnReviewRequired disabled",
			conn: notification.Connection{
				OnReview: false,
			},
			eventType: notification.EventReviewRequired,
			expected:  false,
		},
		{
			name: "OnSyncError enabled",
			conn: notification.Connection{
				OnError: true,
			},
			eventType: notification.EventSyncError,
			expected:  true,
		},
		{
			name: "OnSyncError disabled",
			conn: notification.Connection{
				OnError: false,
			},
			eventType: notification.EventSyncError,
			expected:  false,
		},
		{
			name: "OnSyncComplete enabled",
			conn: notification.Connection{
				OnComplete: true,
			},
			eventType: notification.EventSyncComplete,
			expected:  true,
		},
		{
			name: "OnSyncComplete disabled",
			conn: notification.Connection{
				OnComplete: false,
			},
			eventType: notification.EventSyncComplete,
			expected:  false,
		},
		{
			name: "OnUpdateAvailable enabled",
			conn: notification.Connection{
				OnUpdate: true,
			},
			eventType: notification.EventUpdateAvailable,
			expected:  true,
		},
		{
			name: "OnUpdateAvailable disabled",
			conn: notification.Connection{
				OnUpdate: false,
			},
			eventType: notification.EventUpdateAvailable,
			expected:  false,
		},
		{
			name:      "Unknown event type",
			conn:      notification.Connection{OnAdded: true},
			eventType: notification.EventType("UNKNOWN"),
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.conn.Matches(tt.eventType)
			if actual != tt.expected {
				t.Fatalf("expected Matches(%q) = %v, got %v", tt.eventType, tt.expected, actual)
			}
		})
	}
}

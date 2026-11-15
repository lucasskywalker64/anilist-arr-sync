// Package notification provides a decoupled notification engine supporting
// Discord webhooks and generic JSON webhooks.
package notification

import (
	"context"
	"time"
)

// EventType identifies the kind of sync or system event being dispatched.
type EventType string

const (
	// EventMediaAdded occurs when media is added or monitored.
	EventMediaAdded EventType = "OnMediaAdded"
	// EventReviewRequired occurs when a title requires manual review.
	EventReviewRequired EventType = "OnReviewRequired"
	// EventSyncError occurs when an error interrupts synchronization.
	EventSyncError EventType = "OnSyncError"
	// EventSyncComplete occurs upon completion of a synchronization pass.
	EventSyncComplete EventType = "OnSyncComplete"
	// EventUpdateAvailable occurs when a new version is detected.
	EventUpdateAvailable EventType = "OnUpdateAvailable"
)

// SyncEvent encapsulates details about a synchronization or lifecycle event.
type SyncEvent struct {
	Type           EventType `json:"type"`
	Timestamp      time.Time `json:"timestamp"`
	Title          string    `json:"title,omitempty"`
	MediaType      string    `json:"mediaType,omitempty"`
	TargetService  string    `json:"targetService,omitempty"`
	AniListID      int       `json:"anilistId,omitempty"`
	Season         *int      `json:"season,omitempty"`
	Episodes       []int     `json:"episodes,omitempty"`
	PosterURL      string    `json:"posterUrl,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	Error          string    `json:"error,omitempty"`
	CurrentVersion string    `json:"currentVersion,omitempty"`
	NewVersion     string    `json:"newVersion,omitempty"`
	ChangelogURL   string    `json:"changelogUrl,omitempty"`

	ItemsScanned      int   `json:"itemsScanned,omitempty"`
	AddedRadarr       int   `json:"addedRadarr,omitempty"`
	MonitoredSonarr   int   `json:"monitoredSonarr,omitempty"`
	UnmonitoredSonarr int   `json:"unmonitoredSonarr,omitempty"`
	UnmonitoredRadarr int   `json:"unmonitoredRadarr,omitempty"`
	QueuedReview      int   `json:"queuedReview,omitempty"`
	DurationMs        int64 `json:"durationMs,omitempty"`
}

// Provider defines the contract for notification dispatchers.
type Provider interface {
	Name() string
	Test(ctx context.Context) error
	Send(ctx context.Context, event SyncEvent) error
}

// Connection represents a configured notification destination in the database.
type Connection struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Provider   string    `json:"provider"`
	ConfigJSON string    `json:"configJson"`
	OnAdded    bool      `json:"onAdded"`
	OnReview   bool      `json:"onReview"`
	OnError    bool      `json:"onError"`
	OnComplete bool      `json:"onComplete"`
	OnUpdate   bool      `json:"onUpdate"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Matches checks whether this connection is configured to receive the given event type.
func (c Connection) Matches(eventType EventType) bool {
	switch eventType {
	case EventMediaAdded:
		return c.OnAdded
	case EventReviewRequired:
		return c.OnReview
	case EventSyncError:
		return c.OnError
	case EventSyncComplete:
		return c.OnComplete
	case EventUpdateAvailable:
		return c.OnUpdate
	default:
		return false
	}
}

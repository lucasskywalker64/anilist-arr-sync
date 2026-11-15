package notification

import (
	"time"
)

// GenericWebhookPayload represents a structured JSON payload delivered to
// generic webhook endpoints.
type GenericWebhookPayload struct {
	Event     EventType `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	Data      SyncEvent `json:"data"`
}

// BuildWebhookPayload transforms a SyncEvent into a structured GenericWebhookPayload.
func BuildWebhookPayload(event SyncEvent) (GenericWebhookPayload, error) {
	ts := event.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return GenericWebhookPayload{
		Event:     event.Type,
		Timestamp: ts,
		Data:      event,
	}, nil
}

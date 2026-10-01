package anilist

import (
	"testing"
	"time"
)

func TestParseRetryAfter_BackoffCappedAtMax(t *testing.T) {
	c := NewClient("")
	// Test high attempt number that would otherwise overflow int64 nanoseconds
	d := c.parseRetryAfter("", 50)
	if d != 5*time.Minute {
		t.Fatalf("expected backoff capped at 5 minutes, got %v", d)
	}

	// Test attempt 0
	d0 := c.parseRetryAfter("", 0)
	if d0 != c.backoffBase {
		t.Fatalf("expected backoffBase for attempt 0, got %v", d0)
	}
}

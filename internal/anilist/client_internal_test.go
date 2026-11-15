package anilist

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter_BackoffCappedAtMax(t *testing.T) {
	c := NewClient("")
	// Test high attempt number that would otherwise overflow int64 nanoseconds
	d := c.parseRetryAfter("", 50)
	if d != defaultMaxBackoff {
		t.Fatalf("expected backoff capped at %v, got %v", defaultMaxBackoff, d)
	}

	// Test attempt 0
	d0 := c.parseRetryAfter("", 0)
	if d0 != c.backoffBase {
		t.Fatalf("expected backoffBase for attempt 0, got %v", d0)
	}
}

func TestParseRetryAfter_RetryAfterHeaderCappedAtMax(t *testing.T) {
	c := NewClient("")

	// Explicit seconds exceeding defaultMaxBackoff
	dNumeric := c.parseRetryAfter("3600", 0)
	if dNumeric != defaultMaxBackoff {
		t.Fatalf("expected Retry-After seconds capped at %v, got %v", defaultMaxBackoff, dNumeric)
	}

	// Explicit HTTP date exceeding defaultMaxBackoff
	futureDate := time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat)
	dDate := c.parseRetryAfter(futureDate, 0)
	if dDate != defaultMaxBackoff {
		t.Fatalf("expected Retry-After HTTP date capped at %v, got %v", defaultMaxBackoff, dDate)
	}

	// Explicit seconds under cap
	dNormal := c.parseRetryAfter("30", 0)
	if dNormal != 30*time.Second {
		t.Fatalf("expected 30 seconds, got %v", dNormal)
	}
}

func TestLimiter_CanceledRequestsDoNotAccumulateDebt(t *testing.T) {
	// 60 requests per minute = 1 second interval
	l := newLimiter(60)

	// First request succeeds immediately
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("unexpected error on first wait: %v", err)
	}

	// Spawn several requests with already-canceled contexts
	for i := 0; i < 5; i++ {
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := l.wait(canceledCtx); err == nil {
			t.Fatalf("expected error on canceled context, got nil")
		}
	}

	// An active request should not be pushed 5 seconds into the future.
	// It should only have to wait for the remaining time of the 1-second interval since the first request.
	if time.Until(l.lastExecuted) > l.interval {
		t.Fatalf("lastExecuted is too far in the future (%v), debt accumulated", l.lastExecuted)
	}
}

func TestLimiter_ContextCancellationDuringWaitDoesNotAccumulateDebt(t *testing.T) {
	// 60 requests per minute = 1 second interval
	l := newLimiter(60)

	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("unexpected error on first wait: %v", err)
	}

	// Start a request that will wait on the timer, but cancel after 10ms
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := l.wait(timeoutCtx); err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// Verify lastExecuted was not advanced into the future by the canceled wait
	if time.Until(l.lastExecuted) > 0 {
		t.Fatalf("lastExecuted should not have been advanced into the future, got %v", l.lastExecuted)
	}
}

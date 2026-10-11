package notification

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Engine manages asynchronous dispatching of notification events across
// configured destinations.
type Engine struct {
	store   *Store
	client  *http.Client
	wg      sync.WaitGroup
	closeMu sync.RWMutex
	closed  bool
}

// NewEngine creates a new notification engine.
func NewEngine(store *Store, client *http.Client) *Engine {
	c := client
	if c == nil {
		c = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &Engine{
		store:  store,
		client: c,
	}
}

// Dispatch sends a SyncEvent asynchronously in a background goroutine
// to all active connections matching the event type, using a strict 10-second
// context timeout per delivery.
func (e *Engine) Dispatch(event SyncEvent) {
	e.closeMu.RLock()
	if e.closed {
		e.closeMu.RUnlock()
		return
	}
	e.wg.Add(1)
	e.closeMu.RUnlock()

	go func() {
		defer e.wg.Done()

		// Read connections from store with a short query timeout
		fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 5*time.Second)
		connections, err := e.store.List(fetchCtx)
		fetchCancel()
		if err != nil {
			log.Printf("notification engine: failed to load connections: %v", err)
			return
		}

		var sendWg sync.WaitGroup
		for _, conn := range connections {
			if !conn.Matches(event.Type) {
				continue
			}

			c := conn
			sendWg.Add(1)
			go func() {
				defer sendWg.Done()

				provider, provErr := NewProviderFromConnection(c, e.client)
				if provErr != nil {
					log.Printf("notification engine: failed to create provider for %s (%d): %v", c.Name, c.ID, provErr)
					return
				}

				sendCtx, sendCancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
				defer sendCancel()

				if sendErr := provider.Send(sendCtx, event); sendErr != nil {
					log.Printf("notification engine: failed to send %s notification to %s (%d): %v",
						event.Type, c.Name, c.ID, sendErr)
				}
			}()
		}
		sendWg.Wait()
	}()
}

// TestConnection verifies connectivity for a specific connection record.
func (e *Engine) TestConnection(ctx context.Context, conn Connection) error {
	provider, err := NewProviderFromConnection(conn, e.client)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	testCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()

	if testErr := provider.Test(testCtx); testErr != nil {
		return fmt.Errorf("connection test failed: %w", testErr)
	}

	return nil
}

// Wait blocks until all ongoing background dispatches have finished.
func (e *Engine) Wait() {
	e.wg.Wait()
}

// Close closes the engine and waits for pending background dispatches.
func (e *Engine) Close() {
	e.closeMu.Lock()
	e.closed = true
	e.closeMu.Unlock()

	e.wg.Wait()
}

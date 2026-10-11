package notification

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Dispatcher manages asynchronous dispatching of notification events across
// configured destinations.
type Dispatcher struct {
	store   *Store
	client  *http.Client
	wg      sync.WaitGroup
	closeMu sync.RWMutex
	closed  bool
}

// NewDispatcher creates a new notification dispatcher.
func NewDispatcher(store *Store, client *http.Client) *Dispatcher {
	c := client
	if c == nil {
		c = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &Dispatcher{
		store:  store,
		client: c,
	}
}

// Dispatch sends a SyncEvent asynchronously in a background goroutine
// to all active connections matching the event type, using a strict 10-second
// context timeout per delivery.
func (d *Dispatcher) Dispatch(event SyncEvent) {
	d.closeMu.RLock()
	if d.closed {
		d.closeMu.RUnlock()
		return
	}
	d.wg.Add(1)
	d.closeMu.RUnlock()

	go func() {
		defer d.wg.Done()

		// Read connections from store with a short query timeout
		fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 5*time.Second)
		connections, err := d.store.List(fetchCtx)
		fetchCancel()
		if err != nil {
			log.Printf("notification dispatcher: failed to load connections: %v", err)
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

				provider, provErr := NewProviderFromConnection(c, d.client)
				if provErr != nil {
					log.Printf("notification dispatcher: failed to create provider for %s (%d): %v", c.Name, c.ID, provErr)
					return
				}

				sendCtx, sendCancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
				defer sendCancel()

				if sendErr := provider.Send(sendCtx, event); sendErr != nil {
					log.Printf("notification dispatcher: failed to send %s notification to %s (%d): %v",
						event.Type, c.Name, c.ID, sendErr)
				}
			}()
		}
		sendWg.Wait()
	}()
}

// TestConnection verifies connectivity for a specific connection record.
func (d *Dispatcher) TestConnection(ctx context.Context, conn Connection) error {
	provider, err := NewProviderFromConnection(conn, d.client)
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
func (d *Dispatcher) Wait() {
	d.wg.Wait()
}

// Close closes the dispatcher and waits for pending background dispatches.
func (d *Dispatcher) Close() {
	d.closeMu.Lock()
	d.closed = true
	d.closeMu.Unlock()

	d.wg.Wait()
}

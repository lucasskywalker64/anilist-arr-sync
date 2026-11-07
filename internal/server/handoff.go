package server

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

// RestartHandoff serves only restart progress on the previous HTTPS address.
// The runtime owner starts it after releasing the application's listeners.
type RestartHandoff struct {
	mu         sync.RWMutex
	status     string
	instanceID string
	path       string
	target     *url.URL
	server     *http.Server
	tlsConfig  *tls.Config
	listener   net.Listener
	deadline   time.Time
	closed     sync.Once
	done       chan struct{}
}

func (s *Server) prepareRestartHandoff(next config.Config, target string) string {
	// A temporary TLS listener cannot occupy the next HTTP listener's port.
	if !s.boundEnableSsl || s.httpsServer == nil || next.Port == s.boundSslPort {
		return ""
	}
	s.workMu.Lock()
	defer s.workMu.Unlock()
	if s.handoff == nil {
		parsed, err := url.Parse(target)
		if err != nil {
			return ""
		}
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			return ""
		}
		h := &RestartHandoff{
			status: "starting", target: parsed, done: make(chan struct{}),
			path:      normalizeURLBase(s.boundURLBase) + "/api/v1/system/restart-status/" + hex.EncodeToString(id),
			tlsConfig: s.httpsServer.TLSConfig.Clone(),
			deadline:  time.Now().Add(time.Minute),
		}
		h.server = &http.Server{
			Addr:    net.JoinHostPort(s.boundBindAddress, strconv.Itoa(s.boundSslPort)),
			Handler: http.HandlerFunc(h.serveStatus), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second,
		}
		s.handoff = h
	}
	return s.handoff.path
}

// RestartHandoff returns the pending handoff for the runtime owner.
func (s *Server) RestartHandoff() *RestartHandoff {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	return s.handoff
}

// Start reuses the old certificate and expires the status listener after a minute.
func (h *RestartHandoff) Start(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !time.Now().Before(h.deadline) {
		return context.DeadlineExceeded
	}
	listener, err := tls.Listen("tcp", h.server.Addr, h.tlsConfig)
	if err != nil {
		return err
	}
	h.listener = listener
	go func() { _ = h.server.Serve(listener) }()
	go func() {
		timer := time.NewTimer(time.Until(h.deadline))
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
		case <-h.done:
		}
		h.Close()
	}()
	return nil
}

// Ready verifies that the replacement HTTP listener serves the new boot.
func (h *RestartHandoff) Ready(ctx context.Context, next *Server) error {
	ctx, cancel := context.WithDeadline(ctx, h.deadline)
	defer cancel()
	host := next.boundBindAddress
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	probeURL := "http://" + net.JoinHostPort(host, strconv.Itoa(next.boundPort)) + next.getCleanURLBase() + "/api/v1/health"
	client := &http.Client{Transport: &http.Transport{}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
		if err != nil {
			return err
		}
		req.Host = h.target.Host
		res, err := client.Do(req)
		if err == nil {
			var health struct {
				Status     string `json:"status"`
				InstanceID string `json:"instanceId"`
			}
			err = json.NewDecoder(res.Body).Decode(&health)
			_ = res.Body.Close()
			if err == nil && res.StatusCode == http.StatusOK && health.Status == "ok" && health.InstanceID == next.instanceID {
				h.mu.Lock()
				h.status, h.instanceID = "ready", health.InstanceID
				h.mu.Unlock()
				return nil
			}
		}
		select {
		case <-ctx.Done():
			h.Fail()
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Fail reports startup failure without exposing configuration or log contents.
func (h *RestartHandoff) Fail() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = "failed"
}

// Wait keeps a failure visible until the listener expires or shutdown is requested.
func (h *RestartHandoff) Wait(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-h.done:
	}
}

// Close releases the temporary listener, including any idle browser connections.
func (h *RestartHandoff) Close() {
	h.closed.Do(func() {
		_ = h.server.Close()
		if h.listener != nil {
			_ = h.listener.Close()
		}
		close(h.done)
	})
}

func (h *RestartHandoff) serveStatus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != h.path {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": h.status, "instanceId": h.instanceID})
}

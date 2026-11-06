package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// LifecycleAction tells the runtime owner to restart or stop this instance.
type LifecycleAction int

// Supported runtime lifecycle actions.
const (
	LifecycleNone LifecycleAction = iota
	LifecycleRestart
	LifecycleShutdown
)

// IsContainerized detects Docker, Podman, environment flags, and configured mode.
func IsContainerized(updateMechanism string) bool {
	if strings.EqualFold(updateMechanism, "Docker") || os.Getenv("IN_DOCKER") != "" || os.Getenv("CONTAINER") != "" {
		return true
	}
	for _, path := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// WaitLifecycle blocks until an API or runtime signal requests a lifecycle action.
func (s *Server) WaitLifecycle() LifecycleAction { return <-s.lifecycle }

// NotifyLifecycle coalesces concurrent requests without blocking HTTP handlers.
// Shutdown takes priority over a pending restart.
func (s *Server) NotifyLifecycle(action LifecycleAction) {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	if action == LifecycleNone {
		return
	}
	select {
	case pending := <-s.lifecycle:
		if pending == LifecycleShutdown {
			action = pending
		}
	default:
	}
	s.lifecycle <- action
}

// SetVersion supplies the runtime build version for health and status responses.
func (s *Server) SetVersion(version string) { s.version = version }

func (s *Server) handleSystemStatus(w http.ResponseWriter, _ *http.Request) {
	cfg := s.Config()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version": s.version, "instanceId": s.instanceID,
		"isContainerized": IsContainerized(cfg.UpdateMechanism),
		"os":              runtime.GOOS, "architecture": runtime.GOARCH,
	})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	port := cfg.Port
	if cfg.EnableSsl {
		port = cfg.SslPort
	}
	if cfg.Port != s.boundPort || cfg.SslPort != s.boundSslPort || cfg.EnableSsl != s.boundEnableSsl {
		hostname := r.URL.Hostname()
		if parsed, err := url.Parse("//" + r.Host); err == nil {
			hostname = parsed.Hostname()
		}
		host = net.JoinHostPort(hostname, strconv.Itoa(port))
		if cfg.EnableSsl {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	reconnectURL := scheme + "://" + host + normalizeURLBase(cfg.URLBase) + "/"
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"restarting": true, "instanceId": s.instanceID, "reconnectUrl": reconnectURL})
	s.NotifyLifecycle(LifecycleRestart)
}

func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	if IsContainerized(s.Config().UpdateMechanism) {
		s.writeJSONError(w, http.StatusBadRequest, "shutdown is disabled in containerized environments")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"shuttingDown": true})
	s.NotifyLifecycle(LifecycleShutdown)
}

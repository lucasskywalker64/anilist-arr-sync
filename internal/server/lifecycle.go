package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/updater"
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

// SetBuildCommit supplies the exact source identity used by health verification.
func (s *Server) SetBuildCommit(commit string) { s.commit = commit }

// InstanceID identifies this boot for update health verification.
func (s *Server) InstanceID() string { return s.instanceID }

// SetUpdater attaches the runtime-owned update supervisor before startup.
func (s *Server) SetUpdater(engine *updater.Engine) { s.updater = engine }

// AutomaticUpdate reads the current preference so channel changes never enable it.
func (s *Server) AutomaticUpdate(ctx context.Context) error {
	if s.updater == nil || !s.Config().UpdateAutomatically {
		return nil
	}
	status, err := s.updater.Status(ctx)
	if err != nil || !status.CanUpdate {
		return err
	}
	return s.updater.Apply(ctx)
}

// SetListenerOverrides applies transient CLI settings before Start is called.
func (s *Server) SetListenerOverrides(port *int, bindAddress *string) {
	s.portOverride, s.bindOverride = port, bindAddress
}

func (s *Server) listenerConfig(cfg config.Config) config.Config {
	if s.portOverride != nil {
		cfg.Port = *s.portOverride
	}
	if s.bindOverride != nil {
		cfg.BindAddress = *s.bindOverride
	}
	return cfg
}

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
	cfg := s.listenerConfig(s.Config())
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	forwardedProto := r.Header.Get("X-Forwarded-Proto")
	proxied := (forwardedProto == "http" || forwardedProto == "https") &&
		ipInNets(ResolveClientIP(r.RemoteAddr, "", nil), s.getTrustedNetworks())
	if proxied {
		scheme = forwardedProto
	}
	host := r.Host
	port := cfg.Port
	if cfg.EnableSsl {
		port = cfg.SslPort
	}
	if !proxied && (cfg.Port != s.boundPort || cfg.SslPort != s.boundSslPort || cfg.EnableSsl != s.boundEnableSsl || cfg.BindAddress != s.boundBindAddress) {
		hostname := r.URL.Hostname()
		if parsed, err := url.Parse("//" + r.Host); err == nil {
			hostname = parsed.Hostname()
		}
		if cfg.BindAddress != s.boundBindAddress && cfg.BindAddress != "" && cfg.BindAddress != "0.0.0.0" && cfg.BindAddress != "::" {
			hostname = cfg.BindAddress
		}
		host = net.JoinHostPort(hostname, strconv.Itoa(port))
		if cfg.EnableSsl {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	reconnectURL := scheme + "://" + host + normalizeURLBase(cfg.URLBase) + "/"
	statusURL := ""
	if !proxied && r.TLS != nil && scheme == "http" {
		statusURL = s.prepareRestartHandoff(cfg, reconnectURL)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"restarting": true, "instanceId": s.instanceID, "reconnectUrl": reconnectURL, "statusUrl": statusURL})
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

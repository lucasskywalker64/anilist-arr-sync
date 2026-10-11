// Package server provides the embedded HTTP web listener, REST API endpoints,
// session management, and authentication handling.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

// Server coordinates the HTTP/HTTPS listeners and REST API routes.
type Server struct {
	cfgMu        sync.RWMutex
	cfg          *config.Config
	configPath   string
	db           *storage.DB
	orch         *orchestrator.Orchestrator
	backup       *backup.Engine
	notifier     *notification.Engine
	notifStore   *notification.Store
	sessions     *SessionStore
	cleanURLBase string
	errChan      chan error

	httpServer  *http.Server
	httpsServer *http.Server

	syncMu         sync.Mutex
	isSyncing      bool
	lastSyncReport *orchestrator.SyncReport
}

// NewServer creates a new Server instance.
func NewServer(
	cfg *config.Config,
	configPath string,
	db *storage.DB,
	orch *orchestrator.Orchestrator,
	backupEngine *backup.Engine,
) *Server {
	cleanBase := normalizeURLBase(cfg.URLBase)

	var notifStore *notification.Store
	var notifEngine *notification.Engine
	if db != nil {
		notifStore = notification.NewStore(db)
		notifEngine = notification.NewEngine(notifStore, nil)
	}

	if orch != nil && notifEngine != nil {
		orch.SetNotifier(notifEngine)
	}

	return &Server{
		cfg:          cfg,
		configPath:   configPath,
		db:           db,
		orch:         orch,
		backup:       backupEngine,
		notifier:     notifEngine,
		notifStore:   notifStore,
		sessions:     NewSessionStore(24 * time.Hour),
		cleanURLBase: cleanBase,
		errChan:      make(chan error, 2),
	}
}

// SetNotifier sets a custom notification engine on the server.
func (s *Server) SetNotifier(n *notification.Engine) {
	s.notifier = n
	if s.orch != nil {
		s.orch.SetNotifier(n)
	}
}

// Notifier returns the configured notification engine.
func (s *Server) Notifier() *notification.Engine {
	return s.notifier
}

// Errors returns a receive-only channel for background listener errors.
func (s *Server) Errors() <-chan error {
	return s.errChan
}

// Config returns a copy of current configuration under a read lock.
func (s *Server) Config() config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return *s.cfg
}

func (s *Server) getAPIKey() string {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.APIKey
}

func (s *Server) getAuthMethod() string {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.AuthenticationMethod
}

func (s *Server) getAuthRequired() string {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.AuthenticationRequired
}

func (s *Server) isSslEnabled() bool {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.EnableSsl
}

// normalizeURLBase strips trailing slashes and ensures a leading slash unless empty.
func normalizeURLBase(base string) string {
	b := strings.TrimSpace(base)
	if b == "" || b == "/" {
		return ""
	}
	if !strings.HasPrefix(b, "/") {
		b = "/" + b
	}
	return strings.TrimSuffix(b, "/")
}

// Handler constructs and returns the HTTP handler with all routes and middleware attached.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	var h http.Handler = mux

	// Subpath prefix routing if cleanURLBase is specified
	if s.cleanURLBase != "" {
		subMux := http.NewServeMux()
		subMux.Handle(s.cleanURLBase+"/", http.StripPrefix(s.cleanURLBase, mux))
		subMux.HandleFunc(s.cleanURLBase, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, s.cleanURLBase+"/", http.StatusMovedPermanently)
		})
		h = subMux
	}

	return s.authMiddleware(s.csrfMiddleware(h))
}

// Start begins listening on configured HTTP and optional HTTPS ports.
func (s *Server) Start() error {
	handler := s.Handler()

	httpAddr := fmt.Sprintf("%s:%d", s.cfg.BindAddress, s.cfg.Port)
	s.httpServer = &http.Server{
		Addr:              httpAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	httpListener, err := net.Listen("tcp", httpAddr)
	if err != nil {
		return fmt.Errorf("server: failed to bind HTTP listener on %s: %w", httpAddr, err)
	}

	go func() {
		if serveErr := s.httpServer.Serve(httpListener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			select {
			case s.errChan <- fmt.Errorf("http serve error: %w", serveErr):
			default:
			}
		}
	}()

	if s.isSslEnabled() {
		httpsAddr := fmt.Sprintf("%s:%d", s.cfg.BindAddress, s.cfg.SslPort)
		cert, certErr := tls.LoadX509KeyPair(s.cfg.SslCertPath, s.cfg.SslKeyPath)
		if certErr != nil {
			_ = s.httpServer.Close()
			return fmt.Errorf("server: failed to load SSL certificates: %w", certErr)
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}

		s.httpsServer = &http.Server{
			Addr:              httpsAddr,
			Handler:           handler,
			TLSConfig:         tlsConfig,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       120 * time.Second,
		}

		httpsListener, listenErr := tls.Listen("tcp", httpsAddr, tlsConfig)
		if listenErr != nil {
			_ = s.httpServer.Close()
			return fmt.Errorf("server: failed to bind HTTPS listener on %s: %w", httpsAddr, listenErr)
		}

		go func() {
			if serveErr := s.httpsServer.Serve(httpsListener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				select {
				case s.errChan <- fmt.Errorf("https serve error: %w", serveErr):
				default:
				}
			}
		}()
	}

	return nil
}

// Shutdown gracefully terminates HTTP and HTTPS listeners.
func (s *Server) Shutdown(ctx context.Context) error {
	var errs []error
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, fmt.Errorf("http shutdown: %w", err))
		}
	}
	if s.httpsServer != nil {
		if err := s.httpsServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, fmt.Errorf("https shutdown: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("POST /api/v1/auth/setup", s.handleSetup)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/user", s.handleCurrentUser)
	mux.HandleFunc("GET /api/v1/review", s.handleListReview)
	mux.HandleFunc("POST /api/v1/review/{id}/resolve", s.handleResolveReview)
	mux.HandleFunc("POST /api/v1/review/{id}/ignore", s.handleIgnoreReview)

	mux.HandleFunc("GET /api/v1/overrides", s.handleListOverrides)
	mux.HandleFunc("POST /api/v1/overrides", s.handleCreateOverride)
	mux.HandleFunc("DELETE /api/v1/overrides/{id}", s.handleDeleteOverride)

	mux.HandleFunc("GET /api/v1/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/v1/config", s.handleUpdateConfig)

	mux.HandleFunc("POST /api/v1/sync/start", s.handleStartSync)
	mux.HandleFunc("GET /api/v1/sync/status", s.handleGetSyncStatus)
	mux.HandleFunc("GET /api/v1/sync/history", s.handleGetSyncHistory)

	mux.HandleFunc("GET /api/v1/staged", s.handleListStaged)
	mux.HandleFunc("POST /api/v1/staged/apply", s.handleApplyStaged)
	mux.HandleFunc("POST /api/v1/staged/reject", s.handleRejectStaged)

	mux.HandleFunc("GET /api/v1/backups", s.handleListBackups)
	mux.HandleFunc("POST /api/v1/backups", s.handleCreateBackup)
	mux.HandleFunc("POST /api/v1/backups/restore", s.handleRestoreBackup)

	mux.HandleFunc("GET /api/v1/update/status", s.handleGetUpdateStatus)
	mux.HandleFunc("POST /api/v1/update/apply", s.handleApplyUpdate)

	mux.HandleFunc("GET /api/v1/notifications", s.handleListNotifications)
	mux.HandleFunc("POST /api/v1/notifications", s.handleCreateNotification)
	mux.HandleFunc("POST /api/v1/notifications/test", s.handleTestNotificationPayload)
	mux.HandleFunc("POST /api/v1/notifications/{id}/test", s.handleTestNotificationConnection)
	mux.HandleFunc("PUT /api/v1/notifications/{id}", s.handleUpdateNotification)
	mux.HandleFunc("DELETE /api/v1/notifications/{id}", s.handleDeleteNotification)
}

// handleHealth responds with service health status and version.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": "1.0.0",
	})
}

// Package server provides the embedded HTTP web listener, REST API endpoints,
// session management, and authentication handling.
package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/scheduler"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
	"github.com/lucasskywalker64/anilist-arr-sync/web"
)

// Server coordinates the HTTP/HTTPS listeners and REST API routes.
type Server struct {
	cfgMu        sync.RWMutex
	cfg          *config.Config
	configPath   string
	db           *storage.DB
	orch         *orchestrator.Orchestrator
	backup       *backup.Engine
	scheduler    *scheduler.Scheduler
	notifier     *notification.Dispatcher
	notifStore   *notification.Store
	sessions     *SessionStore
	trustedNets  []*net.IPNet
	cleanURLBase string
	webFS        fs.FS
	errChan      chan error

	httpServer  *http.Server
	httpsServer *http.Server

	boundBindAddress string
	boundPort        int
	boundURLBase     string
	boundEnableSsl   bool
	boundSslPort     int

	syncMu         sync.Mutex
	lastSyncReport *orchestrator.SyncReport
}

// SetScheduler connects configuration changes to the background scheduler.
// The runtime bootstrap owns the scheduler's Start and Stop lifecycle.
func (s *Server) SetScheduler(scheduled *scheduler.Scheduler) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	s.scheduler = scheduled
	if scheduled != nil {
		scheduled.UpdateConfig(*s.cfg)
	}
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
	var notifDispatcher *notification.Dispatcher
	if db != nil {
		notifStore = notification.NewStore(db)
		notifDispatcher = notification.NewDispatcher(notifStore, nil)
	}

	if orch != nil && notifDispatcher != nil {
		orch.SetNotifier(notifDispatcher)
	}

	var defaultWebFS fs.FS
	if dist, err := web.Dist(); err == nil {
		defaultWebFS = dist
	}

	var trustedNets []*net.IPNet
	var bindAddr, urlBase string
	var port, sslPort int
	var enableSsl bool
	if cfg != nil {
		trustedNets, _ = ParseCIDRs(cfg.TrustedNetworks)
		bindAddr = cfg.BindAddress
		port = cfg.Port
		urlBase = cfg.URLBase
		enableSsl = cfg.EnableSsl
		sslPort = cfg.SslPort
	}

	return &Server{
		cfg:              cfg,
		configPath:       configPath,
		db:               db,
		orch:             orch,
		backup:           backupEngine,
		notifier:         notifDispatcher,
		notifStore:       notifStore,
		sessions:         NewSessionStore(24 * time.Hour),
		trustedNets:      trustedNets,
		cleanURLBase:     cleanBase,
		webFS:            defaultWebFS,
		errChan:          make(chan error, 2),
		boundBindAddress: bindAddr,
		boundPort:        port,
		boundURLBase:     urlBase,
		boundEnableSsl:   enableSsl,
		boundSslPort:     sslPort,
	}
}

// SetWebFS sets a custom filesystem for serving static assets and the web SPA.
func (s *Server) SetWebFS(f fs.FS) {
	s.webFS = f
}

// SetNotifier sets a custom notification dispatcher on the server.
func (s *Server) SetNotifier(n *notification.Dispatcher) {
	s.notifier = n
	if s.orch != nil {
		if n == nil {
			s.orch.SetNotifier(nil)
		} else {
			s.orch.SetNotifier(n)
		}
	}
}

// Notifier returns the configured notification dispatcher.
func (s *Server) Notifier() *notification.Dispatcher {
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

func (s *Server) getCleanURLBase() string {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cleanURLBase
}

func (s *Server) isSetupRequired(ctx context.Context) bool {
	if s.db == nil {
		return false
	}
	var count int
	err := s.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM users;").Scan(&count)
	})
	if err != nil {
		return false
	}
	return count == 0
}

func (s *Server) isRequestAuthorized(r *http.Request) bool {
	// 1. Valid X-Api-Key header matching s.getAPIKey()
	reqKey := r.Header.Get("X-Api-Key")
	apiKey := s.getAPIKey()
	if reqKey != "" && apiKey != "" && subtle.ConstantTimeCompare([]byte(reqKey), []byte(apiKey)) == 1 {
		return true
	}

	// 2. Auth required == "Disabled"
	if s.getAuthRequired() == "Disabled" {
		return true
	}

	// 3. Auth required == "DisabledForLocalAddresses" and IsLocalIP(s.getClientIP(r))
	if s.getAuthRequired() == "DisabledForLocalAddresses" {
		clientIP := s.getClientIP(r)
		if clientIP != nil && IsLocalIP(clientIP) {
			return true
		}
	}

	// 4. Auth method == "Forms" and valid session cookie in s.sessions
	if s.getAuthMethod() == "Forms" && s.sessions != nil {
		if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
			if _, ok := s.sessions.Validate(cookie.Value); ok {
				return true
			}
		}
	}

	// 5. Auth method == "Basic" and valid basic auth credentials in s.db
	if s.getAuthMethod() == "Basic" && s.db != nil {
		user, pass, ok := r.BasicAuth()
		if ok {
			var storedHash string
			err := s.db.Read(r.Context(), func(ctx context.Context, q storage.Querier) error {
				return q.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username = ?;", user).Scan(&storedHash)
			})
			if err == nil && CheckPassword(pass, storedHash) {
				return true
			}
		}
	}

	return false
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

func (s *Server) getTrustedNetworks() []*net.IPNet {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.trustedNets
}

func (s *Server) getAllowedHosts() string {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.AllowedHosts
}

func (s *Server) getClientIP(r *http.Request) net.IP {
	return ResolveClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), s.getTrustedNetworks())
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

	return s.allowedHostsMiddleware(s.authMiddleware(s.csrfMiddleware(h)))
}

// Start begins listening on configured HTTP and optional HTTPS ports.
func (s *Server) Start() error {
	s.cfgMu.Lock()
	s.boundBindAddress = s.cfg.BindAddress
	s.boundPort = s.cfg.Port
	s.boundURLBase = s.cfg.URLBase
	s.boundEnableSsl = s.cfg.EnableSsl
	s.boundSslPort = s.cfg.SslPort
	s.cfgMu.Unlock()

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

	mux.HandleFunc("GET /api/v1/library", s.handleListLibrary)
	mux.HandleFunc("GET /api/v1/library/stats", s.handleGetLibraryStats)

	mux.HandleFunc("GET /api/v1/ignored", s.handleListIgnored)
	mux.HandleFunc("DELETE /api/v1/ignored/{id}", s.handleDeleteIgnored)

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
	mux.HandleFunc("GET /api/v1/filesystem", s.handleFilesystem)

	mux.HandleFunc("GET /api/v1/update/status", s.handleGetUpdateStatus)
	mux.HandleFunc("POST /api/v1/update/apply", s.handleApplyUpdate)

	mux.HandleFunc("GET /api/v1/notifications", s.handleListNotifications)
	mux.HandleFunc("POST /api/v1/notifications", s.handleCreateNotification)
	mux.HandleFunc("POST /api/v1/notifications/test", s.handleTestNotificationPayload)
	mux.HandleFunc("POST /api/v1/notifications/{id}/test", s.handleTestNotificationConnection)
	mux.HandleFunc("PUT /api/v1/notifications/{id}", s.handleUpdateNotification)
	mux.HandleFunc("DELETE /api/v1/notifications/{id}", s.handleDeleteNotification)

	// Web SPA and static assets
	mux.HandleFunc("/", s.handleSPA)
}

// handleSPA serves static frontend assets and provides SPA routing fallback to index.html.
func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}

	if s.webFS == nil {
		http.NotFound(w, r)
		return
	}

	cleanPath := strings.TrimPrefix(r.URL.Path, "/")
	if cleanPath == "" || cleanPath == "index.html" {
		s.serveIndexHTML(w, r)
		return
	}

	// Try serving static file if it exists and is not a directory
	lookupPath := cleanPath
	if strings.Contains(cleanPath, "assets/") {
		lookupPath = cleanPath[strings.Index(cleanPath, "assets/"):]
	}

	f, err := s.webFS.Open(lookupPath)
	if err == nil {
		stat, statErr := f.Stat()
		if statErr == nil && !stat.IsDir() {
			defer func() { _ = f.Close() }()
			if strings.HasPrefix(lookupPath, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			seeker, ok := f.(io.ReadSeeker)
			if ok {
				http.ServeContent(w, r, stat.Name(), stat.ModTime(), seeker)
				return
			}
			content, readErr := io.ReadAll(f)
			if readErr == nil {
				http.ServeContent(w, r, stat.Name(), stat.ModTime(), bytes.NewReader(content))
				return
			}
		} else {
			_ = f.Close()
		}
	}

	// If this was an asset request (e.g. inside assets/ or has a static extension), return 404 rather than index.html
	if strings.Contains(cleanPath, "assets/") || hasStaticFileExtension(cleanPath) {
		http.NotFound(w, r)
		return
	}

	// SPA fallback: serve index.html for client routes
	s.serveIndexHTML(w, r)
}

func hasStaticFileExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".map", ".json":
		return true
	default:
		return false
	}
}

func (s *Server) serveIndexHTML(w http.ResponseWriter, r *http.Request) {
	indexFile, err := s.webFS.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = indexFile.Close() }()

	indexStat, err := indexFile.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	content, err := io.ReadAll(indexFile)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Inject runtime bootstrap config and document base href
	baseHref := "/"
	cleanBase := s.getCleanURLBase()
	if cleanBase != "" {
		baseHref = cleanBase + "/"
	}
	baseTag := fmt.Sprintf("<base href=\"%s\" />", baseHref)

	apiKeyVal := ""
	if s.isRequestAuthorized(r) {
		apiKeyVal = s.getAPIKey()
	}

	bootstrapData := map[string]any{
		"urlBase": cleanBase,
		"version": "1.0.0",
		"apiKey":  apiKeyVal,
	}
	bootstrapJSON, err := json.Marshal(bootstrapData)
	if err != nil {
		bootstrapJSON = []byte(`{"urlBase":"","version":"1.0.0","apiKey":""}`)
	}
	bootstrapScript := fmt.Sprintf("%s\n    <script id=\"arr-bootstrap\">window.AniListArrSync = %s;</script>", baseTag, bootstrapJSON)
	if bytes.Contains(content, []byte("<!-- ANILIST_ARR_SYNC_CONFIG -->")) {
		content = bytes.Replace(content, []byte("<!-- ANILIST_ARR_SYNC_CONFIG -->"), []byte(bootstrapScript), 1)
	} else if bytes.Contains(content, []byte("</head>")) {
		content = bytes.Replace(content, []byte("</head>"), []byte(bootstrapScript+"</head>"), 1)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "index.html", indexStat.ModTime(), bytes.NewReader(content))
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

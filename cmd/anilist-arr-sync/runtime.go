package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/fribb"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/scheduler"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/server"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

type appRuntime struct {
	cfg       *config.Config
	db        *storage.DB
	server    *server.Server
	orch      *orchestrator.Orchestrator
	scheduler *scheduler.Scheduler
	loader    *fribb.Loader
	client    *http.Client
	ctx       context.Context
	cancel    context.CancelFunc
	workers   sync.WaitGroup
}

func bootstrap(ctx context.Context, o options, paths runtimePaths) (*appRuntime, error) {
	cfg, err := loadConfig(o, paths)
	if err != nil {
		return nil, err
	}
	backupDir := dataPath(paths.data, cfg.BackupFolder)
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return nil, err
	}
	client, err := outboundClient(*cfg)
	if err != nil {
		return nil, err
	}
	db, err := storage.Open(dataPath(paths.data, cfg.DatabasePath)) // Open runs migrations on every boot.
	if err != nil {
		client.CloseIdleConnections()
		return nil, err
	}
	fail := func(err error) (*appRuntime, error) {
		client.CloseIdleConnections()
		return nil, errors.Join(err, db.Close())
	}
	sonarrClient, radarrClient, err := serviceClients(cfg, client)
	if err != nil {
		return fail(err)
	}
	al := anilist.NewClient("", anilist.WithHTTPClient(client))
	orch := orchestrator.New(db, cfg, al, sonarrClient, radarrClient)
	backups := backup.NewEngine(db, paths.config, backupDir, backup.WithRetention(cfg.BackupRetention))
	srv := server.NewServer(cfg, paths.config, db, orch, backups)
	srv.SetVersion(version)
	srv.SetNotifier(notification.NewDispatcher(notification.NewStore(db), client))
	scheduledOrch := orch
	if !syncConfigured(cfg) {
		log.Print("scheduled sync idle: configure AniList and a Servarr URL/API key, then restart")
		scheduledOrch = nil
	}
	scheduled := scheduler.New(*cfg, scheduledOrch, backups, nil)
	srv.SetScheduler(scheduled)
	ctx, cancel := context.WithCancel(ctx)
	return &appRuntime{cfg: cfg, db: db, server: srv, orch: orch, scheduler: scheduled,
		loader: fribb.NewLoader(db, fribb.WithHTTPClient(client)), client: client, ctx: ctx, cancel: cancel}, nil
}

func serviceClients(cfg *config.Config, client *http.Client) (*servarr.SonarrClient, *servarr.RadarrClient, error) {
	var sonarr *servarr.SonarrClient
	var radarr *servarr.RadarrClient
	var err error
	if cfg.SonarrURL != "" {
		sonarr, err = servarr.NewSonarrClient(cfg.SonarrURL, cfg.SonarrAPIKey, servarr.WithHTTPClient(client))
		if err != nil {
			return nil, nil, err
		}
	}
	if cfg.RadarrURL != "" {
		radarr, err = servarr.NewRadarrClient(cfg.RadarrURL, cfg.RadarrAPIKey, servarr.WithHTTPClient(client))
		if err != nil {
			return nil, nil, err
		}
	}
	return sonarr, radarr, nil
}

func syncConfigured(cfg *config.Config) bool {
	return strings.TrimSpace(cfg.AniListUsername) != "" &&
		((cfg.SonarrURL != "" && cfg.SonarrAPIKey != "") || (cfg.RadarrURL != "" && cfg.RadarrAPIKey != ""))
}

func (r *appRuntime) refreshMappings(ctx context.Context) error {
	if _, err := r.loader.Sync(ctx); err != nil {
		meta, cachedErr := r.loader.GetMeta(ctx)
		if cachedErr != nil || meta.EntryCount == 0 {
			return err
		}
		log.Printf("Fribb refresh failed, using cached mappings: %v", err)
	}
	return nil
}

func (r *appRuntime) syncOnce(ctx context.Context, out io.Writer) error {
	if !syncConfigured(r.cfg) {
		return errors.New("sync requires an AniList username and a Servarr URL/API key")
	}
	if err := r.refreshMappings(ctx); err != nil {
		return fmt.Errorf("fribb mappings: %w", err)
	}
	report, err := r.orch.Sync(ctx, orchestrator.TriggerCLI)
	if report != nil {
		_, writeErr := fmt.Fprintf(out, "%s: scanned=%d addedRadarr=%d monitoredSonarr=%d queuedReview=%d errors=%d\n",
			report.Status, report.ItemsScanned, report.AddedRadarr, report.MonitoredSonarr, report.QueuedReview, len(report.Errors))
		err = errors.Join(err, writeErr)
		if report.Status != orchestrator.SyncStatusSuccess {
			err = errors.Join(err, fmt.Errorf("sync %s: %s", report.Status, strings.Join(report.Errors, "; ")))
		}
	}
	return err
}

func (r *appRuntime) start() {
	r.scheduler.Start(r.ctx)
	r.workers.Add(1)
	go func() {
		defer r.workers.Done()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			if err := r.refreshMappings(r.ctx); err != nil && r.ctx.Err() == nil {
				log.Printf("Fribb: %v", err)
			}
			select {
			case <-r.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (r *appRuntime) close() error {
	r.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		r.scheduler.Stop()
		if err := r.server.Shutdown(ctx); err != nil {
			done <- err
			return
		}
		r.workers.Wait()
		r.server.Notifier().Close()
		r.client.CloseIdleConnections()
		done <- errors.Join(r.db.Drain(ctx), r.db.Close())
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("shutdown exceeded 15 seconds: %w", ctx.Err())
	}
}

func (r *appRuntime) browserURL() string {
	host := r.cfg.BindAddress
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	scheme, port := "http", r.cfg.Port
	if r.cfg.EnableSsl {
		scheme, port = "https", r.cfg.SslPort
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/" + strings.Trim(r.cfg.URLBase, "/")
}

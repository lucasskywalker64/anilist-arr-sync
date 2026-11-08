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
	cfg         *config.Config
	listenerCfg config.Config
	db          *storage.DB
	server      *server.Server
	orch        *orchestrator.Orchestrator
	scheduler   *scheduler.Scheduler
	loader      *fribb.Loader
	client      *http.Client
	ctx         context.Context
	cancel      context.CancelFunc
	workers     sync.WaitGroup
}

func bootstrap(ctx context.Context, o options, paths runtimePaths) (*appRuntime, error) {
	cfg, err := loadConfig(paths)
	if err != nil {
		return nil, err
	}
	listenerCfg := *cfg
	var portOverride *int
	var bindOverride *string
	if o.portSet {
		portOverride = &o.port
		listenerCfg.Port = o.port
	}
	if o.bindSet {
		bindOverride = &o.bindAddress
		listenerCfg.BindAddress = o.bindAddress
	}
	if err := listenerCfg.Validate(); err != nil {
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
	sonarrClient, radarrClient, err := servarr.NewClients(cfg.SonarrURL, cfg.SonarrAPIKey, cfg.RadarrURL, cfg.RadarrAPIKey, servarr.WithHTTPClient(client))
	if err != nil {
		return fail(err)
	}
	al := anilist.NewClient("", anilist.WithHTTPClient(client))
	orch := orchestrator.New(db, cfg, al, sonarrClient, radarrClient)
	backups := backup.NewEngine(db, paths.config, backupDir, backup.WithRetention(cfg.BackupRetention))
	srv := server.NewServer(cfg, paths.config, db, orch, backups)
	srv.SetBackupSettingsPreparation(func(current, next config.Config) (func(), error) {
		dir := dataPath(paths.data, next.BackupFolder)
		if next.BackupFolder != current.BackupFolder {
			if err := writableDirectory(dir); err != nil {
				return nil, err
			}
		}
		return func() { backups.UpdateSettings(dir, next.BackupRetention) }, nil
	})
	srv.SetOutboundHTTPClient(client)
	srv.SetListenerOverrides(portOverride, bindOverride)
	srv.SetVersion(version)
	srv.SetNotifier(notification.NewDispatcher(notification.NewStore(db), client))
	if !cfg.SyncConfigured() {
		log.Print("scheduled sync idle: configure AniList and a Servarr URL/API key")
	}
	scheduled := scheduler.New(*cfg, orch, backups, nil)
	srv.SetScheduler(scheduled)
	ctx, cancel := context.WithCancel(ctx)
	r := &appRuntime{cfg: cfg, listenerCfg: listenerCfg, db: db, server: srv, orch: orch, scheduler: scheduled,
		loader: fribb.NewLoader(db, fribb.WithHTTPClient(client)), client: client, ctx: ctx, cancel: cancel}
	scheduled.SetSyncPreparation(r.ensureSyncMappings)
	srv.SetSyncPreparation(r.ensureSyncMappings)
	return r, nil
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

func (r *appRuntime) ensureSyncMappings(ctx context.Context) error {
	for {
		meta, err := r.loader.GetMeta(ctx)
		if err == nil && meta.EntryCount > 0 {
			return nil
		}
		if err := r.refreshMappings(ctx); !errors.Is(err, fribb.ErrSyncInProgress) {
			return err
		}
		// The startup refresh is already loading mappings. Wait for it to publish them.
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *appRuntime) syncOnce(ctx context.Context, out io.Writer) error {
	if !r.cfg.SyncConfigured() {
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return r.shutdown(ctx)
}

func (r *appRuntime) shutdown(ctx context.Context) error {
	r.cancel()
	shutdownErr := r.server.Shutdown(ctx)
	r.scheduler.Stop()
	r.workers.Wait()
	notificationErr := r.server.Notifier().Close(ctx)
	r.client.CloseIdleConnections()
	return errors.Join(shutdownErr, notificationErr, r.db.Drain(ctx), r.db.Close())
}

func (r *appRuntime) browserURL() string {
	host := r.listenerCfg.BindAddress
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	scheme, port := "http", r.listenerCfg.Port
	if r.listenerCfg.EnableSsl {
		scheme, port = "https", r.listenerCfg.SslPort
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/" + strings.Trim(r.listenerCfg.URLBase, "/")
}

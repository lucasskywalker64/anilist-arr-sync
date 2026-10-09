// Package backup provides a unified engine for database snapshots, config archiving,
// rolling retention pruning, integrity verification, and connection-drained restore.
package backup

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

// Engine coordinates database snapshots, configuration archiving, retention pruning,
// and safe restore workflows.
type Engine struct {
	db         *storage.DB
	configPath string
	backupDir  string
	retention  int
	nowFn      func() time.Time
}

// Option configures optional parameters on Engine.
type Option func(*Engine)

// WithRetention sets the rolling retention count for backups.
func WithRetention(retention int) Option {
	return func(e *Engine) {
		e.retention = retention
	}
}

// WithNowFunc overrides the clock for deterministic timestamps in tests.
func WithNowFunc(fn func() time.Time) Option {
	return func(e *Engine) {
		e.nowFn = fn
	}
}

// NewEngine creates a new backup and restore Engine instance.
func NewEngine(db *storage.DB, configPath, backupDir string, opts ...Option) *Engine {
	e := &Engine{
		db:         db,
		configPath: configPath,
		backupDir:  backupDir,
		retention:  DefaultRetention,
		nowFn:      time.Now,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Create generates a timestamped zip archive containing an online SQLite snapshot
// and config.xml, then automatically prunes older backups according to the retention window.
func (e *Engine) Create(ctx context.Context, backupType Type) (*Info, error) {
	if backupType == "" {
		backupType = TypeManual
	}

	if err := os.MkdirAll(e.backupDir, 0755); err != nil {
		return nil, fmt.Errorf("backup: failed to create backup directory: %w", err)
	}

	now := e.nowFn()
	archiveName := FormatBackupFilename(backupType, now)
	archivePath := filepath.Join(e.backupDir, archiveName)

	stagingDir, err := os.MkdirTemp("", "anilist-backup-snapshot-*")
	if err != nil {
		return nil, fmt.Errorf("backup: failed to create staging directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(stagingDir)
	}()

	stagedDBPath := filepath.Join(stagingDir, "sync.db")
	if err := e.db.VacuumInto(ctx, stagedDBPath); err != nil {
		return nil, fmt.Errorf("backup: failed to snapshot database via VACUUM INTO: %w", err)
	}

	tempZipPath := filepath.Join(e.backupDir, fmt.Sprintf(".%s.tmp", archiveName))
	zipFile, err := os.OpenFile(tempZipPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("backup: failed to create temporary zip archive: %w", err)
	}

	zw := zip.NewWriter(zipFile)

	// 1. Add sync.db to zip archive
	dbIn, err := os.Open(stagedDBPath)
	if err != nil {
		_ = zw.Close()
		_ = zipFile.Close()
		_ = os.Remove(tempZipPath)
		return nil, fmt.Errorf("backup: failed to read snapshot database: %w", err)
	}

	dbEntry, err := zw.Create("sync.db")
	if err != nil {
		_ = dbIn.Close()
		_ = zw.Close()
		_ = zipFile.Close()
		_ = os.Remove(tempZipPath)
		return nil, fmt.Errorf("backup: failed to create sync.db zip entry: %w", err)
	}

	if _, err := io.Copy(dbEntry, dbIn); err != nil {
		_ = dbIn.Close()
		_ = zw.Close()
		_ = zipFile.Close()
		_ = os.Remove(tempZipPath)
		return nil, fmt.Errorf("backup: failed to write sync.db into zip archive: %w", err)
	}
	_ = dbIn.Close()

	// 2. Add config.xml to zip archive if available
	if e.configPath != "" {
		if cfgData, err := os.ReadFile(e.configPath); err == nil {
			cfgEntry, err := zw.Create("config.xml")
			if err != nil {
				_ = zw.Close()
				_ = zipFile.Close()
				_ = os.Remove(tempZipPath)
				return nil, fmt.Errorf("backup: failed to create config.xml zip entry: %w", err)
			}
			if _, err := cfgEntry.Write(cfgData); err != nil {
				_ = zw.Close()
				_ = zipFile.Close()
				_ = os.Remove(tempZipPath)
				return nil, fmt.Errorf("backup: failed to write config.xml into zip archive: %w", err)
			}
		}
	}

	if err := zw.Close(); err != nil {
		_ = zipFile.Close()
		_ = os.Remove(tempZipPath)
		return nil, fmt.Errorf("backup: failed to finalize zip archive: %w", err)
	}
	if err := zipFile.Close(); err != nil {
		_ = os.Remove(tempZipPath)
		return nil, fmt.Errorf("backup: failed to close zip file: %w", err)
	}

	// Move temporary zip archive to final path
	_ = os.Remove(archivePath)
	if err := os.Rename(tempZipPath, archivePath); err != nil {
		// Fallback to copy if rename fails
		if errCopy := copyFile(tempZipPath, archivePath); errCopy != nil {
			_ = os.Remove(tempZipPath)
			return nil, fmt.Errorf("backup: failed to place backup archive at %s: %w", archivePath, errCopy)
		}
		_ = os.Remove(tempZipPath)
	}

	stat, err := os.Stat(archivePath)
	var sizeBytes int64
	if err == nil {
		sizeBytes = stat.Size()
	}

	// Automatically prune old archives according to rolling retention window
	_, _ = e.Prune(ctx)

	return &Info{
		Filename:  archiveName,
		Path:      archivePath,
		Type:      backupType,
		CreatedAt: now.UTC(),
		SizeBytes: sizeBytes,
	}, nil
}

// List returns all valid backup archives currently stored in the backup directory,
// sorted chronologically with newest first.
func (e *Engine) List() ([]Info, error) {
	if _, err := os.Stat(e.backupDir); os.IsNotExist(err) {
		return []Info{}, nil
	}

	entries, err := os.ReadDir(e.backupDir)
	if err != nil {
		return nil, fmt.Errorf("backup: failed to read backup directory: %w", err)
	}

	var results []Info
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, Extension) {
			continue
		}

		fullPath := filepath.Join(e.backupDir, name)
		info, err := ParseBackupFilename(fullPath)
		if err != nil {
			continue
		}

		fi, err := entry.Info()
		if err == nil {
			info.SizeBytes = fi.Size()
		}

		results = append(results, *info)
	}

	// Sort descending by CreatedAt (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	return results, nil
}

// Prune deletes backup archives that exceed the rolling retention limit,
// returning the list of deleted archive file paths.
func (e *Engine) Prune(_ context.Context) ([]string, error) {
	backups, err := e.List()
	if err != nil {
		return nil, err
	}

	toPrune := SelectBackupsToPrune(backups, e.retention)
	var purged []string
	for _, item := range toPrune {
		if err := os.Remove(item.Path); err == nil {
			purged = append(purged, item.Path)
		}
	}
	return purged, nil
}

// ValidateArchive checks whether a zip file is a valid backup archive and validates
// the embedded database using PRAGMA integrity_check.
func ValidateArchive(archivePath string) error {
	stagingDir, err := os.MkdirTemp("", "anilist-backup-validate-*")
	if err != nil {
		return fmt.Errorf("backup: failed to create validation directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(stagingDir)
	}()

	stagedDBPath, _, err := extractArchive(archivePath, stagingDir)
	if err != nil {
		return err
	}

	if err := storage.CheckFileIntegrity(context.Background(), stagedDBPath); err != nil {
		return fmt.Errorf("backup: integrity check failed: %w", err)
	}
	return nil
}

// Restore unpacks the archive into staging, verifies SQLite database integrity,
// drains active storage.DB connection pools to avoid Windows file locks, executes
// atomic file replacements, and resumes database connections.
func (e *Engine) Restore(ctx context.Context, archivePath string) error {
	if _, err := os.Stat(archivePath); err != nil {
		return fmt.Errorf("backup: backup archive does not exist at %s: %w", archivePath, err)
	}

	stagingDir, err := os.MkdirTemp("", "anilist-restore-staging-*")
	if err != nil {
		return fmt.Errorf("backup: failed to create staging directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(stagingDir)
	}()

	stagedDBPath, stagedConfigPath, err := extractArchive(archivePath, stagingDir)
	if err != nil {
		return fmt.Errorf("backup: failed to extract archive: %w", err)
	}

	// 1. Validate database integrity using PRAGMA integrity_check before touching live files
	if err := storage.CheckFileIntegrity(ctx, stagedDBPath); err != nil {
		return fmt.Errorf("backup: archive database integrity check failed: %w", err)
	}

	// 2. Validate staged configuration XML if present
	if stagedConfigPath != "" {
		cfgData, err := os.ReadFile(stagedConfigPath)
		if err != nil {
			return fmt.Errorf("backup: failed to read staged config: %w", err)
		}
		if _, err := config.ParseXML(cfgData); err != nil {
			return fmt.Errorf("backup: staged config is invalid XML: %w", err)
		}
	}

	// 3. Drain active database connection pools in storage.DB to eliminate Windows sharing violations
	if err := e.db.Drain(ctx); err != nil {
		return fmt.Errorf("backup: failed to drain database connection pools: %w", err)
	}
	drained := true
	defer func() {
		if drained {
			_ = e.db.Resume(ctx)
		}
	}()

	// 4. Atomically swap live database file on disk
	liveDBPath := e.db.Path()
	isMemory := liveDBPath == ":memory:" || strings.HasPrefix(liveDBPath, "file::memory:") || strings.Contains(liveDBPath, "mode=memory")
	if !isMemory {
		// Clean up old WAL and SHM files to prevent replay corruption
		_ = os.Remove(liveDBPath + "-wal")
		_ = os.Remove(liveDBPath + "-shm")

		if err := atomicReplace(stagedDBPath, liveDBPath); err != nil {
			return fmt.Errorf("backup: failed to replace live database file: %w", err)
		}
	}

	// 5. Replace config.xml if included in archive
	if stagedConfigPath != "" && e.configPath != "" {
		if err := atomicReplace(stagedConfigPath, e.configPath); err != nil {
			return fmt.Errorf("backup: failed to replace live config file: %w", err)
		}
	}

	// 6. Resume database connections
	drained = false
	if err := e.db.Resume(ctx); err != nil {
		return fmt.Errorf("backup: failed to resume database connection pools: %w", err)
	}

	return nil
}

const (
	maxDBEntrySize     int64 = 2 << 30  // 2 GiB maximum for SQLite database snapshot
	maxConfigEntrySize int64 = 10 << 20 // 10 MiB maximum for config.xml
)

// extractArchive unpacks a backup zip into stagingDir, returning the paths to sync.db and config.xml.
func extractArchive(archivePath, stagingDir string) (string, string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", "", fmt.Errorf("backup: failed to open zip archive: %w", err)
	}
	defer func() {
		_ = zr.Close()
	}()

	var stagedDBPath, stagedConfigPath string

	for _, file := range zr.File {
		cleanName := filepath.Base(file.Name)
		if cleanName != "sync.db" && cleanName != "config.xml" {
			continue
		}

		if (cleanName == "sync.db" && stagedDBPath != "") || (cleanName == "config.xml" && stagedConfigPath != "") {
			return "", "", fmt.Errorf("backup: duplicate %s entry in zip archive", cleanName)
		}

		// Security check: zip slip prevention
		destPath := filepath.Join(stagingDir, cleanName)
		if !strings.HasPrefix(filepath.Clean(destPath), filepath.Clean(stagingDir)) {
			return "", "", fmt.Errorf("backup: illegal file path in zip archive: %s", file.Name)
		}

		rc, err := file.Open()
		if err != nil {
			return "", "", fmt.Errorf("backup: failed to open zip entry %s: %w", file.Name, err)
		}

		outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			_ = rc.Close()
			return "", "", fmt.Errorf("backup: failed to create staging file %s: %w", destPath, err)
		}

		maxSize := maxConfigEntrySize
		if cleanName == "sync.db" {
			maxSize = maxDBEntrySize
		}

		n, err := io.Copy(outFile, io.LimitReader(rc, maxSize+1))
		_ = outFile.Close()
		_ = rc.Close()
		if err != nil {
			return "", "", fmt.Errorf("backup: failed to extract zip entry %s: %w", file.Name, err)
		}
		if n > maxSize {
			return "", "", fmt.Errorf("backup: zip entry %s exceeds maximum allowed size of %d bytes", file.Name, maxSize)
		}

		switch cleanName {
		case "sync.db":
			stagedDBPath = destPath
		case "config.xml":
			stagedConfigPath = destPath
		}
	}

	if stagedDBPath == "" {
		return "", "", fmt.Errorf("backup: archive does not contain sync.db")
	}

	return stagedDBPath, stagedConfigPath, nil
}

// atomicReplace replaces dst with src, handling Windows file locking and cross-volume boundaries.
func atomicReplace(src, dst string) error {
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	bakPath := dst + ".bak"
	_ = os.Remove(bakPath)

	hasExisting := false
	if _, err := os.Stat(dst); err == nil {
		hasExisting = true
		if err := os.Rename(dst, bakPath); err != nil {
			return fmt.Errorf("failed to backup existing file %s: %w", dst, err)
		}
	}

	// Try atomic rename first
	if err := os.Rename(src, dst); err != nil {
		// Fallback to copy across drive or volume boundaries
		if errCopy := copyFile(src, dst); errCopy != nil {
			if hasExisting {
				_ = os.Rename(bakPath, dst)
			}
			return fmt.Errorf("failed to replace destination file %s: %w", dst, errCopy)
		}
	}

	if hasExisting {
		_ = os.Remove(bakPath)
	}
	return nil
}

// copyFile copies src contents to dst and flushes buffers to disk.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = in.Close()
	}()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

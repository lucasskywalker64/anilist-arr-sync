package backup_test

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func setupTestEnvironment(t *testing.T) (*storage.DB, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sync.db")
	configPath := filepath.Join(dir, "config.xml")
	backupDir := filepath.Join(dir, "backups")

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	cfg := config.NewDefault()
	cfg.InstanceName = "TestInstance"
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	return db, dbPath, configPath, backupDir
}

func TestBackupEngine_CreateBackup_GeneratesZipWithDBSnapshotAndConfig(t *testing.T) {
	t.Parallel()

	db, _, configPath, backupDir := setupTestEnvironment(t)
	ctx := context.Background()

	// Insert test data in DB
	err := db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", "test_user", "test_hash")
		return err
	})
	if err != nil {
		t.Fatalf("insert user failed: %v", err)
	}

	fixedTime := time.Date(2026, 10, 9, 23, 30, 0, 0, time.UTC)
	engine := backup.NewEngine(db, configPath, backupDir, backup.WithNowFunc(func() time.Time {
		return fixedTime
	}))

	info, err := engine.Create(ctx, backup.TypeScheduled)
	if err != nil {
		t.Fatalf("engine.Create failed: %v", err)
	}

	expectedName := "anilist-arr-sync_backup_scheduled_2026.10.09_23.30.00.zip"
	if info.Filename != expectedName {
		t.Errorf("expected filename %q, got %q", expectedName, info.Filename)
	}
	if info.Type != backup.TypeScheduled {
		t.Errorf("expected type %q, got %q", backup.TypeScheduled, info.Type)
	}

	// Verify zip file exists on disk
	if _, err := os.Stat(info.Path); err != nil {
		t.Fatalf("backup file does not exist at %s: %v", info.Path, err)
	}

	// Inspect contents of the generated zip archive
	zipReader, err := zip.OpenReader(info.Path)
	if err != nil {
		t.Fatalf("failed to open zip archive: %v", err)
	}
	defer func() {
		_ = zipReader.Close()
	}()

	foundDB := false
	foundConfig := false

	extractDir := t.TempDir()

	for _, file := range zipReader.File {
		cleanName := filepath.Base(file.Name)
		if cleanName == "sync.db" {
			foundDB = true
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("failed to open sync.db in zip: %v", err)
			}
			outPath := filepath.Join(extractDir, "extracted_sync.db")
			outFile, err := os.Create(outPath)
			if err != nil {
				_ = rc.Close()
				t.Fatalf("failed to create extracted db file: %v", err)
			}
			_, _ = io.Copy(outFile, rc)
			_ = outFile.Close()
			_ = rc.Close()
		}
		if cleanName == "config.xml" {
			foundConfig = true
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("failed to open config.xml in zip: %v", err)
			}
			data, _ := io.ReadAll(rc)
			_ = rc.Close()
			if !strings.Contains(string(data), "TestInstance") {
				t.Errorf("expected config.xml to contain 'TestInstance', got %s", string(data))
			}
		}
	}

	if !foundDB {
		t.Fatal("sync.db not found in backup archive")
	}
	if !foundConfig {
		t.Fatal("config.xml not found in backup archive")
	}

	// Open extracted database and verify data
	extractedDB, err := storage.Open(filepath.Join(extractDir, "extracted_sync.db"))
	if err != nil {
		t.Fatalf("failed to open extracted db: %v", err)
	}
	defer func() { _ = extractedDB.Close() }()

	var userCount int
	err = extractedDB.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username = 'test_user';").Scan(&userCount)
	})
	if err != nil {
		t.Fatalf("query extracted db failed: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("expected 1 user in extracted db, got %d", userCount)
	}
}

func TestBackupEngine_CreateBackup_ConcurrentOperations(t *testing.T) {
	t.Parallel()

	db, _, configPath, backupDir := setupTestEnvironment(t)
	engine := backup.NewEngine(db, configPath, backupDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_ = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
						_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO users (username, password_hash) VALUES (?, ?);",
							fmt.Sprintf("conc_user_%d_%d", workerID, idx), "h")
						return err
					})
					idx++
				}
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)

	info, err := engine.Create(context.Background(), backup.TypeManual)
	if err != nil {
		t.Fatalf("engine.Create during concurrent operations failed: %v", err)
	}

	cancel()
	wg.Wait()

	if err := backup.ValidateArchive(info.Path); err != nil {
		t.Fatalf("generated archive failed validation: %v", err)
	}
}

func TestBackupEngine_RollingRetentionPruning(t *testing.T) {
	t.Parallel()

	db, _, configPath, backupDir := setupTestEnvironment(t)
	baseTime := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	currentTime := baseTime

	engine := backup.NewEngine(db, configPath, backupDir,
		backup.WithRetention(7),
		backup.WithNowFunc(func() time.Time {
			return currentTime
		}),
	)

	ctx := context.Background()

	// Create 10 daily backups
	for day := 0; day < 10; day++ {
		currentTime = baseTime.Add(time.Duration(day) * 24 * time.Hour)
		_, err := engine.Create(ctx, backup.TypeScheduled)
		if err != nil {
			t.Fatalf("failed to create backup on day %d: %v", day, err)
		}
	}

	// Verify exactly 7 daily backups remain in backup directory
	list, err := engine.List()
	if err != nil {
		t.Fatalf("engine.List failed: %v", err)
	}
	if len(list) != 7 {
		t.Fatalf("expected 7 backups after retention pruning, got %d", len(list))
	}

	// Verify the oldest 3 backups (days 0, 1, 2) were pruned, and days 3-9 remain
	oldestRemainingTime := baseTime.Add(3 * 24 * time.Hour)
	newestRemainingTime := baseTime.Add(9 * 24 * time.Hour)

	if !list[0].CreatedAt.Equal(newestRemainingTime) {
		t.Errorf("expected newest backup to be %v, got %v", newestRemainingTime, list[0].CreatedAt)
	}
	if !list[len(list)-1].CreatedAt.Equal(oldestRemainingTime) {
		t.Errorf("expected oldest remaining backup to be %v, got %v", oldestRemainingTime, list[len(list)-1].CreatedAt)
	}
}

func TestBackupEngine_Restore_ValidatesIntegrityCheckAndRejectsCorruptDB(t *testing.T) {
	t.Parallel()

	db, dbPath, configPath, backupDir := setupTestEnvironment(t)
	ctx := context.Background()

	// Initial user in live DB
	_ = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", "original_user", "orig_hash")
		return err
	})

	// Create a corrupted backup archive manually
	_ = os.MkdirAll(backupDir, 0755)
	corruptZipPath := filepath.Join(backupDir, "anilist-arr-sync_backup_manual_2026.10.09_23.00.00.zip")
	zipFile, err := os.Create(corruptZipPath)
	if err != nil {
		t.Fatalf("failed to create zip file: %v", err)
	}
	zw := zip.NewWriter(zipFile)

	// Write garbage data for sync.db
	dbEntry, err := zw.Create("sync.db")
	if err != nil {
		t.Fatalf("failed to create sync.db entry in zip: %v", err)
	}
	_, _ = dbEntry.Write([]byte("THIS IS NOT A VALID SQLITE DATABASE FILE GARBAGE DATA"))

	cfgEntry, err := zw.Create("config.xml")
	if err != nil {
		t.Fatalf("failed to create config.xml entry in zip: %v", err)
	}
	_, _ = cfgEntry.Write([]byte("<Config><InstanceName>CorruptBackup</InstanceName></Config>"))

	_ = zw.Close()
	_ = zipFile.Close()

	engine := backup.NewEngine(db, configPath, backupDir)

	// Attempt restore of corrupt archive
	err = engine.Restore(ctx, corruptZipPath)
	if err == nil {
		t.Fatal("expected restore of corrupted database to fail integrity check, got nil")
	}

	// Verify live DB was NOT touched and original user is still present
	var count int
	err = db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username = 'original_user';").Scan(&count)
	})
	if err != nil {
		t.Fatalf("query live DB failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected original_user to remain in live DB, got %d", count)
	}

	// Verify live config.xml was NOT overwritten
	cfg, err := config.Load(configPath, nil)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.InstanceName != "TestInstance" {
		t.Errorf("expected config instance name 'TestInstance', got %q", cfg.InstanceName)
	}
	_ = dbPath
}

func TestBackupEngine_Restore_FullRestoreWithConnectionDraining(t *testing.T) {
	t.Parallel()

	db, _, configPath, backupDir := setupTestEnvironment(t)
	ctx := context.Background()

	// Initial data for Backup A
	_ = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", "user_backup_a", "hash_a")
		return err
	})

	cfgA, _ := config.Load(configPath, nil)
	cfgA.InstanceName = "InstanceA"
	_ = config.Save(configPath, cfgA)

	engine := backup.NewEngine(db, configPath, backupDir)

	// Create Backup A
	backupA, err := engine.Create(ctx, backup.TypeManual)
	if err != nil {
		t.Fatalf("failed to create backup A: %v", err)
	}

	// Mutate live DB and config to State B
	_ = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", "user_state_b", "hash_b")
		return err
	})
	cfgB, _ := config.Load(configPath, nil)
	cfgB.InstanceName = "InstanceB"
	_ = config.Save(configPath, cfgB)

	// Perform full restore of Backup A
	err = engine.Restore(ctx, backupA.Path)
	if err != nil {
		t.Fatalf("engine.Restore failed: %v", err)
	}

	// Verify database content: user_backup_a exists, user_state_b DOES NOT exist
	var countA, countB int
	_ = db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		_ = q.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username = 'user_backup_a';").Scan(&countA)
		_ = q.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username = 'user_state_b';").Scan(&countB)
		return nil
	})

	if countA != 1 {
		t.Errorf("expected user_backup_a to exist after restore, got %d", countA)
	}
	if countB != 0 {
		t.Errorf("expected user_state_b to be eliminated after restore, got %d", countB)
	}

	// Verify config was restored to InstanceA
	restoredCfg, err := config.Load(configPath, nil)
	if err != nil {
		t.Fatalf("load restored config failed: %v", err)
	}
	if restoredCfg.InstanceName != "InstanceA" {
		t.Errorf("expected restored config to be 'InstanceA', got %q", restoredCfg.InstanceName)
	}

	// Verify database can continue accepting new writes after restore
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", "post_restore_user", "post_hash")
		return err
	})
	if err != nil {
		t.Fatalf("write after restore failed: %v", err)
	}
}

func TestBackupEngine_Restore_ArchiveMissingSyncDB(t *testing.T) {
	t.Parallel()

	db, _, configPath, backupDir := setupTestEnvironment(t)
	_ = os.MkdirAll(backupDir, 0755)
	zipPath := filepath.Join(backupDir, "anilist-arr-sync_backup_manual_2026.10.09_23.40.00.zip")

	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("failed to create zip file: %v", err)
	}
	zw := zip.NewWriter(zipFile)
	cfgEntry, _ := zw.Create("config.xml")
	_, _ = cfgEntry.Write([]byte("<Config></Config>"))
	_ = zw.Close()
	_ = zipFile.Close()

	engine := backup.NewEngine(db, configPath, backupDir)
	err = engine.Restore(context.Background(), zipPath)
	if err == nil {
		t.Fatal("expected restore to fail when sync.db is missing from archive, got nil")
	}
	if !strings.Contains(err.Error(), "does not contain sync.db") {
		t.Errorf("expected error message to mention sync.db, got %v", err)
	}
}

func TestBackupEngine_Restore_NonExistentArchive(t *testing.T) {
	t.Parallel()

	db, _, configPath, backupDir := setupTestEnvironment(t)
	engine := backup.NewEngine(db, configPath, backupDir)

	err := engine.Restore(context.Background(), filepath.Join(backupDir, "non_existent.zip"))
	if err == nil {
		t.Fatal("expected error for non-existent archive, got nil")
	}
}

func TestBackupEngine_List_NonExistentBackupDir(t *testing.T) {
	t.Parallel()

	db, _, configPath, _ := setupTestEnvironment(t)
	engine := backup.NewEngine(db, configPath, filepath.Join(t.TempDir(), "does_not_exist"))

	list, err := engine.List()
	if err != nil {
		t.Fatalf("unexpected error from List on non-existent dir: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 items, got %d", len(list))
	}
}

func TestBackupEngine_Restore_RejectsDuplicateEntries(t *testing.T) {
	t.Parallel()

	db, _, configPath, backupDir := setupTestEnvironment(t)
	_ = os.MkdirAll(backupDir, 0755)
	zipPath := filepath.Join(backupDir, "anilist-arr-sync_backup_manual_2026.10.09_23.50.00.zip")

	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("failed to create zip file: %v", err)
	}
	zw := zip.NewWriter(zipFile)
	e1, _ := zw.Create("a/sync.db")
	_, _ = e1.Write([]byte("db1"))
	e2, _ := zw.Create("b/sync.db")
	_, _ = e2.Write([]byte("db2"))
	_ = zw.Close()
	_ = zipFile.Close()

	engine := backup.NewEngine(db, configPath, backupDir)
	err = engine.Restore(context.Background(), zipPath)
	if err == nil {
		t.Fatal("expected restore to reject archive with duplicate sync.db entries, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate sync.db entry") {
		t.Errorf("expected duplicate error message, got %v", err)
	}
}

func TestBackupEngine_Restore_IgnoresExtraneousEntries(t *testing.T) {
	t.Parallel()

	db, _, configPath, backupDir := setupTestEnvironment(t)
	engine := backup.NewEngine(db, configPath, backupDir)
	ctx := context.Background()

	// Create valid backup
	info, err := engine.Create(ctx, backup.TypeManual)
	if err != nil {
		t.Fatalf("Create backup failed: %v", err)
	}

	// Create a new zip with an extra file added
	corruptDir := t.TempDir()
	modZipPath := filepath.Join(corruptDir, "mod_backup.zip")
	modFile, err := os.Create(modZipPath)
	if err != nil {
		t.Fatalf("create mod zip failed: %v", err)
	}
	zw := zip.NewWriter(modFile)

	origReader, err := zip.OpenReader(info.Path)
	if err != nil {
		t.Fatalf("open original zip failed: %v", err)
	}
	for _, f := range origReader.File {
		w, _ := zw.Create(f.Name)
		rc, _ := f.Open()
		_, _ = io.Copy(w, rc)
		_ = rc.Close()
	}
	_ = origReader.Close()

	// Add extraneous file
	extra, _ := zw.Create("malicious.exe")
	_, _ = extra.Write([]byte("should not be extracted"))
	_ = zw.Close()
	_ = modFile.Close()

	// Restoring the archive should succeed and ignore malicious.exe
	if err := engine.Restore(ctx, modZipPath); err != nil {
		t.Fatalf("Restore with extra file failed: %v", err)
	}
}

func TestBackupEngine_Restore_RecoversOnCanceledContext(t *testing.T) {
	t.Parallel()

	db, _, configPath, backupDir := setupTestEnvironment(t)
	engine := backup.NewEngine(db, configPath, backupDir)

	ctx := context.Background()
	info, err := engine.Create(ctx, backup.TypeManual)
	if err != nil {
		t.Fatalf("Create backup failed: %v", err)
	}

	// Create an already-canceled context
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	// Restore with canceled context should fail, but defer must un-drain the database pools
	err = engine.Restore(canceledCtx, info.Path)
	if err == nil {
		t.Fatal("expected restore with canceled context to fail, got nil")
	}

	// Live database must not be permanently drained
	var count int
	err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM users;").Scan(&count)
	})
	if err != nil {
		t.Fatalf("database connection pool remained drained after canceled restore: %v", err)
	}
}

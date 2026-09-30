package storage_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func TestOpen_Memory(t *testing.T) {
	t.Parallel()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close failed: %v", err)
		}
	}()

	var mode string
	err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&mode)
	})
	if err != nil {
		t.Fatalf("query journal_mode failed: %v", err)
	}
	// In-memory SQLite uses memory journal mode
	if mode != "memory" {
		t.Errorf("expected journal_mode memory for :memory:, got %q", mode)
	}
}

func TestOpen_OnDiskWAL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sync.db")

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dbPath, err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close failed: %v", err)
		}
	}()

	var mode string
	err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&mode)
	})
	if err != nil {
		t.Fatalf("query journal_mode failed: %v", err)
	}
	if mode != "wal" {
		t.Errorf("expected journal_mode wal for on-disk database, got %q", mode)
	}
}

func TestMigrations_TablesAndIndicesExist(t *testing.T) {
	t.Parallel()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close failed: %v", err)
		}
	}()

	expectedTables := []string{
		"users",
		"mapping_overrides",
		"review_queue",
		"staged_sync_actions",
		"fribb_meta",
		"fribb_entries",
		"sync_history",
		"update_history",
		"ignored_titles",
		"notification_connections",
	}

	for _, table := range expectedTables {
		var name string
		err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
			return q.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?;", table).Scan(&name)
		})
		if err != nil {
			t.Errorf("table %q does not exist: %v", table, err)
		}
	}

	expectedIndices := []string{
		"idx_fribb_tvdb",
		"idx_fribb_tmdb",
	}

	for _, index := range expectedIndices {
		var name string
		err := db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
			return q.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?;", index).Scan(&name)
		})
		if err != nil {
			t.Errorf("index %q does not exist: %v", index, err)
		}
	}
}

func TestMigrations_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sync.db")

	// First open applies migrations
	db1, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}

	// Insert sample record to verify data preservation
	err = db1.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", "admin", "hash123")
		return err
	})
	if err != nil {
		_ = db1.Close()
		t.Fatalf("failed to insert test record: %v", err)
	}

	if err := db1.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// Reopen existing database, triggering migrations again
	db2, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	defer func() {
		if err := db2.Close(); err != nil {
			t.Errorf("second Close failed: %v", err)
		}
	}()

	var username string
	err = db2.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT username FROM users WHERE id = 1;").Scan(&username)
	})
	if err != nil {
		t.Fatalf("failed to query test user after reopen: %v", err)
	}
	if username != "admin" {
		t.Errorf("expected username 'admin', got %q", username)
	}
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	t.Parallel()

	testDB := func(t *testing.T, path string) {
		db, err := storage.Open(path)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer func() {
			if err := db.Close(); err != nil {
				t.Errorf("Close failed: %v", err)
			}
		}()

		const numWriters = 8
		const writesPerGoroutine = 25
		const numReaders = 16

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var readersWg sync.WaitGroup
		var writersWg sync.WaitGroup

		// Start concurrent readers
		for range numReaders {
			readersWg.Go(func() {
				for {
					select {
					case <-ctx.Done():
						return
					default:
						var count int
						_ = db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
							return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM users;").Scan(&count)
						})
					}
				}
			})
		}

		// Start concurrent writers
		for workerID := range numWriters {
			writersWg.Go(func() {
				for j := range writesPerGoroutine {
					username := fmt.Sprintf("user_%d_%d", workerID, j)
					err := db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
						_, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", username, "hash")
						return err
					})
					if err != nil {
						t.Errorf("worker %d write %d failed: %v", workerID, j, err)
						return
					}
				}
			})
		}

		writersWg.Wait()
		cancel()
		readersWg.Wait()

		var totalUsers int
		err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
			return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM users;").Scan(&totalUsers)
		})
		if err != nil {
			t.Fatalf("query total users failed: %v", err)
		}

		expectedTotal := numWriters * writesPerGoroutine
		if totalUsers != expectedTotal {
			t.Errorf("expected %d total users, got %d", expectedTotal, totalUsers)
		}
	}

	t.Run("OnDisk", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testDB(t, filepath.Join(dir, "concurrency.db"))
	})

	t.Run("InMemory", func(t *testing.T) {
		t.Parallel()
		testDB(t, ":memory:")
	})
}

func TestWrite_RollbackOnError(t *testing.T) {
	t.Parallel()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	errExpected := fmt.Errorf("intentional failure")
	err = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", "rollback_user", "hash"); err != nil {
			return err
		}
		return errExpected
	})
	if err != errExpected {
		t.Fatalf("expected error %v, got %v", errExpected, err)
	}

	var count int
	err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username = ?;", "rollback_user").Scan(&count)
	})
	if err != nil {
		t.Fatalf("query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 users after rollback, got %d", count)
	}
}

func TestWrite_PanicRecoveryRollsBack(t *testing.T) {
	t.Parallel()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic, got nil")
			}
		}()

		_ = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", "panicker", "hash"); err != nil {
				return err
			}
			panic("simulated panic inside write callback")
		})
	}()

	// Verify uncommitted rows from panicked callback were rolled back
	var count int
	err = db.Read(context.Background(), func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username = ?;", "panicker").Scan(&count)
	})
	if err != nil {
		t.Fatalf("query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 users after panic rollback, got %d", count)
	}

	// Verify write pool connection was returned to pool and subsequent writes succeed
	err = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", "next_user", "hash")
		return err
	})
	if err != nil {
		t.Fatalf("subsequent write after panic failed: %v", err)
	}
}

func TestClosedDB(t *testing.T) {
	t.Parallel()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Double close should succeed idempotently
	if err := db.Close(); err != nil {
		t.Errorf("subsequent Close failed: %v", err)
	}

	err = db.Read(context.Background(), func(_ context.Context, _ storage.Querier) error {
		return nil
	})
	if err == nil {
		t.Error("expected Read on closed DB to return error")
	}

	err = db.Write(context.Background(), func(_ context.Context, _ *sql.Tx) error {
		return nil
	})
	if err == nil {
		t.Error("expected Write on closed DB to return error")
	}
}

func TestOpen_InvalidPath(t *testing.T) {
	t.Parallel()

	_, err := storage.Open("")
	if err == nil {
		t.Error("expected Open with empty path to return error")
	}
}

func TestDBAccessors(t *testing.T) {
	t.Parallel()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	if db.Path() != ":memory:" {
		t.Errorf("expected Path :memory:, got %q", db.Path())
	}
	if db.Reader() == nil {
		t.Error("expected Reader() to not be nil")
	}
	if db.Writer() == nil {
		t.Error("expected Writer() to not be nil")
	}
}

func TestTableConstraintsAndSchema(t *testing.T) {
	t.Parallel()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	ctx := context.Background()

	// 1. users: unique constraint
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", "alice", "hash1")
		return err
	})
	if err != nil {
		t.Fatalf("inserting first user failed: %v", err)
	}
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?);", "alice", "hash2")
		return err
	})
	if err == nil {
		t.Error("expected duplicate username to violate UNIQUE constraint")
	}

	// 2. mapping_overrides: media_type CHECK constraint
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO mapping_overrides (anilist_id, media_type) VALUES (?, ?);", 101, "INVALID")
		return err
	})
	if err == nil {
		t.Error("expected invalid media_type in mapping_overrides to violate CHECK constraint")
	}
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO mapping_overrides (anilist_id, media_type) VALUES (?, ?);", 101, "SERIES")
		return err
	})
	if err != nil {
		t.Errorf("valid mapping_overrides insert failed: %v", err)
	}

	// 3. review_queue: media_type and status CHECK constraints
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO review_queue (anilist_id, media_type, title_romaji, candidates_json, reason, status)
			VALUES (?, ?, ?, ?, ?, ?);`,
			202, "MOVIE", "Test Title", "[]", "ambiguous", "BAD_STATUS")
		return err
	})
	if err == nil {
		t.Error("expected invalid status in review_queue to violate CHECK constraint")
	}

	// 4. staged_sync_actions: action_type CHECK constraint
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO staged_sync_actions (action_type, media_type, title, target_service, payload_json)
			VALUES (?, ?, ?, ?, ?);`,
			"DELETE_EVERYTHING", "MOVIE", "Title", "RADARR", "{}")
		return err
	})
	if err == nil {
		t.Error("expected invalid action_type in staged_sync_actions to violate CHECK constraint")
	}

	// 5. fribb_meta and fribb_entries
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO fribb_meta (key, etag, entry_count) VALUES (?, ?, ?);", "anime-list-mini", "etag1", 50); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO fribb_entries (anilist_id, tvdb_id, tmdb_id) VALUES (?, ?, ?);", 303, 1234, 5678)
		return err
	})
	if err != nil {
		t.Errorf("inserting fribb meta and entry failed: %v", err)
	}

	// 6. sync_history: status and trigger_type CHECK constraints
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO sync_history (duration_ms, status, trigger_type)
			VALUES (?, ?, ?);`,
			150, "NOT_REAL", "CLI")
		return err
	})
	if err == nil {
		t.Error("expected invalid status in sync_history to violate CHECK constraint")
	}

	// 7. update_history: status CHECK constraint
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO update_history (version, status) VALUES (?, ?);", "v1.0.0", "INVALID")
		return err
	})
	if err == nil {
		t.Error("expected invalid status in update_history to violate CHECK constraint")
	}

	// 8. ignored_titles
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO ignored_titles (anilist_id, title_romaji, reason) VALUES (?, ?, ?);", 404, "Skipped Show", "User skipped")
		return err
	})
	if err != nil {
		t.Errorf("inserting ignored title failed: %v", err)
	}

	// 9. notification_connections: provider CHECK constraint
	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO notification_connections (name, provider, config_json) VALUES (?, ?, ?);", "Bad", "TELEGRAM", "{}")
		return err
	})
	if err == nil {
		t.Error("expected invalid provider in notification_connections to violate CHECK constraint")
	}

	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO notification_connections (name, provider, config_json) VALUES (?, ?, ?);", "Discord Alert", "DISCORD", "{}")
		return err
	})
	if err != nil {
		t.Errorf("inserting valid notification connection failed: %v", err)
	}
}

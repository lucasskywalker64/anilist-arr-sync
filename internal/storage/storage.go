// Package storage manages the embedded SQLite database engine and schema migrations.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	_ "modernc.org/sqlite" // Register pure-Go SQLite database driver.
)

var memCounter atomic.Uint64

// Querier abstracts database query operations across *sql.DB, *sql.Tx, and *sql.Conn.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DB coordinates SQLite access with concurrent readers and serialized writers.
type DB struct {
	readPool  *sql.DB
	writePool *sql.DB
	txMu      sync.RWMutex
	closeMu   sync.RWMutex
	drainMu   sync.Mutex
	isDrained bool
	isClosed  bool
	isMemory  bool
	path      string
}

// openPools initializes SQLite connection pools with PRAGMA settings.
func openPools(path string, isMemory bool) (*sql.DB, *sql.DB, error) {
	var dsn string
	if isMemory {
		id := memCounter.Add(1)
		dsn = fmt.Sprintf("file:memdb_%d?mode=memory&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", id)
	} else {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, nil, fmt.Errorf("storage: failed to create database directory %s: %w", dir, err)
		}
		cleanPath := filepath.ToSlash(path)
		dsn = fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", cleanPath)
	}

	writePool, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: failed to open write pool: %w", err)
	}
	writePool.SetMaxOpenConns(1)
	writePool.SetMaxIdleConns(1)

	if !isMemory {
		if _, err := writePool.Exec("PRAGMA journal_mode = WAL;"); err != nil {
			_ = writePool.Close()
			return nil, nil, fmt.Errorf("storage: failed to set WAL mode: %w", err)
		}
		if _, err := writePool.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
			_ = writePool.Close()
			return nil, nil, fmt.Errorf("storage: failed to set synchronous mode: %w", err)
		}
	}
	if _, err := writePool.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		_ = writePool.Close()
		return nil, nil, fmt.Errorf("storage: failed to set busy_timeout: %w", err)
	}
	if _, err := writePool.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		_ = writePool.Close()
		return nil, nil, fmt.Errorf("storage: failed to enable foreign keys: %w", err)
	}

	var readPool *sql.DB
	if isMemory {
		readPool = writePool
	} else {
		readPool, err = sql.Open("sqlite", dsn)
		if err != nil {
			_ = writePool.Close()
			return nil, nil, fmt.Errorf("storage: failed to open read pool: %w", err)
		}

		numCPU := runtime.NumCPU()
		if numCPU < 4 {
			numCPU = 4
		}
		readPool.SetMaxOpenConns(numCPU)
		readPool.SetMaxIdleConns(numCPU)
	}

	return readPool, writePool, nil
}

// Open initializes an embedded SQLite database at path, applies PRAGMA settings,
// and prepares connection pools for concurrent reads and serialized writes.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("storage: database path cannot be empty")
	}

	isMemory := path == ":memory:" || strings.HasPrefix(path, "file::memory:") || strings.Contains(path, "mode=memory")
	readPool, writePool, err := openPools(path, isMemory)
	if err != nil {
		return nil, err
	}

	db := &DB{
		readPool:  readPool,
		writePool: writePool,
		isMemory:  isMemory,
		path:      path,
	}

	if err := db.Migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("storage: failed to execute startup migrations: %w", err)
	}

	return db, nil
}

// Close cleanly flushes and closes both read and write connection pools.
func (db *DB) Close() error {
	db.drainMu.Lock()
	defer db.drainMu.Unlock()

	db.closeMu.Lock()
	defer db.closeMu.Unlock()

	if db.isClosed {
		return nil
	}
	db.isClosed = true

	var errs []string
	if db.readPool != nil && db.readPool != db.writePool {
		if err := db.readPool.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("read pool: %v", err))
		}
	}
	if db.writePool != nil {
		if err := db.writePool.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("write pool: %v", err))
		}
	}

	db.readPool = nil
	db.writePool = nil

	if len(errs) > 0 {
		return fmt.Errorf("storage: error closing database: %s", strings.Join(errs, ", "))
	}
	return nil
}

// Drain pauses database operations, waits for active read and write transactions
// to complete, and closes both connection pools. This eliminates Windows file locks
// so the underlying database file can be replaced.
func (db *DB) Drain(_ context.Context) error {
	db.drainMu.Lock()
	defer db.drainMu.Unlock()

	db.closeMu.Lock()
	defer db.closeMu.Unlock()

	if db.isClosed {
		return fmt.Errorf("storage: database is closed")
	}
	if db.isDrained {
		return nil
	}
	if db.isMemory {
		return nil
	}

	var errs []string
	if db.readPool != nil && db.readPool != db.writePool {
		if err := db.readPool.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("read pool: %v", err))
		}
	}
	if db.writePool != nil {
		if err := db.writePool.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("write pool: %v", err))
		}
	}

	db.readPool = nil
	db.writePool = nil
	db.isDrained = true

	if len(errs) > 0 {
		return fmt.Errorf("storage: error draining database pools: %s", strings.Join(errs, ", "))
	}
	return nil
}

// Resume re-opens the read and write connection pools, executes any pending
// schema migrations on the new pools, and unblocks pending operations.
func (db *DB) Resume(ctx context.Context) error {
	db.drainMu.Lock()
	defer db.drainMu.Unlock()

	db.closeMu.Lock()
	defer db.closeMu.Unlock()

	if db.isClosed {
		return fmt.Errorf("storage: database is closed")
	}
	if !db.isDrained {
		return nil
	}
	if db.isMemory {
		return nil
	}

	readPool, writePool, err := openPools(db.path, db.isMemory)
	if err != nil {
		return fmt.Errorf("storage: failed to resume database pools: %w", err)
	}

	if err := runMigrations(ctx, writePool); err != nil {
		_ = writePool.Close()
		if readPool != writePool {
			_ = readPool.Close()
		}
		return fmt.Errorf("storage: failed to migrate database on resume: %w", err)
	}

	db.readPool = readPool
	db.writePool = writePool
	db.isDrained = false
	return nil
}

// DrainAndReplace executes a file replacement callback while connection pools are drained.
func (db *DB) DrainAndReplace(ctx context.Context, replaceFn func() error) (err error) {
	if drainErr := db.Drain(ctx); drainErr != nil {
		return drainErr
	}
	defer func() {
		resumeErr := db.Resume(context.Background())
		if err == nil && resumeErr != nil {
			err = resumeErr
		}
	}()
	return replaceFn()
}

// VacuumInto executes VACUUM INTO to create an online point-in-time snapshot of the database
// at destPath within a read transaction concurrently with active operations.
func (db *DB) VacuumInto(ctx context.Context, destPath string) error {
	if destPath == "" {
		return fmt.Errorf("storage: destination path cannot be empty")
	}

	cleanPath := filepath.ToSlash(destPath)
	escapedPath := strings.ReplaceAll(cleanPath, "'", "''")
	query := fmt.Sprintf("VACUUM INTO '%s';", escapedPath)

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("storage: failed to create destination directory %s: %w", dir, err)
	}

	_ = os.Remove(destPath)

	return db.Read(ctx, func(ctx context.Context, q Querier) error {
		if _, err := q.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("storage: vacuum into failed: %w", err)
		}
		return nil
	})
}

// CheckFileIntegrity verifies the SQLite database integrity at filePath using PRAGMA integrity_check.
func CheckFileIntegrity(ctx context.Context, filePath string) error {
	cleanPath := filepath.ToSlash(filePath)
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)", cleanPath)
	checkDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("storage: failed to open file for integrity check: %w", err)
	}
	defer func() { _ = checkDB.Close() }()

	rows, err := checkDB.QueryContext(ctx, "PRAGMA integrity_check;")
	if err != nil {
		return fmt.Errorf("storage: failed to execute PRAGMA integrity_check: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []string
	for rows.Next() {
		var res string
		if err := rows.Scan(&res); err != nil {
			return fmt.Errorf("storage: failed to scan integrity check row: %w", err)
		}
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: integrity check iteration error: %w", err)
	}

	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("storage: database integrity check failed: %s", strings.Join(results, "; "))
	}
	return nil
}

// CheckIntegrity executes PRAGMA integrity_check on the current database connection.
func (db *DB) CheckIntegrity(ctx context.Context) error {
	return db.Read(ctx, func(ctx context.Context, q Querier) error {
		rows, err := q.QueryContext(ctx, "PRAGMA integrity_check;")
		if err != nil {
			return fmt.Errorf("storage: failed to execute PRAGMA integrity_check: %w", err)
		}
		defer func() { _ = rows.Close() }()

		var results []string
		for rows.Next() {
			var res string
			if err := rows.Scan(&res); err != nil {
				return fmt.Errorf("storage: failed to scan integrity check row: %w", err)
			}
			results = append(results, res)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("storage: integrity check iteration error: %w", err)
		}

		if len(results) != 1 || results[0] != "ok" {
			return fmt.Errorf("storage: database integrity check failed: %s", strings.Join(results, "; "))
		}
		return nil
	})
}

// Read executes a read-only callback using the read connection pool.
func (db *DB) Read(ctx context.Context, fn func(ctx context.Context, q Querier) error) error {
	db.closeMu.RLock()
	defer db.closeMu.RUnlock()

	if db.isClosed {
		return fmt.Errorf("storage: database is closed")
	}
	if db.isDrained {
		return fmt.Errorf("storage: database is drained")
	}

	if db.isMemory {
		db.txMu.RLock()
		defer db.txMu.RUnlock()
	}

	return fn(ctx, db.readPool)
}

// Write executes a write callback serialized through the write connection pool and mutex.
func (db *DB) Write(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	db.closeMu.RLock()
	defer db.closeMu.RUnlock()

	if db.isClosed {
		return fmt.Errorf("storage: database is closed")
	}
	if db.isDrained {
		return fmt.Errorf("storage: database is drained")
	}

	db.txMu.Lock()
	defer db.txMu.Unlock()

	tx, err := db.writePool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: failed to begin write transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := fn(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: failed to commit write transaction: %w", err)
	}

	return nil
}

// Reader returns the underlying read connection pool.
func (db *DB) Reader() *sql.DB {
	return db.readPool
}

// Writer returns the underlying write connection pool.
func (db *DB) Writer() *sql.DB {
	return db.writePool
}

// Path returns the configured database file path or URI.
func (db *DB) Path() string {
	return db.path
}

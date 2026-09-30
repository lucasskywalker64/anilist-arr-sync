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

var memCounter uint64

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
	writeMu   sync.Mutex
	memMu     sync.RWMutex
	closeMu   sync.RWMutex
	isClosed  bool
	isMemory  bool
	path      string
}

// Open initializes an embedded SQLite database at path, applies PRAGMA settings,
// and prepares connection pools for concurrent reads and serialized writes.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("storage: database path cannot be empty")
	}

	var dsn string
	isMemory := path == ":memory:" || strings.HasPrefix(path, "file::memory:") || strings.Contains(path, "mode=memory")

	if isMemory {
		id := atomic.AddUint64(&memCounter, 1)
		dsn = fmt.Sprintf("file:memdb_%d?mode=memory&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", id)
	} else {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("storage: failed to create database directory %s: %w", dir, err)
		}
		// Convert Windows backslashes to forward slashes for SQLite URI if needed
		cleanPath := filepath.ToSlash(path)
		dsn = fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", cleanPath)
	}

	// Open write pool with single connection to serialize writes.
	writePool, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to open write pool: %w", err)
	}
	writePool.SetMaxOpenConns(1)
	writePool.SetMaxIdleConns(1)

	// Configure database pragmas on the write connection.
	if !isMemory {
		if _, err := writePool.Exec("PRAGMA journal_mode = WAL;"); err != nil {
			_ = writePool.Close()
			return nil, fmt.Errorf("storage: failed to set WAL mode: %w", err)
		}
		if _, err := writePool.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
			_ = writePool.Close()
			return nil, fmt.Errorf("storage: failed to set synchronous mode: %w", err)
		}
	}
	if _, err := writePool.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		_ = writePool.Close()
		return nil, fmt.Errorf("storage: failed to set busy_timeout: %w", err)
	}
	if _, err := writePool.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		_ = writePool.Close()
		return nil, fmt.Errorf("storage: failed to enable foreign keys: %w", err)
	}

	// For in-memory databases, share the single connection to prevent table-level
	// lock conflicts from shared-cache mode. For on-disk, open a dedicated read pool.
	var readPool *sql.DB
	if isMemory {
		readPool = writePool
	} else {
		readPool, err = sql.Open("sqlite", dsn)
		if err != nil {
			_ = writePool.Close()
			return nil, fmt.Errorf("storage: failed to open read pool: %w", err)
		}

		numCPU := runtime.NumCPU()
		if numCPU < 4 {
			numCPU = 4
		}
		readPool.SetMaxOpenConns(numCPU)
		readPool.SetMaxIdleConns(numCPU)
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

	if len(errs) > 0 {
		return fmt.Errorf("storage: error closing database: %s", strings.Join(errs, ", "))
	}
	return nil
}

// Read executes a read-only callback using the read connection pool.
func (db *DB) Read(ctx context.Context, fn func(ctx context.Context, q Querier) error) error {
	db.closeMu.RLock()
	defer db.closeMu.RUnlock()

	if db.isClosed {
		return fmt.Errorf("storage: database is closed")
	}

	if db.isMemory {
		db.memMu.RLock()
		defer db.memMu.RUnlock()
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

	if db.isMemory {
		db.memMu.Lock()
		defer db.memMu.Unlock()
	}

	db.writeMu.Lock()
	defer db.writeMu.Unlock()

	tx, err := db.writePool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: failed to begin write transaction: %w", err)
	}

	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback()
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

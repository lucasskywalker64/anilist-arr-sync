package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("storage: failed to read embedded migrations directory: %w", err)
	}

	var list []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		filename := entry.Name()
		base := strings.TrimSuffix(filename, ".sql")
		parts := strings.SplitN(base, "_", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("storage: invalid migration filename %q (expected format: 0001_name.sql)", filename)
		}

		ver, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("storage: invalid migration version prefix in %q: %w", filename, err)
		}

		content, err := fs.ReadFile(migrationsFS, "migrations/"+filename)
		if err != nil {
			return nil, fmt.Errorf("storage: failed to read migration file %q: %w", filename, err)
		}

		list = append(list, migration{
			version: ver,
			name:    parts[1],
			sql:     string(content),
		})
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].version < list[j].version
	})

	return list, nil
}

// Migrate executes all pending schema migrations idempotently in a write transaction.
func (db *DB) Migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	return db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`
		if _, err := tx.ExecContext(ctx, createMigrationsTable); err != nil {
			return fmt.Errorf("storage: failed to create schema_migrations table: %w", err)
		}

		for _, m := range migrations {
			var applied int
			row := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?;", m.version)
			if err := row.Scan(&applied); err != nil {
				return fmt.Errorf("storage: failed to check migration status for version %d: %w", m.version, err)
			}

			if applied > 0 {
				continue
			}

			if _, err := tx.ExecContext(ctx, m.sql); err != nil {
				return fmt.Errorf("storage: failed to apply migration %d (%s): %w", m.version, m.name, err)
			}

			if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, name) VALUES (?, ?);", m.version, m.name); err != nil {
				return fmt.Errorf("storage: failed to record migration %d: %w", m.version, err)
			}
		}

		return nil
	})
}

package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

// Store provides persistence operations for notification connections.
type Store struct {
	db *storage.DB
}

// NewStore constructs a Store backed by storage.DB.
func NewStore(db *storage.DB) *Store {
	return &Store{db: db}
}

// List returns all persisted notification connections ordered by ID.
func (s *Store) List(ctx context.Context) ([]Connection, error) {
	var connections []Connection
	err := s.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		rows, err := q.QueryContext(ctx, `
			SELECT id, name, provider, config_json, on_added, on_review, on_error, on_complete, on_update, created_at, updated_at
			FROM notification_connections
			ORDER BY id ASC;
		`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var c Connection
			if err := rows.Scan(
				&c.ID, &c.Name, &c.Provider, &c.ConfigJSON,
				&c.OnAdded, &c.OnReview, &c.OnError, &c.OnComplete, &c.OnUpdate,
				&c.CreatedAt, &c.UpdatedAt,
			); err != nil {
				return err
			}
			connections = append(connections, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list notification connections: %w", err)
	}
	if connections == nil {
		connections = []Connection{}
	}
	return connections, nil
}

// Get retrieves a single notification connection by ID, returning nil if not found.
func (s *Store) Get(ctx context.Context, id int64) (*Connection, error) {
	var c Connection
	err := s.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		row := q.QueryRowContext(ctx, `
			SELECT id, name, provider, config_json, on_added, on_review, on_error, on_complete, on_update, created_at, updated_at
			FROM notification_connections
			WHERE id = ?;
		`, id)
		return row.Scan(
			&c.ID, &c.Name, &c.Provider, &c.ConfigJSON,
			&c.OnAdded, &c.OnReview, &c.OnError, &c.OnComplete, &c.OnUpdate,
			&c.CreatedAt, &c.UpdatedAt,
		)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get notification connection %d: %w", id, err)
	}
	return &c, nil
}

// Insert persists a new notification connection and returns the generated ID.
func (s *Store) Insert(ctx context.Context, c Connection) (int64, error) {
	var id int64
	err := s.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO notification_connections (
				name, provider, config_json, on_added, on_review, on_error, on_complete, on_update, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
		`, c.Name, c.Provider, c.ConfigJSON, c.OnAdded, c.OnReview, c.OnError, c.OnComplete, c.OnUpdate)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("failed to insert notification connection: %w", err)
	}
	return id, nil
}

// Update modifies an existing notification connection in the database.
func (s *Store) Update(ctx context.Context, c Connection) error {
	err := s.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE notification_connections
			SET name = ?, provider = ?, config_json = ?,
			    on_added = ?, on_review = ?, on_error = ?, on_complete = ?, on_update = ?,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = ?;
		`, c.Name, c.Provider, c.ConfigJSON, c.OnAdded, c.OnReview, c.OnError, c.OnComplete, c.OnUpdate, c.ID)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update notification connection %d: %w", c.ID, err)
	}
	return nil
}

// Delete removes a notification connection from the database.
func (s *Store) Delete(ctx context.Context, id int64) error {
	err := s.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			DELETE FROM notification_connections
			WHERE id = ?;
		`, id)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to delete notification connection %d: %w", id, err)
	}
	return nil
}

package notification_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func setupTestDB(t *testing.T) *storage.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_notifications.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open storage db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func TestStore_CRUD(t *testing.T) {
	db := setupTestDB(t)
	store := notification.NewStore(db)
	ctx := context.Background()

	// 1. Initially empty
	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("failed to list connections: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 connections, got %d", len(list))
	}

	// 2. Insert connection
	newConn := notification.Connection{
		Name:       "Discord Alerts",
		Provider:   "DISCORD",
		ConfigJSON: `{"url":"https://discord.com/api/webhooks/123/abc"}`,
		OnAdded:    true,
		OnReview:   true,
		OnError:    true,
		OnComplete: false,
		OnUpdate:   true,
	}

	id, err := store.Insert(ctx, newConn)
	if err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive ID, got %d", id)
	}

	// 3. Get connection
	saved, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("failed to get connection %d: %v", id, err)
	}
	if saved == nil {
		t.Fatalf("expected connection, got nil")
	}
	if saved.Name != "Discord Alerts" || saved.Provider != "DISCORD" {
		t.Fatalf("unexpected fields in saved connection: %+v", saved)
	}
	if !saved.OnAdded || saved.OnComplete {
		t.Fatalf("unexpected event flags: %+v", saved)
	}

	// 4. Update connection
	saved.Name = "Discord Channel General"
	saved.OnComplete = true
	if err := store.Update(ctx, *saved); err != nil {
		t.Fatalf("failed to update connection: %v", err)
	}

	updated, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("failed to get updated connection: %v", err)
	}
	if updated.Name != "Discord Channel General" {
		t.Fatalf("expected name %q, got %q", "Discord Channel General", updated.Name)
	}
	if !updated.OnComplete {
		t.Fatalf("expected OnComplete=true")
	}

	// 5. List connections
	list, err = store.List(ctx)
	if err != nil {
		t.Fatalf("failed to list connections: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(list))
	}

	// 6. Delete connection
	if err := store.Delete(ctx, id); err != nil {
		t.Fatalf("failed to delete connection: %v", err)
	}

	deleted, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("unexpected error getting deleted connection: %v", err)
	}
	if deleted != nil {
		t.Fatalf("expected nil for deleted connection, got %+v", deleted)
	}
}

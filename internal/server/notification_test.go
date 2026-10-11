package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
)

func TestNotificationEndpoints(t *testing.T) {
	db := newTestStorage(t)
	cfg := config.NewDefault()
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "test-key"

	srv := NewServer(cfg, "", db, nil, nil)
	handler := srv.Handler()

	// Mock webhook receiver
	var receivedCount int
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		receivedCount++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	// 1. Initial GET /api/v1/notifications should return empty list
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Api-Key", "test-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from GET /api/v1/notifications, got %d: %s", rec.Code, rec.Body.String())
	}
	var list []notification.Connection
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode list JSON: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 connections, got %d", len(list))
	}

	// 2. POST /api/v1/notifications creates a new connection
	createBody, _ := json.Marshal(map[string]any{
		"name":       "Discord Channel",
		"provider":   "DISCORD",
		"configJson": fmt.Sprintf(`{"url":%q}`, mockServer.URL),
		"onAdded":    true,
		"onReview":   true,
		"onError":    true,
		"onComplete": false,
		"onUpdate":   true,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(createBody))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "test-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("expected created status, got %d: %s", rec.Code, rec.Body.String())
	}
	var created notification.Connection
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode created connection: %v", err)
	}
	if created.ID <= 0 || created.Name != "Discord Channel" {
		t.Fatalf("unexpected created connection: %+v", created)
	}

	// 3. Test alert endpoint directly with payload: POST /api/v1/notifications/test
	testBody, _ := json.Marshal(map[string]any{
		"provider":   "DISCORD",
		"configJson": fmt.Sprintf(`{"url":%q}`, mockServer.URL),
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/notifications/test", bytes.NewReader(testBody))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "test-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from test endpoint, got %d: %s", rec.Code, rec.Body.String())
	}

	// 4. Test alert endpoint on saved connection: POST /api/v1/notifications/{id}/test
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/notifications/%d/test", created.ID), nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Api-Key", "test-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from test saved connection endpoint, got %d: %s", rec.Code, rec.Body.String())
	}

	// 5. PUT /api/v1/notifications/{id} updates connection
	updateBody, _ := json.Marshal(map[string]any{
		"name":       "Discord Updated",
		"provider":   "DISCORD",
		"configJson": fmt.Sprintf(`{"url":%q}`, mockServer.URL),
		"onAdded":    true,
		"onReview":   false,
		"onError":    true,
		"onComplete": true,
		"onUpdate":   false,
	})
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/notifications/%d", created.ID), bytes.NewReader(updateBody))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "test-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from update connection, got %d: %s", rec.Code, rec.Body.String())
	}

	// 6. DELETE /api/v1/notifications/{id} deletes connection
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/notifications/%d", created.ID), nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Api-Key", "test-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("expected 200/204 from delete connection, got %d: %s", rec.Code, rec.Body.String())
	}

	// 7. DELETE /api/v1/notifications/{id} for unknown ID returns 404
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/notifications/99999", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Api-Key", "test-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found from deleting nonexistent connection, got %d: %s", rec.Code, rec.Body.String())
	}

	// 8. Verify empty list
	req = httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Api-Key", "test-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	list = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("expected 0 connections after deletion, got %d", len(list))
	}
}

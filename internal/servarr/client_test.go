package servarr_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

func TestClient_AuthenticationHeader(t *testing.T) {
	var receivedKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client, err := servarr.NewClient(srv.URL, "secret-api-key")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	tags, err := client.GetTags(context.Background())
	if err != nil {
		t.Fatalf("GetTags failed: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected 0 tags, got %d", len(tags))
	}

	if receivedKey != "secret-api-key" {
		t.Fatalf("expected X-Api-Key 'secret-api-key', got %q", receivedKey)
	}
}

func TestClient_EnsureTag(t *testing.T) {
	t.Run("returns existing tag id when present", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]servarr.Tag{
					{ID: 1, Label: "existing"},
					{ID: 42, Label: "anilist-sync"},
				})
				return
			}
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}))
		defer srv.Close()

		client, err := servarr.NewClient(srv.URL, "key")
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		id, err := client.EnsureTag(context.Background(), "anilist-sync")
		if err != nil {
			t.Fatalf("EnsureTag failed: %v", err)
		}
		if id != 42 {
			t.Fatalf("expected tag id 42, got %d", id)
		}
	})

	t.Run("creates new tag when absent", func(t *testing.T) {
		var createdTag servarr.Tag
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]servarr.Tag{
					{ID: 1, Label: "other"},
				})
			case r.Method == http.MethodPost && r.URL.Path == "/api/v3/tag":
				_ = json.NewDecoder(r.Body).Decode(&createdTag)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(servarr.Tag{
					ID:    99,
					Label: createdTag.Label,
				})
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				http.Error(w, "unexpected request", http.StatusBadRequest)
			}
		}))
		defer srv.Close()

		client, err := servarr.NewClient(srv.URL, "key")
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		id, err := client.EnsureTag(context.Background(), "anilist-sync")
		if err != nil {
			t.Fatalf("EnsureTag failed: %v", err)
		}
		if id != 99 {
			t.Fatalf("expected tag id 99, got %d", id)
		}
		if createdTag.Label != "anilist-sync" {
			t.Fatalf("expected created label 'anilist-sync', got %q", createdTag.Label)
		}
	})
}

func TestClient_WithTimeout_DoesNotMutateCallerClient(t *testing.T) {
	callerClient := &http.Client{Timeout: 5 * time.Second}
	_, err := servarr.NewClient("http://localhost:8989", "key",
		servarr.WithHTTPClient(callerClient),
		servarr.WithTimeout(15*time.Second),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if callerClient.Timeout != 5*time.Second {
		t.Fatalf("expected caller client timeout to remain 5s, got %v", callerClient.Timeout)
	}
}

func TestClient_WithTimeout_OrderIndependence(t *testing.T) {
	callerClient := &http.Client{Timeout: 5 * time.Second}
	// WithTimeout passed before WithHTTPClient
	client, err := servarr.NewClient("http://localhost:8989", "key",
		servarr.WithTimeout(15*time.Second),
		servarr.WithHTTPClient(callerClient),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if callerClient.Timeout != 5*time.Second {
		t.Fatalf("expected caller client timeout to remain 5s, got %v", callerClient.Timeout)
	}
	_ = client
}

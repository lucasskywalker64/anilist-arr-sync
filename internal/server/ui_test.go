package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func newTestServerWithUI(t *testing.T, urlBase string, mockFS fstest.MapFS) *Server {
	t.Helper()
	cfg := &config.Config{
		Port:                   7171,
		URLBase:                urlBase,
		AuthenticationMethod:   "None",
		AuthenticationRequired: "Disabled",
		APIKey:                 "test-api-key",
	}
	s := NewServer(cfg, "", nil, nil, nil)
	s.SetWebFS(mockFS)
	return s
}

func TestEmbeddedSPA_DeliveryAndRouting(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!DOCTYPE html><html><head><title>AniList Arr Sync</title></head><body><div id=\"app\"></div></body></html>"),
		},
		"assets/bundle.js": &fstest.MapFile{
			Data: []byte("console.log('spa bundle loaded');"),
		},
		"assets/style.css": &fstest.MapFile{
			Data: []byte("body { background: #0b0f19; }"),
		},
		"favicon.svg": &fstest.MapFile{
			Data: []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"),
		},
	}

	s := newTestServerWithUI(t, "", mockFS)
	handler := s.Handler()

	t.Run("Root path serves index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		contentType := rec.Header().Get("Content-Type")
		if !strings.HasPrefix(contentType, "text/html") {
			t.Errorf("expected Content-Type text/html, got %s", contentType)
		}
		if !strings.Contains(rec.Body.String(), "<div id=\"app\">") {
			t.Errorf("expected body to contain app div, got %s", rec.Body.String())
		}
	})

	t.Run("Static asset delivers file directly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/bundle.js", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		body, _ := io.ReadAll(rec.Body)
		if string(body) != "console.log('spa bundle loaded');" {
			t.Errorf("unexpected body content: %s", string(body))
		}
	})

	t.Run("Static CSS delivers with text/css content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/style.css", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		contentType := rec.Header().Get("Content-Type")
		if !strings.HasPrefix(contentType, "text/css") {
			t.Errorf("expected text/css, got %s", contentType)
		}
	})

	t.Run("Client-side route falls back to index.html", func(t *testing.T) {
		routes := []string{"/review", "/sync", "/overrides", "/settings", "/review/42"}
		for _, route := range routes {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("route %s: expected status 200, got %d", route, rec.Code)
			}
			contentType := rec.Header().Get("Content-Type")
			if !strings.HasPrefix(contentType, "text/html") {
				t.Errorf("route %s: expected Content-Type text/html, got %s", route, contentType)
			}
			if !strings.Contains(rec.Body.String(), "<div id=\"app\">") {
				t.Errorf("route %s: expected body to contain app div", route)
			}
		}
	})

	t.Run("Non-existent API route returns 404, not index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
		req.Header.Set("X-Api-Key", "test-api-key")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "<div id=\"app\">") {
			t.Errorf("API 404 should never return index.html")
		}
	})
}

func TestEmbeddedSPA_WithURLBasePrefix(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!DOCTYPE html><html><body><div id=\"app\">Base SPA</div></body></html>"),
		},
		"assets/bundle.js": &fstest.MapFile{
			Data: []byte("console.log('prefixed');"),
		},
	}

	s := newTestServerWithUI(t, "/anilist-sync", mockFS)
	handler := s.Handler()

	t.Run("Redirect from bare prefix to slash prefix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/anilist-sync", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("expected status 301, got %d", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if loc != "/anilist-sync/" {
			t.Errorf("expected Location /anilist-sync/, got %s", loc)
		}
	})

	t.Run("Prefixed root path serves index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/anilist-sync/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Base SPA") {
			t.Errorf("expected body to contain Base SPA, got %s", rec.Body.String())
		}
	})

	t.Run("Prefixed client-side route falls back to index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/anilist-sync/review", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Base SPA") {
			t.Errorf("expected body to contain Base SPA, got %s", rec.Body.String())
		}
	})

	t.Run("Prefixed static asset delivers asset", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/anilist-sync/assets/bundle.js", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "prefixed") {
			t.Errorf("expected asset content, got %s", rec.Body.String())
		}
	})
}

func TestEmbeddedSPA_DefaultWebDistDelivery(t *testing.T) {
	cfg := &config.Config{
		Port:                   7171,
		AuthenticationMethod:   "None",
		AuthenticationRequired: "Disabled",
		APIKey:                 "test-api-key",
	}
	s := NewServer(cfg, "", nil, nil, nil)
	handler := s.Handler()

	t.Run("Root path serves compiled index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "AniList Arr Sync") {
			t.Errorf("expected body to contain AniList Arr Sync title, got %s", body)
		}
		if !strings.Contains(body, "<div id=\"app\">") {
			t.Errorf("expected body to contain div id=app, got %s", body)
		}
	})

	t.Run("Client-side fallback routes to compiled index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/review", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "AniList Arr Sync") {
			t.Errorf("expected body to contain title")
		}
	})
}

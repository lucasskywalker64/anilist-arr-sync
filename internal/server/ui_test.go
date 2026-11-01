package server

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/web"
)

func newTestServerWithUI(t *testing.T, urlBase string, mockFS fstest.MapFS, authMethod string) *Server {
	t.Helper()
	cfg := &config.Config{
		Port:                   7171,
		URLBase:                urlBase,
		AuthenticationMethod:   authMethod,
		AuthenticationRequired: "Disabled",
		APIKey:                 "test-api-key",
	}
	if authMethod == "Forms" {
		cfg.AuthenticationRequired = "Enabled"
	}
	s := NewServer(cfg, "", nil, nil, nil)
	if mockFS != nil {
		s.SetWebFS(mockFS)
	}
	return s
}

func TestEmbeddedSPA_DeliveryAndRouting(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!DOCTYPE html><html><head><title>AniList Arr Sync</title><!-- ANILIST_ARR_SYNC_CONFIG --></head><body><div id=\"app\"></div></body></html>"),
		},
		"assets/bundle.js": &fstest.MapFile{
			Data: []byte("console.log('spa bundle loaded');"),
		},
		"assets/style.css": &fstest.MapFile{
			Data: []byte("body { background: #202020; }"),
		},
		"favicon.svg": &fstest.MapFile{
			Data: []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"),
		},
	}

	s := newTestServerWithUI(t, "", mockFS, "None")
	handler := s.Handler()

	t.Run("Root path serves index.html with bootstrap config", func(t *testing.T) {
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
		body := rec.Body.String()
		if !strings.Contains(body, "<div id=\"app\">") {
			t.Errorf("expected body to contain app div, got %s", body)
		}
		if !strings.Contains(body, "AniListArrSync") {
			t.Errorf("expected body to contain AniListArrSync bootstrap script, got %s", body)
		}
		if !strings.Contains(body, "test-api-key") {
			t.Errorf("expected body to contain apiKey in AniListArrSync bootstrap script, got %s", body)
		}
	})

	t.Run("Static JS delivers file directly with cache header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/bundle.js", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		cacheControl := rec.Header().Get("Cache-Control")
		if !strings.Contains(cacheControl, "public") || !strings.Contains(cacheControl, "immutable") {
			t.Errorf("expected immutable cache header for assets, got %s", cacheControl)
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
		routes := []string{"/library", "/staged", "/overrides", "/activity/review", "/activity/history", "/activity/blocklist", "/activity/ignored", "/settings/general", "/settings/sync", "/settings/servarr", "/settings/connect", "/system/status", "/system/backup", "/system/updates"}
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
			if !strings.Contains(rec.Body.String(), "<base href=\"/\" />") {
				t.Errorf("route %s: expected body to contain base tag", route)
			}
		}
	})

	t.Run("Nested path asset request resolves static asset", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/activity/assets/bundle.js", nil)
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

	t.Run("Missing static asset returns 404, never index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "<div id=\"app\">") {
			t.Errorf("missing asset should return 404, not index.html")
		}
	})

	t.Run("Non-existent API route returns 404 JSON, not index.html", func(t *testing.T) {
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

	t.Run("Unauthenticated user can access static assets and client routes under Forms auth", func(t *testing.T) {
		sAuth := newTestServerWithUI(t, "", mockFS, "Forms")
		authHandler := sAuth.Handler()

		// Static assets
		reqAsset := httptest.NewRequest(http.MethodGet, "/assets/bundle.js", nil)
		recAsset := httptest.NewRecorder()
		authHandler.ServeHTTP(recAsset, reqAsset)
		if recAsset.Code != http.StatusOK {
			t.Fatalf("expected 200 for static asset under Forms auth, got %d", recAsset.Code)
		}

		// Client route fallback
		reqRoute := httptest.NewRequest(http.MethodGet, "/activity/review", nil)
		recRoute := httptest.NewRecorder()
		authHandler.ServeHTTP(recRoute, reqRoute)
		if recRoute.Code != http.StatusOK {
			t.Fatalf("expected 200 for client route under Forms auth, got %d", recRoute.Code)
		}

		// API route requires auth and returns 401
		reqAPI := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
		recAPI := httptest.NewRecorder()
		authHandler.ServeHTTP(recAPI, reqAPI)
		if recAPI.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for protected API under Forms auth, got %d", recAPI.Code)
		}
	})
}

func TestEmbeddedSPA_WithURLBasePrefix(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!DOCTYPE html><html><head><title>AniList Arr Sync</title></head><body><div id=\"app\">Base SPA</div></body></html>"),
		},
		"assets/bundle.js": &fstest.MapFile{
			Data: []byte("console.log('prefixed');"),
		},
	}

	s := newTestServerWithUI(t, "/anilist-sync", mockFS, "None")
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

	t.Run("Prefixed root path serves index.html with urlBase in bootstrap", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/anilist-sync/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Base SPA") {
			t.Errorf("expected body to contain Base SPA, got %s", body)
		}
		if !strings.Contains(body, `"/anilist-sync"`) {
			t.Errorf("expected body to contain urlBase in bootstrap config, got %s", body)
		}
	})

	t.Run("Prefixed client-side route falls back to index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/anilist-sync/activity/review", nil)
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

func TestEmbeddedSPA_SonarrThemeTokenVerification(t *testing.T) {
	dist, err := web.Dist()
	if err != nil {
		t.Fatalf("failed to open web dist: %v", err)
	}

	// Verify required Sonarr dark theme CSS tokens and dimensions in theme.css or built CSS assets
	requiredTokens := []string{
		"#202020", // pageBackground
		"#2a2a2a", // pageHeaderBackgroundColor / sidebarBackgroundColor / cardCenterBackgroundColor
		"#262626", // toolbarBackgroundColor
		"#35c5f4", // sonarrBlue / themeBlue
		"#2193b5", // themeAlternateBlue
		"#5d9cec", // primaryColor / infoColor
		"#dde6e9", // inputBorderColor
		"#333333", // cardBackgroundColor / sidebarActiveBackgroundColor
		"#909293", // helpTextColor
		"210px",   // sidebarWidth
		"60px",    // headerHeight / toolbarHeight / toolbarButtonWidth
		"250px",   // formLabelLargeWidth
		"35px",    // input height
	}

	// Look in dist for CSS files
	var combinedCSS bytes.Buffer
	var foundCSS bool
	_ = fs.WalkDir(dist, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".css") {
			if f, openErr := dist.Open(path); openErr == nil {
				defer func() { _ = f.Close() }()
				data, _ := io.ReadAll(f)
				combinedCSS.Write(data)
				combinedCSS.WriteByte('\n')
				foundCSS = true
			}
		}
		return nil
	})

	// If no CSS files in dist yet (before frontend build), this check will be run once built.
	if !foundCSS {
		t.Skip("no CSS files found in dist yet (run npm run build first)")
	}

	cssContent := combinedCSS.String()
	for _, token := range requiredTokens {
		if !strings.Contains(cssContent, token) {
			t.Errorf("expected Sonarr dark theme token %q in CSS, but was not found", token)
		}
	}
}

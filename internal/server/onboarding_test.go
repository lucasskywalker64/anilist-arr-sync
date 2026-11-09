package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func onboardingRequest(s *Server, method, path, payload, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(payload))
	req.Header.Set("X-Api-Key", key)
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	return res
}

func TestOnboardingConnectionTests(t *testing.T) {
	cfg := config.NewDefault()
	cfg.APIKey = "admin-key"
	cfg.SonarrURL, cfg.SonarrAPIKey = "http://sonarr/arr", "saved-key"
	srv := NewServer(cfg, "", nil, nil, nil)
	var requests []string
	srv.SetOutboundHTTPClient(&http.Client{Transport: syncTransportFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Method+" "+req.URL.Path)
		status, body := http.StatusOK, ""
		if req.URL.Host == "graphql.anilist.co" {
			var query struct {
				Variables map[string]string
			}
			if err := json.NewDecoder(req.Body).Decode(&query); err != nil {
				t.Fatal(err)
			}
			switch query.Variables["username"] {
			case "viewer":
				body = `{"data":{"User":{"name":"Viewer","avatar":{"large":"https://avatar.example/viewer.jpg"},"statistics":{"anime":{"count":42},"manga":{"count":5}}}}}`
			case "missing":
				status, body = http.StatusNotFound, `{"errors":[{"message":"Not Found.","status":404}]}`
			default:
				t.Fatalf("unexpected AniList username: %v", query.Variables)
			}
		} else if key := req.Header.Get("X-Api-Key"); key != "unsaved-key" && key != "saved-key" {
			status, body = http.StatusUnauthorized, `{"message":"invalid key"}`
		} else {
			switch strings.TrimPrefix(req.URL.Path, "/arr") {
			case "/api/v3/system/status":
				body = `{"appName":"` + req.URL.Host + `"}`
			case "/api/v3/qualityprofile":
				body = `[{"id":7,"name":"HD"}]`
			case "/api/v3/rootfolder":
				body = `[{"id":2,"path":"/media"}]`
			default:
				t.Fatalf("unexpected Servarr path %s", req.URL.Path)
			}
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	for _, tc := range []struct {
		name, endpoint, payload, apiKey, contains string
		status                                    int
		calls                                     int
	}{
		{"AniList profile", "anilist", `{"username":" viewer "}`, cfg.APIKey, `"animeCount":42`, 200, 1},
		{"AniList missing", "anilist", `{"username":"missing"}`, cfg.APIKey, `not found`, 404, 1},
		{"AniList empty", "anilist", `{"username":" "}`, cfg.APIKey, `required`, 400, 0},
		{"AniList unauthenticated", "anilist", `{"username":"viewer"}`, "", `unauthorized`, 401, 0},
		{"Sonarr subpath", "servarr", `{"type":"sonarr","baseUrl":"http://sonarr/arr","apiKey":"unsaved-key"}`, cfg.APIKey, `"qualityProfiles":[{"id":7,"name":"HD"}],"rootFolders":[{"id":2,"path":"/media"}]`, 200, 3},
		{"Radarr", "servarr", `{"type":"radarr","baseUrl":"http://radarr","apiKey":"unsaved-key"}`, cfg.APIKey, `"rootFolders":[{"id":2,"path":"/media"}]`, 200, 3},
		{"Wrong service", "servarr", `{"type":"radarr","baseUrl":"http://sonarr","apiKey":"unsaved-key"}`, cfg.APIKey, `Radarr`, 400, 1},
		{"Invalid key", "servarr", `{"type":"sonarr","baseUrl":"http://sonarr","apiKey":"wrong"}`, cfg.APIKey, `401`, 400, 1},
		{"Saved key", "servarr", `{"type":"sonarr","baseUrl":"http://sonarr/arr/","apiKey":""}`, cfg.APIKey, `"qualityProfiles"`, 200, 3},
		{"Saved key with different URL", "servarr", `{"type":"sonarr","baseUrl":"http://radarr","apiKey":""}`, cfg.APIKey, `required`, 400, 0},
		{"Servarr unauthenticated", "servarr", `{"type":"sonarr","baseUrl":"http://sonarr","apiKey":"unsaved-key"}`, "", `unauthorized`, 401, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests = nil
			res := onboardingRequest(srv, "POST", "/api/v1/"+tc.endpoint+"/test", tc.payload, tc.apiKey)
			if res.Code != tc.status || !strings.Contains(res.Body.String(), tc.contains) || len(requests) != tc.calls {
				t.Fatalf("got %d %s, outbound requests %v", res.Code, res.Body, requests)
			}
		})
	}
	if cfg.AniListUsername != "" || cfg.SonarrAPIKey != "saved-key" || cfg.RadarrURL != "" {
		t.Fatal("pre-flight tests changed config")
	}
}

func TestOnboardingConfig(t *testing.T) {
	cfg := config.NewDefault()
	cfg.APIKey = "admin-key"
	path := filepath.Join(t.TempDir(), "config.xml")
	srv := NewServer(cfg, path, nil, nil, nil)
	assertState := func(required, completed bool) {
		t.Helper()
		res := onboardingRequest(srv, "GET", "/api/v1/auth/user", "", cfg.APIKey)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || res.Code != http.StatusOK {
			t.Fatalf("auth/user: %d %s", res.Code, res.Body)
		}
		for field, expected := range map[string]bool{"onboardingRequired": required, "wizardCompleted": completed} {
			var actual bool
			if err := json.Unmarshal(body[field], &actual); err != nil || actual != expected {
				t.Fatalf("%s: got %s, want %t", field, body[field], expected)
			}
		}
	}
	assertState(true, false)
	update := func(payload string) *httptest.ResponseRecorder {
		return onboardingRequest(srv, "PUT", "/api/v1/config", payload, cfg.APIKey)
	}
	if res := update(`{"aniListUsername":"viewer"}`); res.Code != http.StatusOK {
		t.Fatalf("partial configuration: %d %s", res.Code, res.Body)
	}
	assertState(true, false)
	if res := update(`{"sonarrUrl":"http://sonarr:8989"}`); res.Code != http.StatusOK {
		t.Fatalf("legacy configuration: %d %s", res.Code, res.Body)
	}
	assertState(false, false)
	if res := update(`{"wizardCompleted":true}`); res.Code != http.StatusBadRequest {
		t.Fatalf("incomplete setup: %d %s", res.Code, res.Body)
	}
	for _, invalidHours := range []string{"0", "3.5"} {
		if res := update(`{"wizardCompleted":true,"sonarrApiKey":"sonarr-secret","sonarrRootFolderPath":"/anime","syncIntervalHours":` + invalidHours + `}`); res.Code != http.StatusBadRequest {
			t.Fatalf("invalid completion with interval %s: %d %s", invalidHours, res.Code, res.Body)
		}
	}
	assertState(false, false)
	const setup = `{
		"wizardCompleted":true,"sonarrApiKey":"sonarr-secret","sonarrQualityProfileId":7,"sonarrRootFolderPath":"/anime","sonarrMonitorOnAdd":false,
		"radarrUrl":"http://radarr:7878","radarrApiKey":"radarr-secret","radarrQualityProfileId":11,"radarrRootFolderPath":"/movies","radarrMonitorOnAdd":false,
		"sonarrSearchOnAdd":true,"radarrSearchOnAdd":true,"syncIntervalHours":8,"firstRunDryRun":true
	}`
	for _, key := range []string{"SYNC__FIRSTRUNDRYRUN", "SONARR__SEARCHONADD", "RADARR__SEARCHONADD"} {
		t.Run("conflicting environment override/"+key, func(t *testing.T) {
			t.Setenv("ANILIST_SYNC__"+key, "false")
			if res := update(setup); res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "environment variables override") {
				t.Fatalf("setup under a conflicting environment override: %d %s", res.Code, res.Body)
			}
		})
	}
	assertState(false, false)
	res := update(setup)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"restartRequired":false`) {
		t.Fatalf("finish setup: %d %s", res.Code, res.Body)
	}
	assertState(false, true)
	saved, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.WizardCompleted || saved.SonarrQualityProfileID != 7 || saved.SonarrRootFolderPath != "/anime" || saved.SonarrMonitorOnAdd ||
		saved.RadarrMonitorOnAdd || !saved.SonarrSearchOnAdd || !saved.RadarrSearchOnAdd || saved.SyncIntervalHours != 8 {
		t.Fatalf("onboarding fields did not persist")
	}
	loaded := onboardingRequest(srv, "GET", "/api/v1/config", "", cfg.APIKey)
	if !strings.Contains(loaded.Body.String(), `"radarrMonitorOnAdd":false`) {
		t.Fatalf("GET /config did not return the saved Radarr monitor preference: %s", loaded.Body)
	}
	if !strings.Contains(loaded.Body.String(), `"sonarrApiKey":"******"`) || strings.Contains(loaded.Body.String(), "sonarr-secret") {
		t.Fatalf("GET /config did not mask the Sonarr key: %s", loaded.Body)
	}
}

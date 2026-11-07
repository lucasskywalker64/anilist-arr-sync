package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

func TestOnboardingActivatesSavedConnectionsAndScheduleAfterRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		paths := runtimePaths{data: dir, config: filepath.Join(dir, "config.xml")}
		cfg := config.NewDefault()
		cfg.APIKey = "runtime-key"
		if err := config.Save(paths.config, cfg); err != nil {
			t.Fatal(err)
		}
		boot := func() *appRuntime {
			t.Helper()
			r, err := bootstrap(t.Context(), options{}, paths)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		request := func(r *appRuntime, method, path, body string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("X-Api-Key", cfg.APIKey)
			res := httptest.NewRecorder()
			r.server.Handler().ServeHTTP(res, req)
			return res
		}

		first := boot()
		saved := request(first, "PUT", "/api/v1/config", `{
			"wizardCompleted":true,"aniListUsername":"viewer","sonarrUrl":"http://sonarr/arr",
			"sonarrApiKey":"sonarr-secret","sonarrQualityProfileId":7,"sonarrRootFolderPath":"/anime",
			"sonarrMonitorOnAdd":false,"firstRunDryRun":false,"syncIntervalHours":2
		}`)
		pending := request(first, "POST", "/api/v1/sync/start?dryRun=true", "")
		if err := first.close(); err != nil {
			t.Fatal(err)
		}
		if saved.Code != 200 || !strings.Contains(saved.Body.String(), `"restartRequired":true`) || pending.Code != 409 {
			t.Fatalf("save: %d %s, dry run before restart: %d %s", saved.Code, saved.Body, pending.Code, pending.Body)
		}

		r := boot()
		defer func() {
			if err := r.close(); err != nil {
				t.Error(err)
			}
		}()
		var writes atomic.Int32
		r.client.Transport = runtimeTransportFunc(func(req *http.Request) (*http.Response, error) {
			body := `[]`
			switch req.URL.Host {
			case "graphql.anilist.co":
				payload, err := io.ReadAll(req.Body)
				if err != nil || !strings.Contains(string(payload), `"viewer"`) {
					t.Errorf("saved AniList username missing from request: %v", err)
				}
				body = `{"data":{"MediaListCollection":{"lists":[{"status":"CURRENT","entries":[{"id":1,"mediaId":1,"status":"CURRENT","media":{"id":1,"format":"TV","status":"FINISHED","title":{"romaji":"Example Anime"}}}]}]}}}`
			case "sonarr":
				if req.Header.Get("X-Api-Key") != "sonarr-secret" || !strings.HasPrefix(req.URL.Path, "/arr/api/v3/") {
					t.Error("Sonarr did not use the saved key and URL base")
				}
				if req.Method == "POST" && req.URL.Path == "/arr/api/v3/series" {
					writes.Add(1)
					body = `{"id":10}`
				} else if req.URL.Path == "/arr/api/v3/tag" {
					body = `[{"id":1,"label":"anilist-sync"}]`
				}
			default:
				time.Sleep(time.Second) // Allow the dry-run request to overlap the startup mapping refresh.
				body = `[{"anilist_id":1,"tvdb_id":2,"type":"TV","season":1}]`
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		r.start()
		synctest.Wait()
		if got := time.Until(r.scheduler.Status().Sync.Next); got != 2*time.Hour {
			t.Fatalf("scheduled interval after restart = %s, want 2h", got)
		}
		if started := request(r, "POST", "/api/v1/sync/start?dryRun=true", ""); started.Code != 202 {
			t.Fatalf("initial dry run: %d %s", started.Code, started.Body)
		}
		time.Sleep(3 * time.Second)
		synctest.Wait()
		staged := request(r, "GET", "/api/v1/staged", "")
		var actions []orchestrator.StagedAction
		if err := json.Unmarshal(staged.Body.Bytes(), &actions); err != nil || len(actions) != 1 || writes.Load() != 0 {
			status := request(r, "GET", "/api/v1/sync/status", "")
			t.Fatalf("dry run did not stage exactly one action without a Sonarr write: %s, err=%v, writes=%d, sync=%s", staged.Body, err, writes.Load(), status.Body)
		}
		var series servarr.AddSeriesRequest
		if err := json.Unmarshal([]byte(actions[0].PayloadJSON), &series); err != nil || series.QualityProfileID != 7 || series.RootFolderPath != "/anime" || series.Monitored {
			t.Fatalf("staged series did not use saved preferences: %s, err=%v", actions[0].PayloadJSON, err)
		}
		time.Sleep(time.Until(r.scheduler.Status().Sync.Next) + 3*time.Second)
		synctest.Wait()
		if writes.Load() != 1 {
			t.Fatal("scheduled sync did not use the saved non-dry-run preference")
		}
		if updated := request(r, "PUT", "/api/v1/config", `{"sonarrUrl":"http://next-sonarr/arr","firstRunDryRun":true}`); updated.Code != 200 {
			t.Fatalf("save new connection: %d %s", updated.Code, updated.Body)
		}
		time.Sleep(time.Until(r.scheduler.Status().Sync.Next) + 3*time.Second)
		synctest.Wait()
		if writes.Load() != 1 {
			t.Fatal("scheduled sync wrote to the old Sonarr connection while restart was pending")
		}
	})
}

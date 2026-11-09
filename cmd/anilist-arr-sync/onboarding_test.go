package main

import (
	"encoding/json"
	"fmt"
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

func TestOnboardingActivatesSavedSettingsWithoutRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		paths := runtimePaths{data: dir, config: filepath.Join(dir, "config.xml")}
		cfg := config.NewDefault()
		cfg.APIKey = "runtime-key"
		if err := config.Save(paths.config, cfg); err != nil {
			t.Fatal(err)
		}
		r, err := bootstrap(t.Context(), options{}, paths)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := r.close(); err != nil {
				t.Error(err)
			}
		}()
		request := func(method, path, body string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("X-Api-Key", cfg.APIKey)
			res := httptest.NewRecorder()
			r.server.Handler().ServeHTTP(res, req)
			return res
		}

		var mutations, seriesWrites, reloadedSeriesWrites atomic.Int32
		r.client.Transport = runtimeTransportFunc(func(req *http.Request) (*http.Response, error) {
			body := `[]`
			switch req.URL.Host {
			case "graphql.anilist.co":
				payload, err := io.ReadAll(req.Body)
				if err != nil || !strings.Contains(string(payload), `"viewer"`) {
					t.Errorf("saved AniList username missing from request: %v", err)
				}
				body = `{"data":{"MediaListCollection":{"lists":[{"status":"CURRENT","entries":[{"id":1,"mediaId":1,"status":"CURRENT","media":{"id":1,"format":"TV","status":"FINISHED","title":{"romaji":"Example Anime"}}}]}]}}}`
			case "sonarr", "next-sonarr":
				key := "sonarr-secret"
				if req.URL.Host == "next-sonarr" {
					key = "next-secret"
				}
				if req.Header.Get("X-Api-Key") != key || !strings.HasPrefix(req.URL.Path, "/arr/api/v3/") {
					t.Error("Sonarr did not use the saved key and URL base")
				}
				if req.Method != http.MethodGet {
					mutations.Add(1)
				}
				if req.Method == http.MethodPost && req.URL.Path == "/arr/api/v3/series" {
					if req.URL.Host == "next-sonarr" {
						reloadedSeriesWrites.Add(1)
					} else {
						seriesWrites.Add(1)
					}
					body = `{"id":10}`
				} else if req.Method == http.MethodPost && req.URL.Path == "/arr/api/v3/tag" {
					body = `{"id":1,"label":"anilist-sync"}`
				}
			default:
				time.Sleep(time.Second) // Allow the dry-run request to overlap the startup mapping refresh.
				body = `[{"anilist_id":1,"tvdb_id":2,"type":"TV","season":1}]`
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		r.start()
		synctest.Wait()
		if !r.scheduler.Status().Sync.Next.IsZero() {
			t.Fatal("sync was scheduled before setup")
		}
		saved := request("PUT", "/api/v1/config", `{
			"wizardCompleted":true,"aniListUsername":"viewer","sonarrUrl":"http://sonarr/arr",
			"sonarrApiKey":"sonarr-secret","sonarrQualityProfileId":7,"sonarrRootFolderPath":"/anime",
			"sonarrMonitorOnAdd":false,"firstRunDryRun":false,"syncIntervalHours":2
		}`)
		if saved.Code != 200 || !strings.Contains(saved.Body.String(), `"restartRequired":false`) {
			t.Fatalf("save: %d %s", saved.Code, saved.Body)
		}
		if got := time.Until(r.scheduler.Status().Sync.Next); got != 2*time.Hour {
			t.Fatalf("scheduled interval after save = %s, want 2h", got)
		}
		if started := request("POST", "/api/v1/sync/start?dryRun=true", ""); started.Code != 202 {
			t.Fatalf("initial dry run: %d %s", started.Code, started.Body)
		}
		time.Sleep(3 * time.Second)
		synctest.Wait()
		staged := request("GET", "/api/v1/staged", "")
		var actions []orchestrator.StagedAction
		if err := json.Unmarshal(staged.Body.Bytes(), &actions); err != nil || len(actions) != 1 || mutations.Load() != 0 {
			status := request("GET", "/api/v1/sync/status", "")
			t.Fatalf("dry run did not stage exactly one action without a Sonarr write: %s, err=%v, writes=%d, sync=%s", staged.Body, err, mutations.Load(), status.Body)
		}
		var series servarr.AddSeriesRequest
		if err := json.Unmarshal([]byte(actions[0].PayloadJSON), &series); err != nil || series.QualityProfileID != 7 || series.RootFolderPath != "/anime" || series.Monitored {
			t.Fatalf("staged series did not use saved preferences: %s, err=%v", actions[0].PayloadJSON, err)
		}
		time.Sleep(time.Until(r.scheduler.Status().Sync.Next) + 3*time.Second)
		synctest.Wait()
		if seriesWrites.Load() != 1 {
			t.Fatal("scheduled sync did not use the saved non-dry-run preference")
		}
		updated := request("PUT", "/api/v1/config", `{"sonarrUrl":"http://next-sonarr/arr","sonarrApiKey":"next-secret"}`)
		if updated.Code != 200 || !strings.Contains(updated.Body.String(), `"restartRequired":false`) {
			t.Fatalf("save new connection: %d %s", updated.Code, updated.Body)
		}
		applied := request("POST", "/api/v1/staged/apply", fmt.Sprintf(`{"ids":[%d]}`, actions[0].ID))
		if applied.Code != 200 || seriesWrites.Load() != 1 || reloadedSeriesWrites.Load() != 1 {
			t.Fatalf("staged apply after save: %d %s, old writes=%d, new writes=%d", applied.Code, applied.Body, seriesWrites.Load(), reloadedSeriesWrites.Load())
		}
		time.Sleep(time.Until(r.scheduler.Status().Sync.Next) + 3*time.Second)
		synctest.Wait()
		if seriesWrites.Load() != 1 || reloadedSeriesWrites.Load() != 2 {
			t.Fatalf("scheduled sync after save: old writes=%d, new writes=%d", seriesWrites.Load(), reloadedSeriesWrites.Load())
		}
		if removed := request("PUT", "/api/v1/config", `{"sonarrUrl":""}`); removed.Code != 200 || !r.scheduler.Status().Sync.Next.IsZero() {
			t.Fatalf("removing the last connection left sync scheduled: %d %s", removed.Code, removed.Body)
		}
	})
}

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

type runtimeTransportFunc func(*http.Request) (*http.Response, error)

func (f runtimeTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRuntimeScheduledSyncWaitsForMappings(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		cfg := config.NewDefault()
		cfg.APIKey, cfg.AniListUsername = "runtime-key", "user"
		cfg.SonarrURL, cfg.SonarrAPIKey = "http://sonarr.example", "key"
		cfg.SyncIntervalHours = 1
		path := filepath.Join(dir, "config.xml")
		if err := config.Save(path, cfg); err != nil {
			t.Fatal(err)
		}
		r, err := bootstrap(t.Context(), options{}, runtimePaths{data: dir, config: path})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := r.close(); err != nil {
				t.Error(err)
			}
		}()
		var available atomic.Bool
		var syncs atomic.Int32
		r.client.Transport = runtimeTransportFunc(func(req *http.Request) (*http.Response, error) {
			body := `[{"anilist_id":1,"tvdb_id":2,"type":"TV"}]`
			if req.Method == "POST" {
				syncs.Add(1)
				body = `{"data":{"MediaListCollection":{"lists":[],"hasNextChunk":false}}}`
			} else if !available.Load() {
				body = `[]`
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		r.start()
		req := httptest.NewRequest("POST", "/api/v1/sync/start", nil)
		req.Header.Set("X-Api-Key", cfg.APIKey)
		rec := httptest.NewRecorder()
		r.server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "could not prepare mappings") || syncs.Load() != 0 {
			t.Fatalf("manual sync without mappings: %d %s, syncs=%d", rec.Code, rec.Body, syncs.Load())
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if syncs.Load() != 0 || !r.scheduler.Status().Sync.Last.IsZero() {
			t.Fatal("scheduled sync ran without mappings")
		}
		if r.scheduler.Status().Backup.Next.IsZero() {
			t.Fatal("waiting for mappings disabled backup scheduling")
		}
		available.Store(true)
		time.Sleep(time.Hour)
		synctest.Wait()
		if syncs.Load() != 1 {
			t.Fatalf("syncs after mapping recovery: %d", syncs.Load())
		}
		available.Store(false)
		last := r.scheduler.Status().Sync.Last
		time.Sleep(time.Hour)
		synctest.Wait()
		if !r.scheduler.Status().Sync.Last.After(last) {
			t.Fatal("cached mappings did not permit scheduled sync")
		}
	})
}

func TestRuntimeShutdownDeadlineStillClosesStorage(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewDefault()
	cfg.APIKey, cfg.AniListUsername = "runtime-key", "user"
	cfg.SonarrURL, cfg.SonarrAPIKey = "http://sonarr.example", "key"
	path := filepath.Join(dir, "config.xml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	r, err := bootstrap(t.Context(), options{}, runtimePaths{data: dir, config: path})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := r.db.Reader(), r.db.Writer()
	entered, release := make(chan struct{}), make(chan struct{})
	r.client.Transport = runtimeTransportFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[{"anilist_id":1,"tvdb_id":2,"type":"TV"}]`))}, nil
		}
		close(entered)
		<-release
		return nil, req.Context().Err()
	})
	req := httptest.NewRequest("POST", "/api/v1/sync/start", nil)
	req.Header.Set("X-Api-Key", "runtime-key")
	rec := httptest.NewRecorder()
	r.server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.shutdown(ctx) }()
	<-ctx.Done()
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish cleanup after its deadline")
	}
	for _, pool := range []*sql.DB{reader, writer} {
		if err := pool.PingContext(t.Context()); err == nil {
			t.Error("deadline skipped storage cleanup")
		}
	}
}

func TestRuntimeRepeatedBootClosesPools(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewDefault()
	cfg.APIKey, cfg.BindAddress, cfg.Port = "runtime-key", "127.0.0.1", freePort(t)
	path := filepath.Join(dir, "config.xml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		r, err := bootstrap(t.Context(), options{}, runtimePaths{data: dir, config: path})
		if err != nil {
			t.Fatal(err)
		}
		reader, writer := r.db.Reader(), r.db.Writer()
		if err := r.server.Start(); err != nil {
			_ = r.close()
			t.Fatal(err)
		}
		if err := r.close(); err != nil {
			t.Fatal(err)
		}
		for _, pool := range []*sql.DB{reader, writer} {
			if err := pool.PingContext(t.Context()); err == nil {
				t.Fatalf("boot %d left a pool open", i)
			}
		}
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func apiCall(t *testing.T, client *http.Client, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", "runtime-key")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var payload map[string]any
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, payload
}

func waitBoot(t *testing.T, client *http.Client, base, previous string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := client.Get(base + "/api/v1/health")
		if err == nil {
			var payload struct {
				InstanceID string `json:"instanceId"`
			}
			_ = json.NewDecoder(res.Body).Decode(&payload)
			_ = res.Body.Close()
			if res.StatusCode == 200 && payload.InstanceID != "" && payload.InstanceID != previous {
				return payload.InstanceID
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("runtime did not become healthy")
	return ""
}

func TestRuntimeRepeatedRestartReloadsConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewDefault()
	cfg.Port = freePort(t)
	cfg.BindAddress = "127.0.0.1"
	cfg.APIKey = "runtime-key"
	cfg.BackupFolder = "custom-backups"
	cfg.BackupInterval = 3
	configPath := filepath.Join(dir, "config.xml")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"--data-dir", dir, "--no-browser"}, io.Discard) }()
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	base := fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
	id := waitBoot(t, client, base, "")
	for i := 0; i < 3; i++ {
		cfg.InstanceName = fmt.Sprintf("boot-%d", i)
		if err := config.Save(configPath, cfg); err != nil {
			t.Fatal(err)
		}
		nextBase := base
		if i == 1 {
			cfg.Port = freePort(t)
			cfg.URLBase = "/runtime"
			code, response := apiCall(t, client, "PUT", base+"/api/v1/config", fmt.Sprintf(`{"port":%d,"urlBase":"/runtime","instanceName":%q}`, cfg.Port, cfg.InstanceName))
			if code != 200 || response["restartRequired"] != true {
				t.Fatalf("listener changes must require restart: %+v", response)
			}
			nextBase = fmt.Sprintf("http://127.0.0.1:%d/runtime", cfg.Port)
		}
		code, response := apiCall(t, client, "POST", base+"/api/v1/system/restart", "")
		if code != 200 || response["instanceId"] != id {
			t.Fatalf("restart response: %d %+v", code, response)
		}
		if response["reconnectUrl"] != nextBase+"/" {
			t.Fatalf("reconnect URL: %+v", response)
		}
		base = nextBase
		id = waitBoot(t, client, base, id)
		code, response = apiCall(t, client, "GET", base+"/api/v1/config", "")
		if code != 200 || response["instanceName"] != cfg.InstanceName {
			t.Fatalf("configuration not reloaded: %+v", response)
		}
		_, response = apiCall(t, client, "GET", base+"/api/v1/sync/status", "")
		if response["running"] != false {
			t.Fatalf("unconfigured startup ran a sync: %+v", response)
		}
	}
	code, _ := apiCall(t, client, "POST", base+"/api/v1/backups", "")
	if code != 201 {
		t.Fatalf("backup endpoint: %d", code)
	}
	archives, err := filepath.Glob(filepath.Join(dir, "custom-backups", "*.zip"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("configured backup directory not used: %v %v", archives, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not stop on cancellation")
	}
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
	if err != nil {
		t.Fatalf("listener was not released: %v", err)
	}
	_ = l.Close()
	if err := os.Rename(filepath.Join(dir, "sync.db"), filepath.Join(dir, "closed.db")); err != nil {
		t.Fatalf("database still locked: %v", err)
	}
}

func TestOneShotSyncSummaryAndErrors(t *testing.T) {
	var fail atomic.Bool
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			_, _ = io.WriteString(w, `[{"anilist_id":1,"tvdb_id":2,"type":"TV"}]`)
			return
		}
		if fail.Load() {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"errors":[{"message":"bad username"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"MediaListCollection":{"lists":[],"hasNextChunk":false}}}`)
	}))
	defer fixture.Close()
	oldTransport := http.DefaultTransport
	transport := fixture.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, fixture.Listener.Addr().String())
	}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = oldTransport; transport.CloseIdleConnections() }()
	dir := t.TempDir()
	cfg := config.NewDefault()
	cfg.AniListUsername = "fixture-user"
	cfg.SonarrURL = "http://sonarr.example"
	cfg.SonarrAPIKey = "key"
	if err := config.Save(filepath.Join(dir, "config.xml"), cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(t.Context(), []string{"--data-dir", dir, "--sync"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "SUCCESS: scanned=0") {
		t.Fatalf("missing summary: %s", out.String())
	}
	fail.Store(true)
	out.Reset()
	if err := run(t.Context(), []string{"--data-dir", dir, "--sync"}, &out); err == nil {
		t.Fatal("failed AniList request must fail CLI sync")
	}
	if !strings.Contains(out.String(), "FAILED:") {
		t.Fatalf("missing failure summary: %s", out.String())
	}
	if err := run(t.Context(), []string{"--data-dir", t.TempDir(), "--sync"}, &out); err == nil {
		t.Fatal("unconfigured CLI sync must fail")
	}
}

func TestRuntimeBackupSettingsTakeEffectOnSave(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewDefault()
	cfg.APIKey = "runtime-key"
	cfg.BackupFolder = "backups"
	configPath := filepath.Join(t.TempDir(), "config.xml")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	r, err := bootstrap(t.Context(), options{}, runtimePaths{data: dir, config: configPath})
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
		rec := httptest.NewRecorder()
		r.server.Handler().ServeHTTP(rec, req)
		return rec
	}
	if err := os.WriteFile(filepath.Join(dir, "blocked"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if rec := request("PUT", "/api/v1/config", `{"backupFolder":"blocked"}`); rec.Code != http.StatusBadRequest || r.server.Config().BackupFolder != "backups" {
		t.Fatalf("invalid folder save: %d %s", rec.Code, rec.Body)
	}
	newDir := filepath.Join(dir, "new-backups")
	if err := os.Mkdir(newDir, 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, age := range []time.Duration{72 * time.Hour, time.Hour, 2 * time.Hour, 3 * time.Hour} {
		name := backup.FormatBackupFilename(backup.TypeManual, now.Add(-age))
		if err := os.WriteFile(filepath.Join(newDir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	expired := backup.FormatBackupFilename(backup.TypeManual, now.Add(-72*time.Hour))
	previousArchive := filepath.Join(dir, "backups", expired)
	if err := os.WriteFile(previousArchive, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{"backupFolder":"new-backups"}`, `{"backupRetention":2}`} {
		if rec := request("PUT", "/api/v1/config", payload); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"restartRequired":false`) {
			t.Fatalf("save %s: %d %s", payload, rec.Code, rec.Body)
		}
	}
	listed := request("GET", "/api/v1/backups", "")
	var backups []backup.Info
	if err := json.Unmarshal(listed.Body.Bytes(), &backups); err != nil || len(backups) != 4 {
		t.Fatalf("backups after saving: %d %s, err=%v", listed.Code, listed.Body, err)
	}
	if rec := request("POST", "/api/v1/backups", ""); rec.Code != http.StatusCreated {
		t.Fatalf("backup: %d %s", rec.Code, rec.Body.String())
	}
	archives, err := filepath.Glob(filepath.Join(newDir, "*.zip"))
	if err != nil || len(archives) != 4 {
		t.Fatalf("retention must keep all four recent archives and remove the old archive: %v, %v", archives, err)
	}
	if _, err := os.Stat(filepath.Join(newDir, expired)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired archive still exists in the active folder: %v", err)
	}
	if _, err := os.Stat(previousArchive); err != nil {
		t.Fatalf("archive in previous folder: %v", err)
	}
}

func TestRuntimeStopsActiveWebSyncBeforeClosingStorage(t *testing.T) {
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `[{"anilist_id":1,"tvdb_id":2,"type":"TV"}]`)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		select {
		case <-r.Context().Done():
			cancelled <- struct{}{}
		case <-time.After(10 * time.Second):
		}
	}))
	defer fixture.Close()
	oldTransport := http.DefaultTransport
	transport := fixture.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, fixture.Listener.Addr().String())
	}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = oldTransport; transport.CloseIdleConnections() }()
	dir := t.TempDir()
	cfg := config.NewDefault()
	cfg.APIKey, cfg.AniListUsername = "runtime-key", "user"
	cfg.SonarrURL, cfg.SonarrAPIKey = "http://sonarr.example", "key"
	path := filepath.Join(dir, "config.xml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	r, err := bootstrap(t.Context(), options{}, runtimePaths{data: dir, config: path})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := r.db.Reader(), r.db.Writer()
	req := httptest.NewRequest("POST", "/api/v1/sync/start", nil)
	req.Header.Set("X-Api-Key", "runtime-key")
	rec := httptest.NewRecorder()
	r.server.Handler().ServeHTTP(rec, req)
	if rec.Code != 202 {
		t.Fatalf("start sync: %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("web sync did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- r.close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel the web sync")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("outbound request was not cancelled")
	}
	for _, pool := range []*sql.DB{reader, writer} {
		if err := pool.PingContext(t.Context()); err == nil {
			t.Error("shutdown left a storage pool open")
		}
	}
	if err := os.Rename(filepath.Join(dir, "sync.db"), filepath.Join(dir, "closed.db")); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCLIOverridesStayTransient(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewDefault()
	cfg.APIKey, cfg.BindAddress, cfg.Port = "runtime-key", "0.0.0.0", freePort(t)
	path := filepath.Join(dir, "config.xml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	r, err := bootstrap(t.Context(), options{port: port, portSet: true, bindAddress: "127.0.0.1", bindSet: true}, runtimePaths{data: dir, config: path})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.close(); err != nil {
			t.Error(err)
		}
	}()
	if err := r.server.Start(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitBoot(t, client, base, "")
	code, response := apiCall(t, client, "PUT", base+"/api/v1/config", `{"instanceName":"changed"}`)
	if code != 200 {
		t.Fatal(response)
	}
	if response["restartRequired"] != false {
		t.Fatalf("an unrelated save must not require restart with CLI listener overrides: %+v", response)
	}
	persisted, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Port != cfg.Port || persisted.BindAddress != cfg.BindAddress {
		t.Fatal("unrelated save persisted CLI overrides")
	}
	newPort := freePort(t)
	code, response = apiCall(t, client, "PUT", base+"/api/v1/config", fmt.Sprintf(`{"port":%d}`, newPort))
	if code != 200 {
		t.Fatal(response)
	}
	code, response = apiCall(t, client, "POST", base+"/api/v1/system/restart", "")
	if code != 200 || response["reconnectUrl"] != base+"/" {
		t.Fatalf("restart must return to the effective CLI listener: %+v", response)
	}
}

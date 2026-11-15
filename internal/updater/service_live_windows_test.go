package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/winservice"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

type liveServiceFixture struct {
	APIBase, PublicKey, Data, Config, Address string
	Expected                                  Identity
}

func init() { runFixtureService = fixtureServiceMain }

func fixtureServiceMain(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var fixture liveServiceFixture
	if err := json.Unmarshal(b, &fixture); err != nil {
		return err
	}
	_, err = winservice.Run(func(ctx context.Context, ready func()) error {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(exe)
		if err != nil {
			return err
		}
		build := string(b[len(b)-3:])
		id := oldBuild
		if build != "old" {
			id = fixture.Expected
		}
		// The application fixture performs a real schema/data change before
		// becoming healthy, so rollback must undo an actual migrated database.
		db, err := storage.Open(filepath.Join(fixture.Data, "sync.db"))
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		if build != "old" {
			if _, err := db.Writer().Exec(`CREATE TABLE fixture_migration(id INTEGER); UPDATE users SET username='candidate'`); err != nil {
				return err
			}
		}
		if build == "bad" {
			return errors.New("fixture service failed after migration")
		}
		stop := make(chan struct{}, 1)
		cfg := config.NewDefault()
		engine := &Engine{DB: db, Client: &http.Client{Timeout: 10 * time.Second}, APIBase: fixture.APIBase, PublicKey: fixture.PublicKey,
			Current: id, Platform: "windows", Arch: runtime.GOARCH, Executable: exe, Staging: filepath.Join(fixture.Data, "staging"),
			HealthURL: "http://" + fixture.Address + "/api/v1/health", InstanceID: strconv.Itoa(os.Getpid()),
			Config: func() config.Config { return *cfg }, Container: func(string) bool { return false }}
		engine.Launch = func(ctx context.Context, plan string) error {
			managed, err := SpawnWorker(ctx, plan, exe)
			if err != nil {
				return err
			}
			if !managed {
				stop <- struct{}{}
			}
			return nil
		}
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
			commit := id.Commit
			if build == "sha" {
				commit = strings.Repeat("d", 40)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": id.Version, "commit": commit, "instanceId": engine.InstanceID, "service": winservice.Name(), "config": fixture.Config, "data": fixture.Data})
		})
		mux.HandleFunc("GET /api/v1/update/status", func(w http.ResponseWriter, r *http.Request) {
			status, _ := engine.Status(r.Context())
			_ = json.NewEncoder(w).Encode(status)
		})
		mux.HandleFunc("POST /api/v1/update/apply", func(w http.ResponseWriter, r *http.Request) {
			if err := engine.Apply(r.Context()); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusAccepted)
		})
		listener, err := net.Listen("tcp", fixture.Address)
		if err != nil {
			return err
		}
		server := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
		done := make(chan error, 1)
		go func() { done <- server.Serve(listener) }()
		ready()
		select {
		case <-ctx.Done():
		case <-stop:
		case err := <-done:
			return err
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	})
	return err
}

// TestLiveWindowsServiceUpdate requires an elevated token and only creates
// uniquely named, demand-start fixture services. It never touches existing ones.
func TestLiveWindowsServiceUpdate(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("live SCM verification requires an elevated administrator token")
	}
	manager, err := mgr.Connect()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Disconnect() })
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, build := range []string{"new", "bad", "sha"} {
		t.Run(build, func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "fixture-service.exe")
			if err := os.WriteFile(exe, append(append([]byte(nil), binary...), []byte("old")...), 0700); err != nil {
				t.Fatal(err)
			}
			fixture := newFixture(t, newBuild, "windows", runtime.GOARCH, append(append([]byte(nil), binary...), []byte(build)...))
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			_ = listener.Close()
			data := filepath.Join(dir, "custom-data")
			if err := os.Mkdir(data, 0700); err != nil {
				t.Fatal(err)
			}
			db, err := storage.Open(filepath.Join(data, "sync.db"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Writer().Exec(`INSERT INTO users(username,password_hash) VALUES('working','fixture')`)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			spec := liveServiceFixture{APIBase: fixture.server.URL, PublicKey: fixture.public, Data: data, Config: filepath.Join(dir, "custom.xml"), Address: address, Expected: newBuild}
			path := filepath.Join(dir, "fixture.json")
			b, _ := json.Marshal(spec)
			if err := os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("AniListArrSyncFixture-%d-%d", os.Getpid(), time.Now().UnixNano())
			service, err := manager.CreateService(name, exe, mgr.Config{DisplayName: name, StartType: mgr.StartManual}, "--fixture-service", path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cleanupLiveService(t, service, data) })
			actions := []mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: time.Minute}}
			if build == "bad" {
				actions = nil
			}
			if actions != nil {
				if err := service.SetRecoveryActions(actions, 86400); err != nil {
					t.Fatal(err)
				}
			}
			if err := service.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
				t.Fatal(err)
			}
			before, err := service.Config()
			if err != nil {
				t.Fatal(err)
			}
			reset, err := service.ResetPeriod()
			if err != nil {
				t.Fatal(err)
			}
			if err := service.Start(); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Timeout: 15 * time.Second}
			healthURL := "http://" + address + "/api/v1/health"
			if err := waitHealth(t.Context(), client, healthURL, oldBuild, "", 15*time.Second); err != nil {
				t.Fatal(err)
			}
			initial, err := fetch(t.Context(), client, healthURL, 64<<10)
			if err != nil {
				t.Fatal(err)
			}
			var initialHealth struct{ InstanceID string }
			if err := json.Unmarshal(initial, &initialHealth); err != nil {
				t.Fatal(err)
			}
			response, err := client.Post("http://"+address+"/api/v1/update/apply", "application/json", nil)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusAccepted {
				t.Fatalf("update apply HTTP %d", response.StatusCode)
			}
			expected := newBuild
			if build != "new" {
				expected = oldBuild
			}
			if err := waitHealth(t.Context(), client, healthURL, expected, initialHealth.InstanceID, 50*time.Second); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(15 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(data, "staging", "update.lock")); errors.Is(err, os.ErrNotExist) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("worker did not restore service configuration")
				}
				time.Sleep(50 * time.Millisecond)
			}
			current, err := service.Config()
			if err != nil || !reflect.DeepEqual(before, current) {
				t.Fatal("registered service configuration changed")
			}
			restored, err := service.RecoveryActions()
			if err != nil || !reflect.DeepEqual(restored, actions) {
				t.Fatal("recovery actions were not restored")
			}
			currentReset, err := service.ResetPeriod()
			if err != nil || currentReset != reset {
				t.Fatal("recovery reset period changed")
			}
			nonCrash, err := service.RecoveryActionsOnNonCrashFailures()
			if err != nil || !nonCrash {
				t.Fatal("non-crash recovery flag was not restored")
			}
			health, err := fetch(t.Context(), client, healthURL, 64<<10)
			if err != nil {
				t.Fatal(err)
			}
			var identity struct{ Service, Config, Data string }
			if err := json.Unmarshal(health, &identity); err != nil || identity.Service != name || identity.Config != spec.Config || identity.Data != spec.Data {
				t.Fatal("registered service identity or launch paths lost")
			}
			statusBytes, err := fetch(t.Context(), client, "http://"+address+"/api/v1/update/status", 64<<10)
			if err != nil {
				t.Fatal(err)
			}
			var status Status
			if err := json.Unmarshal(statusBytes, &status); err != nil {
				t.Fatal(err)
			}
			if status.CanUpdate || (build != "new" && !strings.Contains(status.Reason, "24-hour cooldown")) {
				t.Fatal("restored service lost cooldown")
			}
			if err := (&serviceProcess{service: service}).Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			db, err = storage.Open(filepath.Join(data, "sync.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			var username string
			if err := db.Reader().QueryRow(`SELECT username FROM users`).Scan(&username); err != nil {
				t.Fatal(err)
			}
			var migrated int
			if err := db.Reader().QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='fixture_migration'`).Scan(&migrated); err != nil {
				t.Fatal(err)
			}
			if build == "new" {
				if username != "candidate" || migrated != 1 {
					t.Fatal("new service did not migrate")
				}
			} else if username != "working" || migrated != 0 {
				t.Fatal("rollback did not restore the pre-migration database")
			}
			installed, err := os.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			trailer := "new"
			if build != "new" {
				trailer = "old"
			}
			if !strings.HasSuffix(string(installed), trailer) {
				t.Fatal("incorrect service executable after update")
			}
		})
	}
}

func cleanupLiveService(t *testing.T, service *mgr.Service, data string) {
	t.Helper()
	// An interrupted attempt may have a worker completing rollback. Wait for that
	// worker before deleting only the service created by this test.
	deadline := time.Now().Add(workerAttemptTimeout + workerRecoveryTimeout + 15*time.Second)
	for {
		if _, err := os.Stat(filepath.Join(data, "staging", "update.lock")); errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("fixture worker did not finish; refusing to delete service %s during rollback", service.Name)
			_ = service.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := (&serviceProcess{service: service}).Stop(ctx); err != nil {
		t.Errorf("stop fixture service: %v", err)
	}
	if err := service.Delete(); err != nil {
		t.Errorf("delete fixture service: %v", err)
	}
	_ = service.Close()
}

package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

type fixtureControl struct {
	plan                    Plan
	failure                 string
	starts, stops, restores int
	health                  Identity
	instance                string
	db                      *storage.DB
	cancelAfterStart        context.CancelFunc
}

func (f *fixtureControl) Prepare() error { return nil }
func (f *fixtureControl) Restore() error { f.restores++; return nil }
func (f *fixtureControl) Stop(context.Context) error {
	f.stops++
	if f.db != nil {
		err := f.db.Close()
		f.db = nil
		return err
	}
	return nil
}
func (f *fixtureControl) Start() error {
	f.starts++
	b, err := os.ReadFile(f.plan.Executable)
	if err != nil {
		return err
	}
	if string(b) == "new" {
		if _, err := os.Stat(f.plan.Snapshot); err != nil {
			return errors.New("migration started before snapshot")
		}
		if f.failure == "startup" {
			return errors.New("fixture startup failed")
		}
		f.db, err = storage.Open(f.plan.Database)
		if err != nil {
			return err
		}
		_, err = f.db.Writer().Exec(`CREATE TABLE new_migration(id INTEGER); UPDATE users SET username='new'`)
		if err != nil {
			return err
		}
		f.health, f.instance = f.plan.Expected, "new-boot"
		if f.cancelAfterStart != nil {
			f.cancelAfterStart()
		}
		switch f.failure {
		case "version":
			f.health.Version = "v0.0.0"
		case "commit":
			f.health.Commit = strings.Repeat("c", 40)
		case "old instance":
			f.instance = f.plan.PreviousInstance
		case "unhealthy":
			f.health = Identity{}
		}
	} else {
		f.health, f.instance = f.plan.Previous, "rollback-boot"
	}
	return nil
}

func TestRollbackOutlivesUpdateDeadline(t *testing.T) {
	p := fixturePlan(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fixtureControl{plan: p, cancelAfterStart: cancel}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": f.health.Version, "commit": f.health.Commit, "instanceId": f.instance})
	}))
	defer srv.Close()
	p.HealthURL = srv.URL
	if _, err := execute(ctx, p, f, 150*time.Millisecond, srv.Client()); err == nil {
		t.Fatal("cancelled update succeeded")
	}
	if f.db != nil {
		_ = f.db.Close()
	}
	if f.health != p.Previous {
		t.Fatal("prior build was not restarted")
	}
	checkRestoration(t, p, false)
}

func TestRollbackRestartsWhenHistoryCannotBeRecorded(t *testing.T) {
	p := fixturePlan(t)
	db, err := storage.Open(p.Database)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer().Exec(`CREATE TRIGGER reject_update_history BEFORE INSERT ON update_history BEGIN SELECT RAISE(FAIL,'fixture history unavailable'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	f := &fixtureControl{plan: p, failure: "startup"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": f.health.Version, "commit": f.health.Commit, "instanceId": f.instance})
	}))
	defer srv.Close()
	p.HealthURL = srv.URL
	completed, err := execute(context.Background(), p, f, 150*time.Millisecond, srv.Client())
	if err == nil || !strings.Contains(err.Error(), "fixture history unavailable") {
		t.Fatalf("missing history failure: %v", err)
	}
	if f.health != p.Previous {
		t.Fatal("restored build was left stopped after history failure")
	}
	if completed {
		t.Fatal("history failure released the update block")
	}
}

func TestHealthyCandidateCompletesWhenSuccessHistoryFails(t *testing.T) {
	p := fixturePlan(t)
	db, err := storage.Open(p.Database)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer().Exec(`CREATE TRIGGER reject_success_history BEFORE INSERT ON update_history BEGIN SELECT RAISE(FAIL,'fixture success history unavailable'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	f := &fixtureControl{plan: p}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": f.health.Version, "commit": f.health.Commit, "instanceId": f.instance})
	}))
	defer srv.Close()
	p.HealthURL = srv.URL
	completed, err := execute(context.Background(), p, f, 150*time.Millisecond, srv.Client())
	if f.db != nil {
		defer func() { _ = f.db.Close() }()
	}
	if err == nil || !strings.Contains(err.Error(), "fixture success history unavailable") {
		t.Fatalf("missing bookkeeping error: %v", err)
	}
	if !completed {
		t.Fatal("healthy candidate left updates blocked after success-history failure")
	}
	if f.health != p.Expected {
		t.Fatal("healthy candidate was rolled back")
	}
	b, err := os.ReadFile(p.Executable)
	if err != nil || string(b) != "new" {
		t.Fatal("healthy candidate executable was not retained")
	}
	if _, err := os.Stat(p.Executable + ".previous"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed update retained previous executable")
	}
}

func TestSafePreReplacementFailureCompletesAfterRecovery(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(fmt.Sprintf("previous healthy=%v", healthy), func(t *testing.T) {
			p := fixturePlan(t)
			if err := os.Remove(p.Next); err != nil {
				t.Fatal(err)
			}
			f := &fixtureControl{plan: p}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				status := "ok"
				if !healthy {
					status = "error"
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"status": status, "version": f.health.Version, "commit": f.health.Commit, "instanceId": f.instance})
			}))
			defer srv.Close()
			p.HealthURL = srv.URL
			completed, err := execute(context.Background(), p, f, 150*time.Millisecond, srv.Client())
			if err == nil {
				t.Fatal("missing replacement file was accepted")
			}
			if completed != healthy || f.health != p.Previous {
				t.Fatal("completion did not match verified health of the previous build")
			}
			if f.restores != 1 {
				t.Fatal("service configuration was not restored")
			}
		})
	}
}

type interruptedService struct {
	*fixtureControl
	stopped    bool
	restoreErr error
}

func (f *interruptedService) Stop(context.Context) error {
	return errors.New("fixture stop wait failed")
}
func (f *interruptedService) Stopped(context.Context) (bool, error) { return f.stopped, nil }
func (f *interruptedService) Restore() error {
	f.restores++
	return f.restoreErr
}

func TestInterruptedServiceStopRecovery(t *testing.T) {
	for _, test := range []struct {
		name                               string
		stopped, unhealthy, restoreFailure bool
	}{
		{name: "confirmed stopped", stopped: true},
		{name: "still running"},
		{name: "unhealthy restart", stopped: true, unhealthy: true},
		{name: "configuration restore fails", stopped: true, restoreFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := fixturePlan(t)
			p.Service = "fixture-service"
			p.Lock = filepath.Join(p.Directory, "update.lock")
			path := filepath.Join(p.Directory, "plan.json")
			for _, file := range []string{p.Lock, path + ".go"} {
				if err := os.WriteFile(file, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			f := &interruptedService{fixtureControl: &fixtureControl{plan: p}, stopped: test.stopped}
			if test.restoreFailure {
				f.restoreErr = errors.New("fixture restore failed")
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				status := "ok"
				if test.unhealthy {
					status = "error"
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"status": status, "version": f.health.Version, "commit": f.health.Commit, "instanceId": f.instance})
			}))
			defer srv.Close()
			p.HealthURL = srv.URL
			if err := runWorker(path, p, f, 100*time.Millisecond, srv.Client()); err == nil || !strings.Contains(err.Error(), "fixture stop wait failed") {
				t.Fatalf("missing original stop failure: %v", err)
			}
			wantStarts := 0
			if test.stopped {
				wantStarts = 1
			}
			if f.starts != wantStarts || f.restores != 1 {
				t.Fatalf("starts=%d, restores=%d", f.starts, f.restores)
			}
			safe := test.stopped && !test.unhealthy && !test.restoreFailure
			_, lockErr := os.Stat(p.Lock)
			if errors.Is(lockErr, os.ErrNotExist) != safe {
				t.Fatalf("recovery lock removal does not match confirmed safe state: %v", lockErr)
			}
			if (completedWorker(p.Directory) > 0) != safe {
				t.Fatal("completion proof does not match safe state")
			}
			if !safe {
				if _, err := os.Stat(p.Snapshot); err != nil {
					t.Fatal("unsafe recovery lost snapshot")
				}
			}
		})
	}
}

func fixturePlan(t *testing.T) Plan {
	t.Helper()
	dir := t.TempDir()
	database := filepath.Join(dir, "sync.db")
	db, err := storage.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().Exec(`INSERT INTO users(username,password_hash) VALUES('old','fixture')`); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(dir, "before.db")
	if err := db.VacuumInto(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	// This accepted write occurred after the preparation snapshot. The worker's
	// final snapshot must retain it if the candidate later changes the schema.
	if _, err := db.Writer().Exec(`UPDATE users SET username='accepted-before-stop'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	p := Plan{Expected: newBuild, Previous: oldBuild, Executable: filepath.Join(dir, "app"), Next: filepath.Join(dir, "candidate"), Database: database, Snapshot: snapshot, PreviousInstance: "old-boot", Directory: dir}
	if err := os.WriteFile(p.Executable, []byte("old"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Next, []byte("new"), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

func checkRestoration(t *testing.T, p Plan, success bool) {
	t.Helper()
	b, err := os.ReadFile(p.Executable)
	if err != nil {
		t.Fatal(err)
	}
	want := "old"
	if success {
		want = "new"
	}
	if string(b) != want {
		t.Fatalf("executable=%s", b)
	}
	db, err := storage.Open(p.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var username string
	if err := db.Reader().QueryRow(`SELECT username FROM users`).Scan(&username); err != nil {
		t.Fatal(err)
	}
	if !success && username != "accepted-before-stop" {
		t.Fatalf("database was not restored: %s", username)
	}
	blocked, err := Blocked(context.Background(), db, p.Expected, time.Now())
	if err != nil || blocked == success {
		t.Fatalf("cooldown persistence: blocked=%v err=%v", blocked, err)
	}
	var count int
	if err := db.Reader().QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='new_migration'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if !success && count != 0 {
		t.Fatal("candidate migration survived rollback")
	}
	if !success {
		var rolled bool
		if err := db.Reader().QueryRow(`SELECT rolled_back FROM update_history WHERE version=?`, p.Expected.Key()).Scan(&rolled); err != nil || !rolled {
			t.Fatal("failure was erased by restoration")
		}
	}
}

func TestWorkerRecovery(t *testing.T) {
	for _, failure := range []string{"", "startup", "version", "commit", "old instance", "unhealthy"} {
		name := failure
		if name == "" {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			p := fixturePlan(t)
			if failure == "commit" {
				p.Expected.Channel = "develop"
				p.Expected.Version = "v1.1.0-nightly.20261111010000.bbbbbbb"
			}
			f := &fixtureControl{plan: p, failure: failure}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": f.health.Version, "commit": f.health.Commit, "instanceId": f.instance})
			}))
			defer srv.Close()
			p.HealthURL = srv.URL
			_, err := execute(context.Background(), p, f, 150*time.Millisecond, srv.Client())
			if (err == nil) != (failure == "") {
				t.Fatalf("worker result: %v", err)
			}
			if f.db != nil {
				_ = f.db.Close()
			}
			if f.restores != 1 {
				t.Fatal("did not restore process/service configuration")
			}
			if failure != "" && (f.starts != 2 || f.stops != 1) {
				t.Fatal("did not stop candidate and restart prior build")
			}
			checkRestoration(t, p, failure == "")
		})
	}
}

// TestMain also supplies real, temporary child executables to exercise detached
// worker launch, parent exit, executable replacement and health over localhost.
var runFixtureService func(string) error

func TestMain(m *testing.M) {
	if fixtureSystemctl() {
		os.Exit(0)
	}
	if len(os.Args) >= 3 {
		switch os.Args[1] {
		case "--fixture-service":
			if runFixtureService == nil {
				os.Exit(2)
			}
			if err := runFixtureService(os.Args[2]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		case "--update-worker":
			if os.Getenv("ANILIST_ARR_SYNC_CANCELLATION_FIXTURE") == "exit-after-ready" {
				path := os.Args[2]
				if os.WriteFile(path+".ready", nil, 0600) != nil {
					os.Exit(2)
				}
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(path + ".go"); err == nil {
						// Simulate abrupt termination: skip RunWorker's deferred cleanup.
						os.Exit(1)
					}
					time.Sleep(10 * time.Millisecond)
				}
				os.Exit(2)
			}
			if os.Getenv("ANILIST_ARR_SYNC_CANCELLATION_FIXTURE") == "unconfirmed" {
				os.Exit(0)
			}
			if os.Getenv("ANILIST_ARR_SYNC_CANCELLATION_FIXTURE") == "1" {
				path := os.Args[2]
				_ = os.WriteFile(path+".ready", nil, 0600)
				for {
					if _, err := os.Stat(path + ".cancel"); err == nil {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				time.Sleep(100 * time.Millisecond)
				_ = os.WriteFile(path+".restored", nil, 0600)
				_ = os.Remove(path + ".ready")
				_ = os.WriteFile(filepath.Join(filepath.Dir(path), "completed"), []byte(strconv.Itoa(os.Getpid())), 0600)
				os.Exit(0)
			}
			if err := RunWorker(os.Args[2]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		case "--fixture-parent":
			path := os.Args[2]
			b, err := os.ReadFile(path)
			if err != nil {
				os.Exit(2)
			}
			var p Plan
			if json.Unmarshal(b, &p) != nil {
				os.Exit(2)
			}
			p.ParentPID = os.Getpid()
			b, _ = json.Marshal(p)
			if os.WriteFile(path, b, 0600) != nil {
				os.Exit(2)
			}
			if _, err := SpawnWorker(context.Background(), path, p.Executable); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			os.Exit(0)
		case "--fixture-app":
			fixtureApp(os.Args[2])
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func TestFailedHandoffWaitsForWorkerRestoration(t *testing.T) {
	t.Setenv("ANILIST_ARR_SYNC_CANCELLATION_FIXTURE", "1")
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".go", 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(path+".cancel", nil, 0600)
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(path + ".ready"); errors.Is(err, os.ErrNotExist) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = SpawnWorker(context.Background(), path, exe)
	if err == nil {
		t.Fatal("failed handoff was accepted")
	}
	if errors.Is(err, ErrWorkerActive) {
		t.Fatal("confirmed restored worker left updates blocked")
	}
	if _, err := os.Stat(path + ".restored"); err != nil {
		t.Fatal("handoff returned before worker restored its configuration")
	}
	if _, err := os.Stat(path + ".ready"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("worker was still active after cancellation")
	}
}

func TestUnacknowledgedWorkerExitKeepsRecovery(t *testing.T) {
	t.Setenv("ANILIST_ARR_SYNC_CANCELLATION_FIXTURE", "unconfirmed")
	f := newFixture(t, newBuild, "linux", "amd64", []byte("new"))
	e := fixtureEngine(t, f, "linux", "amd64")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e.Launch = func(ctx context.Context, path string) error {
		_, err := SpawnWorker(ctx, path, self)
		return err
	}
	if err := e.Apply(context.Background()); !errors.Is(err, ErrWorkerActive) {
		t.Fatalf("unacknowledged exit lost its recovery block: %v", err)
	}
	status, err := e.Status(context.Background())
	if err != nil || status.CanUpdate {
		t.Fatal("unacknowledged worker exit allowed another update")
	}
	entries, err := os.ReadDir(e.Staging)
	if err != nil || len(entries) == 0 {
		t.Fatal("unacknowledged worker exit discarded recovery data")
	}
}

func TestCancellationDuringAcknowledgementWaitsForRestoration(t *testing.T) {
	t.Setenv("ANILIST_ARR_SYNC_CANCELLATION_FIXTURE", "1")
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			if _, err := os.Stat(path + ".go"); err == nil {
				cancel()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	_, err = SpawnWorker(ctx, path, self)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrWorkerActive) {
		t.Fatalf("unacknowledged handoff was not safely cancelled: %v", err)
	}
	if _, err := os.Stat(path + ".restored"); err != nil {
		t.Fatal("cancellation returned before worker restoration")
	}
}

func TestAcknowledgementWriteFailureLeavesPreviousBuildRunning(t *testing.T) {
	p := fixturePlan(t)
	p.Lock = filepath.Join(p.Directory, "update.lock")
	path := filepath.Join(p.Directory, "plan.json")
	for _, file := range []string{p.Lock, path + ".go"} {
		if err := os.WriteFile(file, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(path+".accepted", 0700); err != nil {
		t.Fatal(err)
	}
	f := &fixtureControl{plan: p}
	if err := runWorker(path, p, f, time.Second, http.DefaultClient); err == nil {
		t.Fatal("failed acknowledgement was accepted")
	}
	if f.restores != 1 || f.starts != 0 || f.stops != 0 {
		t.Fatal("acknowledgement failure did not restore configuration before stopping the previous build")
	}
	if _, err := os.Stat(p.Lock); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("safe acknowledgement failure retained the recovery lock")
	}
}

func TestWorkerExitAfterReadinessDoesNotRequestShutdown(t *testing.T) {
	t.Setenv("ANILIST_ARR_SYNC_CANCELLATION_FIXTURE", "exit-after-ready")
	f := newFixture(t, newBuild, "linux", "amd64", []byte("new"))
	e := fixtureEngine(t, f, "linux", "amd64")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shutdown := false
	var attempt string
	e.Launch = func(ctx context.Context, path string) error {
		attempt = filepath.Dir(path)
		managed, err := SpawnWorker(ctx, path, self)
		if err == nil && !managed {
			shutdown = true
		}
		return err
	}
	if err := e.Apply(context.Background()); !errors.Is(err, ErrWorkerActive) {
		t.Fatalf("worker exit after readiness accepted a handoff: %v", err)
	}
	if shutdown {
		t.Fatal("original application was asked to shut down without a worker")
	}
	status, err := e.Status(context.Background())
	if err != nil || status.CanUpdate {
		t.Fatal("unconfirmed recovery allowed another update")
	}
	if _, err := os.Stat(filepath.Join(attempt, "before.db")); err != nil {
		t.Fatal("unconfirmed recovery discarded the database snapshot")
	}
}

func fixtureApp(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		os.Exit(2)
	}
	var p Plan
	if json.Unmarshal(b, &p) != nil {
		os.Exit(2)
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}
	b, err = os.ReadFile(exe)
	if err != nil || len(b) < 3 {
		os.Exit(2)
	}
	id := p.Previous
	if string(b[len(b)-3:]) == "new" {
		id = p.Expected
	}
	db, err := storage.Open(p.Database)
	if err != nil {
		os.Exit(2)
	}
	defer func() { _ = db.Close() }()
	if os.WriteFile(filepath.Join(p.Directory, "child.pid"), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
		os.Exit(2)
	}
	if len(os.Args) != 7 || os.Args[3] != "--config" || os.Args[4] != filepath.Join(p.Directory, "custom.xml") || os.Args[5] != "--data-dir" || os.Args[6] != p.Directory {
		os.Exit(2)
	}
	_ = os.WriteFile(filepath.Join(p.Directory, "launch-paths-preserved"), nil, 0600)
	u, err := url.Parse(p.HealthURL)
	if err != nil {
		os.Exit(2)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": id.Version, "commit": id.Commit, "instanceId": strconv.Itoa(os.Getpid())})
	})
	_ = http.ListenAndServe(u.Host, mux)
}

func TestDetachedWorkerReplacementAndRollback(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	app, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			p := fixturePlan(t)
			// The current test executable plus a trailer is a runnable fixture.
			// A malformed candidate exercises a genuine OS startup failure.
			if err := os.Remove(p.Executable); err != nil {
				t.Fatal(err)
			}
			p.Executable += filepath.Ext(self)
			if err := os.WriteFile(p.Executable, append(append([]byte(nil), app...), []byte("old")...), 0700); err != nil {
				t.Fatal(err)
			}
			binary := append(append([]byte(nil), app...), []byte("new")...)
			if fail {
				binary = []byte("invalid executable")
			}
			if err := os.WriteFile(p.Next, binary, 0700); err != nil {
				t.Fatal(err)
			}
			probe := httptest.NewServer(http.NotFoundHandler())
			p.HealthURL = probe.URL + "/api/v1/health"
			probe.Close()
			path := filepath.Join(p.Directory, "plan.json")
			p.Args = []string{"--fixture-app", path, "--config", filepath.Join(p.Directory, "custom.xml"), "--data-dir", p.Directory}
			p.Lock = filepath.Join(p.Directory, "update.lock")
			if err := os.WriteFile(p.Lock, nil, 0600); err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(p)
			if err := os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				b, err := os.ReadFile(filepath.Join(p.Directory, "child.pid"))
				if err != nil {
					return
				}
				pid, err := strconv.Atoi(string(b))
				if err != nil {
					return
				}
				process, err := os.FindProcess(pid)
				if err == nil {
					_ = process.Kill()
					_ = process.Release()
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = waitProcess(ctx, pid)
				}
			})
			parent := exec.Command(p.Executable, "--fixture-parent", path)
			parent.Dir = p.Directory
			if out, err := parent.CombinedOutput(); err != nil {
				t.Fatalf("parent launch: %v %s", err, out)
			}
			expected := p.Expected
			if fail {
				expected = p.Previous
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := waitHealth(ctx, &http.Client{Timeout: time.Second}, p.HealthURL, expected, p.PreviousInstance, 15*time.Second); err != nil {
				t.Fatal(err)
			}
			for {
				if _, err := os.Stat(p.Lock); errors.Is(err, os.ErrNotExist) {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("detached worker did not finish")
				case <-time.After(20 * time.Millisecond):
				}
			}
			for _, staged := range []string{p.Snapshot, p.Next} {
				if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("completed worker retained bulky staging files")
				}
			}
			if _, err := os.Stat(filepath.Join(p.Directory, "launch-paths-preserved")); err != nil {
				t.Fatal("launch paths lost")
			}
			db, err := openDatabase(p.Database)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			var status string
			if err := db.QueryRow(`SELECT status FROM update_history WHERE version=?`, p.Expected.Key()).Scan(&status); err != nil {
				t.Fatal(err)
			}
			want := "SUCCESS"
			if fail {
				want = "FAILED"
			}
			if status != want {
				t.Fatalf("history=%s", status)
			}
			installed, err := os.ReadFile(p.Executable)
			if err != nil {
				t.Fatal(err)
			}
			trailer := "new"
			if fail {
				trailer = "old"
			}
			if !strings.HasSuffix(string(installed), trailer) {
				t.Fatal("wrong binary installed")
			}
		})
	}
}

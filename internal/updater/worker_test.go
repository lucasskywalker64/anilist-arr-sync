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
			err := execute(context.Background(), p, f, 150*time.Millisecond, srv.Client())
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
			if err := SpawnWorker(context.Background(), path, p.Executable); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			if os.WriteFile(path+".go", nil, 0600) != nil {
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

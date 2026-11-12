package updater

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
	"golang.org/x/mod/semver"
)

// Engine is owned by the runtime; settings are read for each operation.
type Engine struct {
	DB                                         *storage.DB
	Client                                     *http.Client
	APIBase, PublicKey, Platform, Arch         string
	Current                                    Identity
	Executable, Staging, HealthURL, InstanceID string
	Args                                       []string
	Config                                     func() config.Config
	Container                                  func(string) bool
	Launch                                     func(context.Context, string) error
	mu                                         sync.Mutex
}

// Status also explains why an installation cannot replace itself.
type Status struct {
	CurrentVersion  string    `json:"currentVersion"`
	LatestVersion   string    `json:"latestVersion"`
	Commit          string    `json:"commit"`
	LatestCommit    string    `json:"latestCommit,omitempty"`
	UpdateAvailable bool      `json:"updateAvailable"`
	CanUpdate       bool      `json:"canUpdate"`
	Owner           string    `json:"updateOwner"`
	Reason          string    `json:"reason,omitempty"`
	ReleaseNotes    string    `json:"releaseNotes,omitempty"`
	LastCheckedAt   time.Time `json:"lastCheckedAt"`
}

// Ownership uses an installation marker, never the presence of package tools.
func Ownership(exe, mechanism string, container bool) (string, string) {
	if container {
		return "Docker", "Updates are managed by container image replacement."
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "update-owner"))
	if err == nil {
		owner := strings.TrimSpace(string(b))
		if owner == "" {
			return "Package", "Invalid update-owner marker; built-in updates are disabled."
		}
		return owner, "Updates are managed by " + owner + "."
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "Package", "Cannot read update-owner marker; built-in updates are disabled."
	}
	if mechanism == "Package" {
		return "Package", "Updates are managed by the installation's package manager."
	}
	return "BuiltIn", ""
}

func (e *Engine) status(ctx context.Context) (Status, *candidate, error) {
	cfg := e.Config()
	owner, reason := Ownership(e.Executable, cfg.UpdateMechanism, e.Container(cfg.UpdateMechanism))
	s := Status{CurrentVersion: e.Current.Version, LatestVersion: e.Current.Version, Commit: e.Current.Commit,
		Owner: owner, Reason: reason, LastCheckedAt: time.Now().UTC()}
	if reason != "" {
		return s, nil, nil
	}
	if _, err := os.Stat(filepath.Join(e.Staging, "update.lock")); err == nil {
		s.Reason = "An update worker or incomplete recovery owns staging. Check the worker log before retrying."
		return s, nil, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		s.Reason = err.Error()
		return s, nil, err
	}
	c, err := e.discover(ctx, cfg.UpdateBranch)
	if err != nil {
		s.Reason = err.Error()
		return s, nil, err
	}
	s.LatestVersion, s.LatestCommit, s.ReleaseNotes = c.Version, c.Commit, c.notes
	if cfg.UpdateBranch == "main" {
		s.UpdateAvailable = !semver.IsValid(e.Current.Version) || semver.Compare(c.Version, e.Current.Version) > 0
	} else {
		s.UpdateAvailable = c.Version != e.Current.Version || c.Commit != e.Current.Commit
	}
	blocked, err := Blocked(ctx, e.DB, c.Identity, time.Now())
	if err != nil {
		s.Reason = err.Error()
		return s, nil, err
	}
	if blocked {
		s.Reason = "This build is in a 24-hour cooldown after a failed update."
	}
	s.CanUpdate = s.UpdateAvailable && !blocked
	return s, c, nil
}

// Status checks the selected channel and reports installation restrictions.
func (e *Engine) Status(ctx context.Context) (Status, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, _, err := e.status(ctx)
	return s, err
}

// Apply authenticates everything before staging executable bytes or snapshotting.
func (e *Engine) Apply(ctx context.Context) error {
	if !e.mu.TryLock() {
		return errors.New("update operation already in progress")
	}
	defer e.mu.Unlock()
	s, c, err := e.status(ctx)
	if err != nil {
		return err
	}
	if !s.CanUpdate {
		if s.Reason != "" {
			return errors.New(s.Reason)
		}
		return errors.New("no newer build available")
	}
	if e.Launch == nil {
		return errors.New("update worker is unavailable")
	}
	data, err := fetch(ctx, e.Client, c.url, 256<<20)
	if err != nil {
		return err
	}
	if err := verifyChecksum(data, c.archive, c.sums); err != nil {
		return err
	}
	binary, err := executable(data, e.Platform)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e.Staging, 0700); err != nil {
		return err
	}
	if err := pruneCompleted(e.Staging); err != nil {
		return err
	}
	lock := filepath.Join(e.Staging, "update.lock")
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("update staging is locked: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(lock)
		return err
	}
	dir, err := os.MkdirTemp(e.Staging, "update-*")
	if err != nil {
		_ = os.Remove(lock)
		return err
	}
	workerOwnsState := false
	defer func() {
		if !workerOwnsState {
			_ = os.RemoveAll(dir)
			_ = os.Remove(lock)
		}
	}()
	next := filepath.Join(dir, filepath.Base(e.Executable))
	if err := os.WriteFile(next, binary, 0700); err != nil {
		return err
	}
	snapshot := filepath.Join(dir, "before.db")
	if err := e.DB.VacuumInto(ctx, snapshot); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	service, userService, err := currentService()
	if err != nil {
		return err
	}
	p := Plan{Expected: c.Identity, Previous: e.Current, Executable: e.Executable, Next: next,
		Database: e.DB.Path(), Snapshot: snapshot, Args: e.Args, Directory: cwd, ParentPID: os.Getpid(),
		HealthURL: e.HealthURL, PreviousInstance: e.InstanceID, Service: service, UserService: userService, Lock: lock}
	path := filepath.Join(dir, "plan.json")
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		return err
	}
	if err := e.Launch(ctx, path); err != nil {
		workerOwnsState = errors.Is(err, ErrWorkerActive)
		return err
	}
	workerOwnsState = true
	return nil
}

// Blocked queries the exact identity, including the source commit of a nightly.
func Blocked(ctx context.Context, db *storage.DB, id Identity, now time.Time) (bool, error) {
	if db == nil {
		return false, errors.New("update database is unavailable")
	}
	var count int
	err := db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		return q.QueryRowContext(ctx, `SELECT count(*) FROM update_history WHERE version = ? AND status = 'FAILED' AND cooldown_until > ?`, id.Key(), now.UTC().Format(time.RFC3339Nano)).Scan(&count)
	})
	return count > 0, err
}

func record(ctx context.Context, database string, id Identity, failure error, now time.Time) (result error) {
	// Open without storage.Open: recording history must never run migrations.
	db, err := openDatabase(database)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	status, rolledBack, message := "SUCCESS", false, ""
	var cooldown any
	if failure != nil {
		status, rolledBack, message = "FAILED", true, failure.Error()
		cooldown = now.Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO update_history(version,status,rolled_back,error_message,cooldown_until) VALUES(?,?,?,?,?)`, id.Key(), status, rolledBack, message, cooldown)
	return err
}

func openDatabase(path string) (*sql.DB, error) {
	// Match storage's filename spelling so Windows connections share the WAL.
	return sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)")
}

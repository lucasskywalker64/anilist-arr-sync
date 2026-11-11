package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Plan is private staging state shared with a temporary copy of this application.
type Plan struct {
	Expected, Previous                              Identity
	Executable, Next, Database, Snapshot, Directory string
	HealthURL, PreviousInstance, Service, Lock      string
	Args                                            []string
	ParentPID                                       int
}

// processControl is the seam for direct child processes and registered services.
type processControl interface {
	Prepare() error
	Stop(context.Context) error
	Start() error
	Restore() error
}

type directProcess struct {
	plan  Plan
	child *exec.Cmd
}

func (p *directProcess) Prepare() error { return nil }
func (p *directProcess) Restore() error { return nil }
func (p *directProcess) Start() error {
	c := exec.Command(p.plan.Executable, p.plan.Args...)
	c.Dir = p.plan.Directory
	detach(c)
	if err := c.Start(); err != nil {
		return err
	}
	p.child = c
	go func() { _ = c.Wait() }()
	return nil
}
func (p *directProcess) Stop(ctx context.Context) error {
	if p.child == nil {
		return nil
	}
	if err := p.child.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return waitProcess(ctx, p.child.Process.Pid)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	err = errors.Join(err, out.Sync(), out.Close())
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}

// SpawnWorker starts a detached copy and waits until service recovery is suspended.
func SpawnWorker(ctx context.Context, path, exe string) error {
	worker := filepath.Join(filepath.Dir(path), "worker"+filepath.Ext(exe))
	if err := copyFile(exe, worker, 0700); err != nil {
		return err
	}
	c := exec.Command(worker, "--update-worker", path)
	detach(c)
	log, err := os.OpenFile(filepath.Join(filepath.Dir(path), "worker.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	c.Stdout, c.Stderr = log, log
	if err := c.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(path + ".ready"); err == nil {
			return nil
		}
		select {
		case err := <-done:
			return fmt.Errorf("update worker exited before readiness: %v", err)
		case <-ctx.Done():
			// The worker owns recovery configuration. Ask it to exit cleanly rather
			// than killing it while the service's recovery actions are disabled.
			_ = os.WriteFile(path+".cancel", nil, 0600)
			<-done
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// RunWorker runs before normal configuration loading, database opening or migrations.
func RunWorker(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var p Plan
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	control, err := controller(p)
	if err != nil {
		return err
	}
	if err := control.Prepare(); err != nil {
		return errors.Join(err, control.Restore())
	}
	defer func() { _ = os.Remove(p.Lock) }()
	defer func() { _ = os.Remove(path + ".ready") }()
	defer func() { _ = os.Remove(path + ".cancel") }()
	if err := os.WriteFile(path+".ready", nil, 0600); err != nil {
		return errors.Join(err, control.Restore())
	}
	// The parent commits the handoff only after SpawnWorker returns successfully.
	for {
		if _, err := os.Stat(path + ".cancel"); err == nil {
			return control.Restore()
		}
		if _, err := os.Stat(path + ".go"); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), control.Restore())
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer func() { _ = os.Remove(path + ".go") }()
	if p.Service != "" {
		if err := control.Stop(ctx); err != nil {
			return errors.Join(err, control.Restore())
		}
	}
	if err := waitProcess(ctx, p.ParentPID); err != nil {
		return errors.Join(err, control.Restore())
	}
	return execute(ctx, p, control, 30*time.Second, &http.Client{Timeout: 2 * time.Second})
}

// execute runs only after the previous process has released its executable and DB.
func execute(ctx context.Context, p Plan, control processControl, healthTimeout time.Duration, client *http.Client) (result error) {
	defer func() { result = errors.Join(result, control.Restore()) }()
	// Refresh the online preparation snapshot after shutdown so accepted writes
	// between preparation and shutdown survive rollback. This opens no migrations.
	if err := os.Remove(p.Snapshot); err != nil {
		return errors.Join(err, control.Start())
	}
	db, err := openDatabase(p.Database)
	if err != nil {
		return errors.Join(err, control.Start())
	}
	_, err = db.ExecContext(ctx, `VACUUM INTO ?`, p.Snapshot)
	err = errors.Join(err, db.Close())
	if err != nil {
		return errors.Join(err, control.Start())
	}
	backup, replacement := p.Executable+".previous", p.Executable+".next"
	if err := copyFile(p.Next, replacement, 0755); err != nil {
		return errors.Join(err, control.Start())
	}
	defer func() { _ = os.Remove(replacement) }()
	if err := os.Rename(p.Executable, backup); err != nil {
		return errors.Join(err, control.Start())
	}
	if err := os.Rename(replacement, p.Executable); err != nil {
		return errors.Join(err, os.Rename(backup, p.Executable), control.Start())
	}
	failure := control.Start()
	if failure == nil {
		failure = waitHealth(ctx, client, p.HealthURL, p.Expected, p.PreviousInstance, healthTimeout)
	}
	if failure == nil {
		if err := record(ctx, p.Database, p.Expected, nil, time.Now()); err != nil {
			return err
		}
		_ = os.Remove(backup)
		return nil
	}
	// Never restore a database while the failed process can still hold or write it.
	if err := control.Stop(ctx); err != nil {
		return errors.Join(failure, err)
	}
	if err := os.Remove(p.Executable); err != nil {
		return errors.Join(failure, err)
	}
	if err := os.Rename(backup, p.Executable); err != nil {
		return errors.Join(failure, err)
	}
	for _, suffix := range []string{"-wal", "-shm", ""} {
		if err := os.Remove(p.Database + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Join(failure, err)
		}
	}
	if err := copyFile(p.Snapshot, p.Database, 0600); err != nil {
		return errors.Join(failure, err)
	}
	// Write after restoration and before restarting the old process. Its DB schema
	// may predate this updater, so history uses the original update_history columns.
	if err := record(ctx, p.Database, p.Expected, failure, time.Now()); err != nil {
		return errors.Join(failure, err)
	}
	if err := control.Start(); err != nil {
		return errors.Join(failure, err)
	}
	return errors.Join(failure, waitHealth(ctx, client, p.HealthURL, p.Previous, p.PreviousInstance, healthTimeout))
}

func waitHealth(ctx context.Context, client *http.Client, url string, expected Identity, previous string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		b, err := fetch(ctx, client, url, 64<<10)
		if err == nil {
			var h struct{ Status, Version, Commit, InstanceID string }
			if json.Unmarshal(b, &h) == nil && h.Status == "ok" && h.Version == expected.Version &&
				h.Commit == expected.Commit && h.InstanceID != "" && h.InstanceID != previous {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("expected build did not become healthy: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

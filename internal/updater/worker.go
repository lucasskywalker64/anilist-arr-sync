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

const (
	workerAttemptTimeout  = 2 * time.Minute
	workerRecoveryTimeout = 2 * time.Minute
)

// Plan is private staging state shared with a temporary copy of this application.
type Plan struct {
	Expected, Previous                              Identity
	Executable, Next, Database, Snapshot, Directory string
	HealthURL, PreviousInstance, Service, Lock      string
	Args                                            []string
	ParentPID                                       int
	UserService                                     bool
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

// ErrWorkerActive prevents preparation cleanup when cancellation cannot be confirmed.
var ErrWorkerActive = errors.New("update worker may still be active; staging retained")

// SpawnWorker starts a detached copy and commits the handoff after readiness.
// The result reports whether the worker owns stopping a registered service.
func SpawnWorker(ctx context.Context, path, exe string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var plan Plan
	if err := json.Unmarshal(b, &plan); err != nil {
		return false, err
	}
	worker := filepath.Join(filepath.Dir(path), "worker"+filepath.Ext(exe))
	if err := copyFile(exe, worker, 0700); err != nil {
		return false, err
	}
	c := workerCommand(plan, worker, path)
	detach(c)
	log, err := os.OpenFile(filepath.Join(filepath.Dir(path), "worker.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return false, err
	}
	defer func() { _ = log.Close() }()
	c.Stdout, c.Stderr = log, log
	if err := c.Start(); err != nil {
		return false, err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	exited := func(cause error) (bool, error) {
		// systemd-run waits for the worker but has its own PID. Require both the
		// worker's safe-completion acknowledgement and its exit before cleanup.
		pid := completedWorker(filepath.Dir(path))
		if pid > 0 {
			waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if waitProcess(waitCtx, pid) == nil {
				return false, cause
			}
		}
		return false, errors.Join(cause, ErrWorkerActive)
	}
	cancelWorker := func(cause error) (bool, error) {
		cancelErr := os.WriteFile(path+".cancel", nil, 0600)
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case err := <-done:
			return exited(errors.Join(cause, cancelErr, err))
		case <-timer.C:
			return false, errors.Join(cause, cancelErr, ErrWorkerActive)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(path + ".ready"); err == nil {
			if err := ctx.Err(); err != nil {
				return cancelWorker(err)
			}
			if err := os.WriteFile(path+".go", nil, 0600); err != nil {
				return cancelWorker(err)
			}
			return plan.Service != "", nil
		}
		select {
		case err := <-done:
			return exited(fmt.Errorf("update worker exited before readiness: %v", err))
		case <-ctx.Done():
			// The worker owns recovery configuration. Ask it to exit cleanly rather
			// than killing it while the service's recovery actions are disabled.
			return cancelWorker(ctx.Err())
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
	control, err := controller(p)
	if err != nil {
		return errors.Join(err, finishAttempt(path, p))
	}
	return runWorker(path, p, control, 30*time.Second, &http.Client{Timeout: 2 * time.Second})
}

func runWorker(path string, p Plan, control processControl, healthTimeout time.Duration, client *http.Client) (result error) {
	ctx, cancel := context.WithTimeout(context.Background(), workerAttemptTimeout)
	defer cancel()
	releaseLock := true
	defer func() {
		if releaseLock {
			result = errors.Join(result, finishAttempt(path, p))
		}
	}()
	restore := func() error {
		err := control.Restore()
		if err != nil {
			releaseLock = false
		}
		return err
	}
	if err := control.Prepare(); err != nil {
		return errors.Join(err, restore())
	}
	defer func() { _ = os.Remove(path + ".ready") }()
	defer func() { _ = os.Remove(path + ".cancel") }()
	if err := os.WriteFile(path+".ready", nil, 0600); err != nil {
		return errors.Join(err, restore())
	}
	// The parent commits the handoff only after SpawnWorker returns successfully.
	for {
		if _, err := os.Stat(path + ".cancel"); err == nil {
			return restore()
		}
		if _, err := os.Stat(path + ".go"); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), restore())
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer func() { _ = os.Remove(path + ".go") }()
	releaseLock = false
	recoverService := func(cause error) error {
		recovered, recoveryErr := recoverStoppedService(p, control, healthTimeout, client)
		releaseLock = recovered
		return errors.Join(cause, recoveryErr, restore())
	}
	if p.Service != "" {
		if err := control.Stop(ctx); err != nil {
			return recoverService(err)
		}
	}
	if err := waitProcess(ctx, p.ParentPID); err != nil {
		if p.Service != "" {
			return recoverService(err)
		}
		return errors.Join(err, restore())
	}
	completed, err := execute(ctx, p, control, healthTimeout, client)
	releaseLock = completed
	return err
}

// execute runs only after the previous process has released its executable and DB.
func execute(ctx context.Context, p Plan, control processControl, healthTimeout time.Duration, client *http.Client) (completed bool, result error) {
	defer func() {
		err := control.Restore()
		if err != nil {
			completed = false
		}
		result = errors.Join(result, err)
	}()
	abort := func(cause error) (bool, error) {
		recoveryCtx, cancel := context.WithTimeout(context.Background(), workerRecoveryTimeout)
		defer cancel()
		recovered, err := restartPrevious(recoveryCtx, p, control, healthTimeout, client)
		return recovered, errors.Join(cause, err)
	}
	// Refresh the online preparation snapshot after shutdown so accepted writes
	// between preparation and shutdown survive rollback. This opens no migrations.
	if err := os.Remove(p.Snapshot); err != nil {
		return abort(err)
	}
	db, err := openDatabase(p.Database)
	if err != nil {
		return abort(err)
	}
	_, err = db.ExecContext(ctx, `VACUUM INTO ?`, p.Snapshot)
	err = errors.Join(err, db.Close())
	if err != nil {
		return abort(err)
	}
	backup, replacement := p.Executable+".previous", p.Executable+".next"
	if err := copyFile(p.Next, replacement, 0755); err != nil {
		return abort(err)
	}
	defer func() { _ = os.Remove(replacement) }()
	if err := os.Rename(p.Executable, backup); err != nil {
		return abort(err)
	}
	if err := os.Rename(replacement, p.Executable); err != nil {
		if restoreErr := os.Rename(backup, p.Executable); restoreErr != nil {
			return false, errors.Join(err, restoreErr)
		}
		return abort(err)
	}
	failure := control.Start()
	if failure == nil {
		failure = waitHealth(ctx, client, p.HealthURL, p.Expected, p.PreviousInstance, healthTimeout)
	}
	if failure == nil {
		recordErr := record(ctx, p.Database, p.Expected, nil, time.Now())
		_ = os.Remove(backup)
		return true, recordErr
	}
	// Never restore a database while the failed process can still hold or write it.
	ctx, cancel := context.WithTimeout(context.Background(), workerRecoveryTimeout)
	defer cancel()
	if err := control.Stop(ctx); err != nil {
		return false, errors.Join(failure, err)
	}
	if err := os.Remove(p.Executable); err != nil {
		return false, errors.Join(failure, err)
	}
	if err := os.Rename(backup, p.Executable); err != nil {
		return false, errors.Join(failure, err)
	}
	for _, suffix := range []string{"-wal", "-shm", ""} {
		if err := os.Remove(p.Database + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, errors.Join(failure, err)
		}
	}
	if err := copyFile(p.Snapshot, p.Database, 0600); err != nil {
		return false, errors.Join(failure, err)
	}
	// Write after restoration and before restarting the old process. Its DB schema
	// may predate this updater, so history uses the original update_history columns.
	recordErr := record(ctx, p.Database, p.Expected, failure, time.Now())
	if err := control.Start(); err != nil {
		return false, errors.Join(failure, recordErr, err)
	}
	healthErr := waitHealth(ctx, client, p.HealthURL, p.Previous, p.PreviousInstance, healthTimeout)
	return recordErr == nil && healthErr == nil, errors.Join(failure, recordErr, healthErr)
}

func recoverStoppedService(p Plan, control processControl, healthTimeout time.Duration, client *http.Client) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), workerRecoveryTimeout)
	defer cancel()
	service, ok := control.(interface {
		Stopped(context.Context) (bool, error)
	})
	if !ok {
		return false, errors.New("cannot confirm service stopped")
	}
	stopped, err := service.Stopped(ctx)
	if err != nil || !stopped {
		return false, errors.Join(errors.New("cannot confirm service stopped"), err)
	}
	if p.ParentPID > 0 {
		if err := waitProcess(ctx, p.ParentPID); err != nil {
			return false, err
		}
	}
	return restartPrevious(ctx, p, control, healthTimeout, client)
}

func restartPrevious(ctx context.Context, p Plan, control processControl, healthTimeout time.Duration, client *http.Client) (bool, error) {
	if err := control.Start(); err != nil {
		return false, err
	}
	err := waitHealth(ctx, client, p.HealthURL, p.Previous, p.PreviousInstance, healthTimeout)
	return err == nil, err
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

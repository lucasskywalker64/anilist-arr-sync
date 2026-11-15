//go:build !windows

package updater

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func workerCommand(p Plan, worker, path string) *exec.Cmd {
	if p.Service != "" {
		return systemdWorkerCommand(p, worker, path)
	}
	return exec.Command(worker, "--update-worker", path)
}
func currentService() (string, bool, error) {
	b, err := os.ReadFile("/proc/self/cgroup")
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	unit, user := systemdOwnership(string(b))
	return unit, user, nil
}
func controller(p Plan) (processControl, error) {
	if p.Service != "" {
		return &systemdProcess{plan: p}, nil
	}
	return &directProcess{plan: p}, nil
}
func waitProcess(ctx context.Context, pid int) error {
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

//go:build !windows

package updater

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func detach(c *exec.Cmd)                        { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func currentService() (string, error)           { return "", nil }
func controller(p Plan) (processControl, error) { return &directProcess{plan: p}, nil }
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

package updater

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/winservice"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS, HideWindow: true}
}

func workerCommand(_ Plan, worker, path string) *exec.Cmd {
	return exec.Command(worker, "--update-worker", path)
}

func waitProcess(ctx context.Context, pid int) error {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	for {
		state, err := windows.WaitForSingleObject(h, 50)
		if err != nil {
			return err
		}
		if state == windows.WAIT_OBJECT_0 {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func currentService() (string, bool, error) {
	return winservice.Name(), false, nil
}

type serviceAPI interface {
	RecoveryActions() ([]mgr.RecoveryAction, error)
	ResetPeriod() (uint32, error)
	RecoveryActionsOnNonCrashFailures() (bool, error)
	SetRecoveryActions([]mgr.RecoveryAction, uint32) error
	ResetRecoveryActions() error
	SetRecoveryActionsOnNonCrashFailures(bool) error
	Control(svc.Cmd) (svc.Status, error)
	Query() (svc.Status, error)
	Start(...string) error
}

type serviceProcess struct {
	service            serviceAPI
	close              func()
	actions            []mgr.RecoveryAction
	reset              uint32
	nonCrash, prepared bool
}

func controller(p Plan) (processControl, error) {
	if p.Service == "" {
		return &directProcess{plan: p}, nil
	}
	m, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	s, err := m.OpenService(p.Service)
	if err != nil {
		_ = m.Disconnect()
		return nil, err
	}
	return &serviceProcess{service: s, close: func() { _ = s.Close(); _ = m.Disconnect() }}, nil
}

func (p *serviceProcess) Prepare() (err error) {
	p.actions, err = p.service.RecoveryActions()
	if err != nil {
		return err
	}
	p.reset, err = p.service.ResetPeriod()
	if err != nil {
		return err
	}
	p.nonCrash, err = p.service.RecoveryActionsOnNonCrashFailures()
	if err != nil {
		return err
	}
	if err = p.service.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.NoAction}}, p.reset); err != nil {
		return err
	}
	p.prepared = true
	if err = p.service.SetRecoveryActionsOnNonCrashFailures(false); err != nil {
		return err
	}
	return nil
}
func (p *serviceProcess) Restore() error {
	defer p.close()
	if !p.prepared {
		return nil
	}
	var err error
	if len(p.actions) == 0 {
		err = p.service.ResetRecoveryActions()
	} else {
		err = p.service.SetRecoveryActions(p.actions, p.reset)
	}
	return errors.Join(err, p.service.SetRecoveryActionsOnNonCrashFailures(p.nonCrash))
}
func (p *serviceProcess) Start() error { return p.service.Start() }
func (p *serviceProcess) Stop(ctx context.Context) error {
	status, err := p.service.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Stopped {
		return nil
	}
	pid := status.ProcessId
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		if status.State == svc.Running || status.State == svc.Paused {
			if _, err := p.service.Control(svc.Stop); err != nil &&
				!errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) && !errors.Is(err, windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL) {
				return err
			}
		}
		status, err = p.service.Query()
		if err != nil {
			return err
		}
		if status.ProcessId != 0 {
			pid = status.ProcessId
		}
		if status.State == svc.Stopped {
			if pid != 0 {
				return waitProcess(ctx, int(pid))
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if pid == 0 {
				return errors.New("service did not stop and has no process ID")
			}
			h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
			if err != nil {
				return err
			}
			err = windows.TerminateProcess(h, 1)
			_ = windows.CloseHandle(h)
			if err != nil {
				return err
			}
			return waitProcess(ctx, int(pid))
		case <-time.After(50 * time.Millisecond):
		}
	}
}

package updater

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func systemdOwnership(cgroups string) (string, bool) {
	for _, line := range strings.Split(cgroups, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || (parts[0] != "0" && !strings.Contains(parts[1], "name=systemd")) {
			continue
		}
		components := strings.Split(parts[2], "/")
		for i := len(components) - 1; i >= 0; i-- {
			name := components[i]
			if strings.HasSuffix(name, ".scope") {
				break
			}
			if !strings.HasSuffix(name, ".service") {
				continue
			}
			if strings.HasPrefix(name, "user@") {
				break
			}
			user := false
			for _, parent := range components[:i] {
				if strings.HasPrefix(parent, "user@") && strings.HasSuffix(parent, ".service") {
					user = true
				}
			}
			return name, user
		}
	}
	return "", false
}

func systemdWorkerCommand(p Plan, worker, path string) *exec.Cmd {
	var args []string
	if p.UserService {
		args = append(args, "--user")
	}
	// A scope moves the worker outside the application's unit while inheriting
	// the caller's cwd, environment and credentials, without putting secrets in argv.
	args = append(args, "--scope", "--quiet", "--collect", "--no-ask-password", "--unit=anilist-arr-sync-"+filepath.Base(filepath.Dir(path))+".scope", "--", worker, "--update-worker", path)
	return exec.Command("systemd-run", args...)
}

type systemdProcess struct{ plan Plan }

func (*systemdProcess) Prepare() error { return nil }
func (*systemdProcess) Restore() error { return nil }
func (p *systemdProcess) control(ctx context.Context, action string) error {
	var args []string
	if p.plan.UserService {
		args = append(args, "--user")
	}
	args = append(args, action, "--", p.plan.Service)
	if err := exec.CommandContext(ctx, "systemctl", args...).Run(); err != nil {
		return fmt.Errorf("systemd %s: %w", action, err)
	}
	return nil
}
func (p *systemdProcess) Stop(ctx context.Context) error { return p.control(ctx, "stop") }
func (p *systemdProcess) Stopped(ctx context.Context) (bool, error) {
	var args []string
	if p.plan.UserService {
		args = append(args, "--user")
	}
	args = append(args, "show", "--property=ActiveState", "--value", "--", p.plan.Service)
	b, err := exec.CommandContext(ctx, "systemctl", args...).Output()
	state := strings.TrimSpace(string(b))
	return err == nil && (state == "inactive" || state == "failed"), err
}
func (p *systemdProcess) Start() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return p.control(ctx, "start")
}

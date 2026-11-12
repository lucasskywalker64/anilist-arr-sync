package updater

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestSystemdOwnership(t *testing.T) {
	for _, tc := range []struct {
		group, unit string
		user        bool
	}{
		{"0::/system.slice/anilist-arr-sync.service", "anilist-arr-sync.service", false},
		{"1:name=systemd:/system.slice/anilist-arr-sync.service", "anilist-arr-sync.service", false},
		{"0::/user.slice/user-1000.slice/user@1000.service/app.slice/anilist-arr-sync.service", "anilist-arr-sync.service", true},
		{"0::/user.slice/user-1000.slice/user@1000.service/app.slice/session.scope", "", false},
		{"0::/system.slice/anilist-arr-sync.service/delegated", "anilist-arr-sync.service", false},
		{"0::/", "", false},
	} {
		unit, user := systemdOwnership(tc.group)
		if unit != tc.unit || user != tc.user {
			t.Fatalf("ownership of %s: %s %v", tc.group, unit, user)
		}
	}
}

func TestSystemdWorkerScope(t *testing.T) {
	p := Plan{Service: "anilist-arr-sync.service", UserService: true}
	command := systemdWorkerCommand(p, "/staging/worker", "/staging/update-fixture/plan.json")
	want := []string{"systemd-run", "--user", "--scope", "--quiet", "--collect", "--no-ask-password", "--unit=anilist-arr-sync-update-fixture.scope", "--", "/staging/worker", "--update-worker", "/staging/update-fixture/plan.json"}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("scope launch: %v", command.Args)
	}
}

func TestSystemdStopsAndStartsOriginalUnit(t *testing.T) {
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "systemctl"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := copyFile(self, filepath.Join(dir, name), 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "commands")
	t.Setenv("ANILIST_ARR_SYNC_SYSTEMCTL_LOG", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	p := &systemdProcess{plan: Plan{Service: "anilist-arr-sync.service", UserService: true}}
	if err := p.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"inactive", "failed", "active", "activating", "deactivating", ""} {
		t.Setenv("ANILIST_ARR_SYNC_SYSTEMCTL_STATE", state)
		stopped, err := p.Stopped(context.Background())
		if err != nil || stopped != (state == "inactive" || state == "failed") {
			t.Fatalf("stopped state %q: %v, %v", state, stopped, err)
		}
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "--user stop -- anilist-arr-sync.service\n--user start -- anilist-arr-sync.service\n" +
		strings.Repeat("--user show --property=ActiveState --value -- anilist-arr-sync.service\n", 6)
	if string(b) != want {
		t.Fatalf("service commands: %s", b)
	}
}

func fixtureSystemctl() bool {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") != "systemctl" {
		return false
	}
	f, err := os.OpenFile(os.Getenv("ANILIST_ARR_SYNC_SYSTEMCTL_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	_, err = f.WriteString(strings.Join(os.Args[1:], " ") + "\n")
	if err != nil {
		os.Exit(2)
	}
	if err := f.Close(); err != nil {
		os.Exit(2)
	}
	if _, err := os.Stdout.WriteString(os.Getenv("ANILIST_ARR_SYNC_SYSTEMCTL_STATE") + "\n"); err != nil {
		os.Exit(2)
	}
	return true
}

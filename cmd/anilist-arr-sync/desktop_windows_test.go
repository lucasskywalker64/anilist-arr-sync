package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"golang.org/x/sys/windows"
)

func TestWindowsBrowserLaunch(t *testing.T) {
	for _, tc := range []struct {
		name               string
		enabled, noBrowser bool
		out                io.Writer
		want               bool
	}{
		{"desktop without terminal", true, false, os.Stdout, true},
		{"disabled in config", false, false, os.Stdout, false},
		{"no-browser flag", true, true, os.Stdout, false},
		{"service", true, false, io.Discard, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.NewDefault()
			cfg.Port, cfg.BindAddress = freePort(t), "127.0.0.1"
			cfg.LaunchBrowser = tc.enabled
			if err := config.Save(filepath.Join(dir, "config.xml"), cfg); err != nil {
				t.Fatal(err)
			}
			previous := launchBrowser
			t.Cleanup(func() { launchBrowser = previous })
			var opened string
			launchBrowser = func(url string) error { opened = url; return nil }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			args := []string{"--data-dir", dir}
			if tc.noBrowser {
				args = append(args, "--no-browser")
			}
			if err := runWithReady(ctx, args, tc.out, cancel); err != nil {
				t.Fatal(err)
			}
			want := ""
			if tc.want {
				want = fmt.Sprintf("http://127.0.0.1:%d/", cfg.Port)
			}
			if opened != want {
				t.Fatalf("opened %q, want %q", opened, want)
			}
		})
	}
}

func TestWindowsExecutable(t *testing.T) {
	if exe := os.Getenv("ANILIST_CONSOLE_FIXTURE"); exe != "" {
		// This helper has its own hidden console, independent of the test runner.
		for _, tc := range []struct {
			args []string
			want string
			fail bool
		}{
			{[]string{"--version"}, "anilist-arr-sync dev", false},
			{[]string{"--help"}, "Run one sync and exit", false},
			{[]string{"--sync", "--data-dir", t.TempDir()}, "AniList", true},
			{[]string{"--port", "0"}, "port must be between", true},
		} {
			output, err := exec.Command(exe, tc.args...).CombinedOutput()
			if (err != nil) != tc.fail || !strings.Contains(string(output), tc.want) {
				t.Fatalf("%v: %v, output: %s", tc.args, err, output)
			}
		}
		cmd := exec.Command(exe, "--version")
		cmd.SysProcAttr = &syscall.SysProcAttr{NoInheritHandles: true}
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		console, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = console.Close() }()
		var text [4096]uint16
		var read uint32
		ok, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("ReadConsoleOutputCharacterW").Call(console.Fd(), uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)), 0, uintptr(unsafe.Pointer(&read)))
		if ok == 0 || !strings.Contains(windows.UTF16ToString(text[:read]), "anilist-arr-sync dev") {
			t.Fatalf("CLI did not attach to parent console: %v", err)
		}

		dir := t.TempDir()
		port := freePort(t)
		cmd = exec.Command(exe, "--no-browser", "--data-dir", dir, "--bind-address", "127.0.0.1", "--port", fmt.Sprint(port))
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		client := &http.Client{Timeout: time.Second}
		defer client.CloseIdleConnections()
		waitBoot(t, client, fmt.Sprintf("http://127.0.0.1:%d", port), "")
		if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid)); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("console interrupt: %v", err)
			}
		case <-time.After(16 * time.Second):
			t.Fatal("console interrupt did not drain the runtime")
		}
		return
	}
	exe := filepath.Join(t.TempDir(), "anilist-arr-sync.exe")
	build := exec.Command("go", "build", "-ldflags=-H windowsgui", "-o", exe, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, output)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsExecutable$", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "ANILIST_CONSOLE_FIXTURE="+exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE, HideWindow: true}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("console fixture: %v: %s", err, output)
	}
}

//go:build !windows

package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestCLISignalTermination(t *testing.T) {
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.NewDefault()
			cfg.Port, cfg.BindAddress = freePort(t), "127.0.0.1"
			if err := config.Save(filepath.Join(dir, "config.xml"), cfg); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestCLIProcess$", "--", "--data-dir", dir, "--no-browser")
			cmd.Env = append(os.Environ(), "ANILIST_TEST_PROCESS=1")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			client := &http.Client{Timeout: time.Second}
			defer client.CloseIdleConnections()
			waitBoot(t, client, fmt.Sprintf("http://127.0.0.1:%d", cfg.Port), "")
			if err := cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(16 * time.Second):
				t.Fatal("signal did not terminate the runtime")
			}
			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
			if err != nil {
				t.Fatal(err)
			}
			_ = listener.Close()
		})
	}
}

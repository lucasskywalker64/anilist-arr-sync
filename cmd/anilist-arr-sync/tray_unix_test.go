//go:build !windows

package main

import (
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestTrayInitializationFailureDoesNotSignalReady(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ready := false
	err = runWithReady(context.Background(), []string{
		"--tray", "--no-browser", "--data-dir", t.TempDir(),
		"--bind-address", "127.0.0.1", "--port", strconv.Itoa(port),
	}, io.Discard, func() { ready = true })
	if err == nil || !strings.Contains(err.Error(), "--tray is supported only on Windows") {
		t.Fatalf("expected unsupported tray initialization to fail: %v", err)
	}
	if ready {
		t.Fatal("application signalled readiness before tray initialization failed")
	}
}

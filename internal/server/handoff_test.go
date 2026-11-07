package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func handoffPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestHTTPSRestartHandoffReportsNewHTTPInstance(t *testing.T) {
	cfg := config.NewDefault()
	cfg.BindAddress, cfg.Port, cfg.SslPort, cfg.EnableSsl = "127.0.0.1", handoffPort(t), handoffPort(t), true
	cfg.APIKey, cfg.URLBase = "handoff-key", "/old"
	cfg.SslCertPath, cfg.SslKeyPath = generateSelfSignedCert(t, t.TempDir())
	cert, err := os.ReadFile(cfg.SslCertPath)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		t.Fatal("invalid test certificate")
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	defer client.CloseIdleConnections()
	srv := NewServer(cfg, "", nil, nil, nil)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	base := fmt.Sprintf("https://127.0.0.1:%d", cfg.SslPort)
	request := func(method, path, body string, authenticated bool) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		if authenticated {
			req.Header.Set("X-Api-Key", "handoff-key")
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		var payload map[string]any
		if res.StatusCode == 200 {
			if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode, payload
	}
	code, _ := request("PUT", "/old/api/v1/config", `{"enableSsl":false,"urlBase":"/new"}`, true)
	if code != 200 {
		t.Fatalf("save SSL setting: %d", code)
	}
	code, result := request("POST", "/old/api/v1/system/restart", "", true)
	if code != 200 {
		t.Fatalf("restart: %d", code)
	}
	statusURL, _ := result["statusUrl"].(string)
	if statusURL == "" {
		t.Fatal("HTTPS downgrade must return an HTTPS handoff status URL")
	}
	_, duplicate := request("POST", "/old/api/v1/system/restart", "", true)
	if duplicate["statusUrl"] != statusURL {
		t.Fatal("duplicate restart created a second handoff")
	}
	handoff := srv.RestartHandoff()
	if handoff == nil {
		t.Fatal("runtime owner did not receive the handoff")
	}
	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := handoff.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer handoff.Close()
	code, status := request("GET", statusURL, "", false)
	if code != 200 || status["status"] != "starting" {
		t.Fatalf("handoff before ready: %d %+v", code, status)
	}
	for _, probe := range []struct {
		method, path string
		code         int
	}{{"GET", "/old/api/v1/config", 404}, {"POST", statusURL, 405}, {"GET", statusURL + "wrong", 404}} {
		if code, _ := request(probe.method, probe.path, "", false); code != probe.code {
			t.Fatalf("handoff %s %s: %d", probe.method, probe.path, code)
		}
	}
	nextCfg := srv.Config()
	next := NewServer(&nextCfg, "", nil, nil, nil)
	if err := next.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = next.Shutdown(context.Background()) }()
	if err := handoff.Ready(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	code, status = request("GET", statusURL, "", false)
	if code != 200 || status["status"] != "ready" || status["instanceId"] == result["instanceId"] {
		t.Fatalf("handoff did not verify the new instance: %d %+v", code, status)
	}
	client.CloseIdleConnections()
	cancel()
	waitCtx, stopWaiting := context.WithTimeout(t.Context(), time.Second)
	defer stopWaiting()
	handoff.Wait(waitCtx)
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.SslPort))
	if err != nil {
		t.Fatalf("handoff listener was not released: %v", err)
	}
	_ = listener.Close()
}

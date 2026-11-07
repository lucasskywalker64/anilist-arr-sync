package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestRegularRestartPreservesSessionsAndRevocation(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewDefault()
	cfg.Port, cfg.BindAddress, cfg.APIKey = freePort(t), "127.0.0.1", "runtime-key"
	cfg.AuthenticationRequired = "Enabled"
	if err := config.Save(filepath.Join(dir, "config.xml"), cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"--data-dir", dir, "--no-browser"}, io.Discard) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("runtime did not stop")
		}
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	base := fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
	id := waitBoot(t, client, base, "")
	csrf := ""
	request := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-CSRF-Token", csrf)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		var payload map[string]any
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, payload
	}
	code, setup := request("POST", "/api/v1/auth/setup", `{"username":"admin","password":"Restart-Test-Only-2026"}`)
	if code != 201 {
		t.Fatalf("setup: %d", code)
	}
	csrf, _ = setup["csrfToken"].(string)
	if csrf == "" {
		t.Fatal("setup did not return a CSRF token")
	}
	for i := 0; i < 2; i++ {
		if code, _ := request("POST", "/api/v1/system/restart", ""); code != 200 {
			t.Fatalf("restart: %d", code)
		}
		id = waitBoot(t, client, base, id)
		code, user := request("GET", "/api/v1/auth/user", "")
		if code != 200 || user["username"] != "admin" {
			t.Fatalf("regular restart lost the session: %d", code)
		}
	}
	if code, _ := request("PUT", "/api/v1/config", `{"username":"renamed"}`); code != 200 {
		t.Fatalf("change credentials: %d", code)
	}
	if code, _ := request("GET", "/api/v1/auth/user", ""); code != 401 {
		t.Fatal("credential change did not revoke the session")
	}
	apiCall(t, client, "POST", base+"/api/v1/system/restart", "")
	id = waitBoot(t, client, base, id)
	if code, _ := request("GET", "/api/v1/auth/user", ""); code != 401 {
		t.Fatal("restart resurrected a revoked session")
	}
	code, login := request("POST", "/api/v1/auth/login", `{"username":"renamed","password":"Restart-Test-Only-2026"}`)
	if code != 200 {
		t.Fatalf("login after credential change: %d", code)
	}
	csrf, _ = login["csrfToken"].(string)
	if code, _ := request("POST", "/api/v1/auth/logout", ""); code != 200 {
		t.Fatalf("logout: %d", code)
	}
	apiCall(t, client, "POST", base+"/api/v1/system/restart", "")
	waitBoot(t, client, base, id)
	if code, _ := request("GET", "/api/v1/auth/user", ""); code != 401 {
		t.Fatal("restart resurrected a logged-out session")
	}
}

func TestRuntimeHTTPSDowngradeHandoff(t *testing.T) {
	t.Run("ready", func(t *testing.T) { testRuntimeHTTPSDowngradeHandoff(t, false) })
	t.Run("startup failure", func(t *testing.T) { testRuntimeHTTPSDowngradeHandoff(t, true) })
}

func testRuntimeHTTPSDowngradeHandoff(t *testing.T, failStartup bool) {
	dir := t.TempDir()
	certificate := httptest.NewTLSServer(http.NotFoundHandler())
	tlsCert := certificate.TLS.Certificates[0]
	certificate.Close()
	key, err := x509.MarshalPKCS8PrivateKey(tlsCert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.NewDefault()
	cfg.BindAddress, cfg.Port, cfg.SslPort = "127.0.0.1", freePort(t), freePort(t)
	cfg.APIKey, cfg.EnableSsl, cfg.URLBase = "runtime-key", true, "/before"
	cfg.AuthenticationRequired = "Enabled"
	cfg.SslCertPath, cfg.SslKeyPath = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(cfg.SslCertPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsCert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.SslKeyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(filepath.Join(dir, "config.xml"), cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"--data-dir", dir, "--no-browser"}, io.Discard) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !failStartup {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("runtime did not stop")
		}
	})
	roots := x509.NewCertPool()
	cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(cert)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12}}}
	defer client.CloseIdleConnections()
	base := fmt.Sprintf("https://127.0.0.1:%d", cfg.SslPort)
	id := waitBoot(t, client, base+"/before", "")
	setup, err := client.Post(base+"/before/api/v1/auth/setup", "application/json", strings.NewReader(`{"username":"admin","password":"Handoff-Test-Only-2026"}`))
	if err != nil {
		t.Fatal(err)
	}
	if setup.StatusCode != 201 {
		t.Fatalf("setup: %d", setup.StatusCode)
	}
	var session *http.Cookie
	for _, cookie := range setup.Cookies() {
		if cookie.Name == "anilist_sync_session" {
			session = cookie
		}
	}
	_ = setup.Body.Close()
	if session == nil {
		t.Fatal("missing session cookie")
	}
	sessionStatus := func(address string, cookie *http.Cookie) int {
		t.Helper()
		req, err := http.NewRequest("GET", address+"/api/v1/auth/user", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(cookie)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		return response.StatusCode
	}
	apiCall(t, client, "POST", base+"/before/api/v1/system/restart", "")
	id = waitBoot(t, client, base+"/before", id)
	if code := sessionStatus(base+"/before", session); code != 200 {
		t.Fatalf("regular HTTPS restart lost the session: %d", code)
	}
	settings := `{"enableSsl":false,"urlBase":"/after"}`
	if failStartup {
		blocked, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = blocked.Close() }()
		settings = fmt.Sprintf(`{"enableSsl":false,"urlBase":"/after","port":%d}`, blocked.Addr().(*net.TCPAddr).Port)
	}
	if code, _ := apiCall(t, client, "PUT", base+"/before/api/v1/config", settings); code != 200 {
		t.Fatalf("save settings: %d", code)
	}
	code, result := apiCall(t, client, "POST", base+"/before/api/v1/system/restart", "")
	statusPath, _ := result["statusUrl"].(string)
	if code != 200 || statusPath == "" {
		t.Fatalf("missing handoff URL: %d", code)
	}
	deadline := time.Now().Add(5 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		response, err := client.Get(base + statusPath)
		if err == nil {
			var status struct{ Status, InstanceID string }
			_ = json.NewDecoder(response.Body).Decode(&status)
			_ = response.Body.Close()
			if failStartup && status.Status == "failed" {
				ready = true
				break
			}
			if !failStartup && status.Status == "ready" && status.InstanceID != "" && status.InstanceID != id {
				ready = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("HTTPS handoff did not report the restart outcome")
	}
	if failStartup {
		return
	}
	newBase := fmt.Sprintf("http://127.0.0.1:%d/after", cfg.Port)
	id = waitBoot(t, client, newBase, id)
	if code := sessionStatus(newBase, session); code != 401 {
		t.Fatal("protocol change retained an old session")
	}
	login, err := client.Post(newBase+"/api/v1/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"Handoff-Test-Only-2026"}`))
	if err != nil {
		t.Fatal(err)
	}
	if login.StatusCode != 200 {
		_ = login.Body.Close()
		t.Fatalf("HTTP login: %d", login.StatusCode)
	}
	session = nil
	for _, cookie := range login.Cookies() {
		if cookie.Name == "anilist_sync_session" {
			session = cookie
		}
	}
	_ = login.Body.Close()
	if session == nil || session.Secure {
		t.Fatal("HTTP login did not issue a non-Secure session cookie")
	}
	if code := sessionStatus(newBase, session); code != 200 {
		t.Fatalf("new HTTP session was not valid before enabling HTTPS: %d", code)
	}
	// Re-enable TLS immediately. The previous handoff must release its port.
	if code, _ := apiCall(t, client, "PUT", newBase+"/api/v1/config", `{"enableSsl":true}`); code != 200 {
		t.Fatalf("enable SSL: %d", code)
	}
	apiCall(t, client, "POST", newBase+"/api/v1/system/restart", "")
	waitBoot(t, client, base+"/after", id)
	if code := sessionStatus(base+"/after", session); code != 401 {
		t.Fatal("enabling HTTPS retained an insecure HTTP session")
	}
}

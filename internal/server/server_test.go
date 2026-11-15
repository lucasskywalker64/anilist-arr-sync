package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func generateSelfSignedCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"AniList Arr Sync Test"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certPath = filepath.Join(dir, "cert.pem")
	certOut, err := os.Create(certPath)
	if err != nil {
		t.Fatalf("failed to create cert file: %v", err)
	}
	defer func() { _ = certOut.Close() }()
	_ = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyPath = filepath.Join(dir, "key.pem")
	keyOut, err := os.Create(keyPath)
	if err != nil {
		t.Fatalf("failed to create key file: %v", err)
	}
	defer func() { _ = keyOut.Close() }()
	b, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("failed to marshal private key: %v", err)
	}
	_ = pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: b})

	return certPath, keyPath
}

func TestURLBaseSubpathPrefix(t *testing.T) {
	cfg := config.NewDefault()
	cfg.URLBase = "/anilist-sync"
	cfg.AuthenticationRequired = "DisabledForLocalAddresses"
	cfg.APIKey = "test-key"

	srv := NewServer(cfg, "", nil, nil, nil)
	handler := srv.Handler()

	// 1. Root /anilist-sync redirects to /anilist-sync/
	{
		req := httptest.NewRequest(http.MethodGet, "/anilist-sync", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMovedPermanently {
			t.Errorf("expected redirect 301, got %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/anilist-sync/" {
			t.Errorf("expected Location /anilist-sync/, got %q", loc)
		}
	}

	// 2. Health check under subpath /anilist-sync/api/v1/health
	{
		req := httptest.NewRequest(http.MethodGet, "/anilist-sync/api/v1/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 under /anilist-sync prefix, got %d", rec.Code)
		}
	}
}

func TestServerLifecycle_StartAndShutdown(t *testing.T) {
	tempDir := t.TempDir()
	certPath, keyPath := generateSelfSignedCert(t, tempDir)

	// Pick random open ports
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on port: %v", err)
	}
	port1 := l1.Addr().(*net.TCPAddr).Port
	_ = l1.Close()

	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on ssl port: %v", err)
	}
	port2 := l2.Addr().(*net.TCPAddr).Port
	_ = l2.Close()

	cfg := config.NewDefault()
	cfg.BindAddress = "127.0.0.1"
	cfg.Port = port1
	cfg.EnableSsl = true
	cfg.SslPort = port2
	cfg.SslCertPath = certPath
	cfg.SslKeyPath = keyPath

	srv := NewServer(cfg, "", nil, nil, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Verify server is listening
	conn, err := net.DialTimeout("tcp", srv.httpServer.Addr, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to HTTP server: %v", err)
	}
	_ = conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("failed to shutdown server: %v", err)
	}
}

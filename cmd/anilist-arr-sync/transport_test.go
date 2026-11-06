package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

func TestHTTPProxyAndLocalBypass(t *testing.T) {
	requests := make(chan string, 4)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Error("proxy credentials missing")
		}
		requests <- r.URL.Host + r.URL.Path
		_, _ = io.WriteString(w, `[]`)
	}))
	defer proxy.Close()
	parsed, _ := url.Parse(proxy.URL)
	port := proxy.Listener.Addr().(*net.TCPAddr).Port
	cfg := config.NewDefault()
	cfg.UseProxy, cfg.ProxyHostname, cfg.ProxyPort = true, parsed.Hostname(), port
	cfg.ProxyUsername, cfg.ProxyPassword = "user", "pass"
	client, err := outboundClient(*cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	sonarr, err := servarr.NewSonarrClient("http://sonarr.example", "key", servarr.WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sonarr.GetTags(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := <-requests; got != "sonarr.example/api/v3/tag" {
		t.Fatal(got)
	}
	provider, err := notification.NewWebhookProvider("http://notify.example/hook", nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Test(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := <-requests; got != "notify.example/hook" {
		t.Fatal(got)
	}
	cfg.ProxyBypassLocalAddresses = true
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "direct") }))
	defer local.Close()
	bypass, err := outboundClient(*cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer bypass.CloseIdleConnections()
	response, err := bypass.Get(local.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "direct" {
		t.Fatalf("local request used proxy: %s", body)
	}
}

func TestSOCKS5Proxy(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		head := make([]byte, 2)
		if _, err := io.ReadFull(conn, head); err != nil {
			done <- err
			return
		}
		methods := make([]byte, head[1])
		if _, err := io.ReadFull(conn, methods); err != nil {
			done <- err
			return
		}
		_, _ = conn.Write([]byte{5, 0})
		request := make([]byte, 5)
		if _, err := io.ReadFull(conn, request); err != nil {
			done <- err
			return
		}
		if request[0] != 5 || request[1] != 1 || request[3] != 3 {
			done <- fmt.Errorf("unexpected SOCKS request %v", request)
			return
		}
		host := make([]byte, request[4])
		_, _ = io.ReadFull(conn, host)
		_, _ = io.ReadFull(conn, head)
		if string(host) != "anilist.example" || binary.BigEndian.Uint16(head) != 80 {
			done <- fmt.Errorf("SOCKS target: %s:%d", host, binary.BigEndian.Uint16(head))
			return
		}
		_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
		buf := make([]byte, 4096)
		_, err = conn.Read(buf)
		body := `{"data":{"MediaListCollection":{"lists":[],"hasNextChunk":false}}}`
		if err == nil {
			_, err = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
		}
		done <- err
	}()
	cfg := config.NewDefault()
	cfg.UseProxy, cfg.ProxyType, cfg.ProxyHostname = true, "Socks5", "127.0.0.1"
	cfg.ProxyPort = listener.Addr().(*net.TCPAddr).Port
	client, err := outboundClient(*cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	// Exercise the real AniList client through the proxy transport.
	al := anilist.NewClient("http://anilist.example", anilist.WithHTTPClient(client), anilist.WithMaxRetries(0))
	_, err = al.FetchWatchlist(context.Background(), anilist.WatchlistFilter{Username: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

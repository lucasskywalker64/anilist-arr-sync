package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func outboundClient(cfg config.Config) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if cfg.UseProxy {
		scheme := strings.ToLower(cfg.ProxyType)
		switch scheme {
		case "http", "https", "socks5":
		default:
			return nil, fmt.Errorf("unsupported proxy type %q", cfg.ProxyType)
		}
		if strings.TrimSpace(cfg.ProxyHostname) == "" || cfg.ProxyPort < 1 || cfg.ProxyPort > 65535 {
			return nil, fmt.Errorf("proxy requires a hostname and port between 1 and 65535")
		}
		proxyURL := &url.URL{Scheme: scheme, Host: net.JoinHostPort(cfg.ProxyHostname, strconv.Itoa(cfg.ProxyPort))}
		if cfg.ProxyUsername != "" {
			proxyURL.User = url.UserPassword(cfg.ProxyUsername, cfg.ProxyPassword)
		}
		transport.Proxy = func(r *http.Request) (*url.URL, error) {
			host := r.URL.Hostname()
			if cfg.ProxyBypassLocalAddresses {
				ip := net.ParseIP(host)
				if strings.EqualFold(host, "localhost") || !strings.ContainsAny(host, ".:") || (ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast())) {
					return nil, nil
				}
			}
			for _, ignored := range strings.Split(cfg.ProxyIgnoredAddresses, ",") {
				ignored = strings.TrimSpace(ignored)
				if ignored != "" && (strings.EqualFold(host, ignored) || strings.HasSuffix(strings.ToLower(host), "."+strings.ToLower(strings.TrimPrefix(ignored, "*.")))) {
					return nil, nil
				}
			}
			return proxyURL, nil
		}
	}
	return &http.Client{Transport: transport, Timeout: 60 * time.Second}, nil
}

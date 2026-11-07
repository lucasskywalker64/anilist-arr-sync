package main

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func outboundClient(cfg config.Config) (*http.Client, error) {
	proxyURL, err := cfg.ProxyURL()
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if proxyURL != nil {
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

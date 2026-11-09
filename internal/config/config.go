// Package config manages application configuration matching Servarr conventions,
// environment variable overrides, file persistence, and validation.
package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

// Config represents the application configuration matching Servarr conventions.
type Config struct {
	// Server
	BindAddress    string `xml:"BindAddress"`
	Port           int    `xml:"Port"`
	URLBase        string `xml:"UrlBase"`
	AllowedHosts   string `xml:"AllowedHosts"`
	ApplicationURL string `xml:"ApplicationUrl"`
	EnableSsl      bool   `xml:"EnableSsl"`
	SslPort        int    `xml:"SslPort"`
	SslCertPath    string `xml:"SslCertPath"`
	SslKeyPath     string `xml:"SslKeyPath"`

	// Auth
	AuthenticationMethod   string `xml:"AuthenticationMethod"`
	AuthenticationRequired string `xml:"AuthenticationRequired"`
	APIKey                 string `xml:"ApiKey"`
	TrustedNetworks        string `xml:"TrustedNetworks"`

	// Proxy
	UseProxy                  bool   `xml:"UseProxy"`
	ProxyType                 string `xml:"ProxyType"`
	ProxyHostname             string `xml:"ProxyHostname"`
	ProxyPort                 int    `xml:"ProxyPort"`
	ProxyUsername             string `xml:"ProxyUsername"`
	ProxyPassword             string `xml:"ProxyPassword"`
	ProxyIgnoredAddresses     string `xml:"ProxyIgnoredAddresses"`
	ProxyBypassLocalAddresses bool   `xml:"ProxyBypassLocalAddresses"`

	// Log
	LogLevel        string `xml:"LogLevel"`
	ConsoleLogLevel string `xml:"ConsoleLogLevel"`
	LogSizeLimit    int    `xml:"LogSizeLimit"`
	LogRotate       int    `xml:"LogRotate"`

	// App
	InstanceName               string `xml:"InstanceName"`
	WizardCompleted            bool   `xml:"WizardCompleted"`
	LaunchBrowser              bool   `xml:"LaunchBrowser"`
	EnableDesktopNotifications bool   `xml:"EnableDesktopNotifications"`

	// Update
	UpdateBranch        string `xml:"UpdateBranch"`
	UpdateAutomatically bool   `xml:"UpdateAutomatically"`
	UpdateMechanism     string `xml:"UpdateMechanism"`

	// Backup
	BackupFolder    string `xml:"BackupFolder"`
	BackupInterval  int    `xml:"BackupInterval"`
	BackupRetention int    `xml:"BackupRetention"`

	// Storage
	DatabasePath string `xml:"DatabasePath"`

	// AniList
	AniListUsername          string `xml:"AniListUsername"`
	AniListTvStatus          string `xml:"AniListTvStatus"`
	AniListMovieStatus       string `xml:"AniListMovieStatus"`
	AniListIncludeUnreleased bool   `xml:"AniListIncludeUnreleased"`

	// Radarr
	RadarrURL              string `xml:"RadarrUrl"`
	RadarrAPIKey           string `xml:"RadarrApiKey"`
	RadarrQualityProfileID int    `xml:"RadarrQualityProfileId"`
	RadarrRootFolderPath   string `xml:"RadarrRootFolderPath"`
	RadarrMonitorOnAdd     bool   `xml:"RadarrMonitorOnAdd"`
	RadarrSearchOnAdd      bool   `xml:"RadarrSearchOnAdd"`

	// Sonarr
	SonarrURL              string `xml:"SonarrUrl"`
	SonarrAPIKey           string `xml:"SonarrApiKey"`
	SonarrQualityProfileID int    `xml:"SonarrQualityProfileId"`
	SonarrRootFolderPath   string `xml:"SonarrRootFolderPath"`
	SonarrSeriesType       string `xml:"SonarrSeriesType"`
	SonarrMonitorOnAdd     bool   `xml:"SonarrMonitorOnAdd"`
	SonarrSearchOnAdd      bool   `xml:"SonarrSearchOnAdd"`

	// Sync
	TagName             string  `xml:"TagName"`
	SyncIntervalHours   int     `xml:"SyncIntervalHours"`
	DateToleranceDays   int     `xml:"DateToleranceDays"`
	ConfidenceThreshold float64 `xml:"ConfidenceThreshold"`
	UnmonitorDropped    bool    `xml:"UnmonitorDropped"`
	UnmonitorCompleted  bool    `xml:"UnmonitorCompleted"`
	DeleteDropped       bool    `xml:"DeleteDropped"`
	DeleteCompleted     bool    `xml:"DeleteCompleted"`
	UnmonitorWatched    bool    `xml:"UnmonitorWatched"`
	DeleteWatched       bool    `xml:"DeleteWatched"`
	FirstRunDryRun      bool    `xml:"FirstRunDryRun"`
}

// NewDefault returns a Config initialized with documented defaults.
func NewDefault() *Config {
	return &Config{
		BindAddress:                "0.0.0.0",
		Port:                       7171,
		URLBase:                    "",
		AllowedHosts:               "",
		ApplicationURL:             "",
		EnableSsl:                  false,
		SslPort:                    7272,
		SslCertPath:                "",
		SslKeyPath:                 "",
		AuthenticationMethod:       "Forms",
		AuthenticationRequired:     "DisabledForLocalAddresses",
		APIKey:                     "",
		TrustedNetworks:            "127.0.0.1/32,::1/128,172.16.0.0/12,192.168.0.0/16,10.0.0.0/8",
		UseProxy:                   false,
		ProxyType:                  "Http",
		ProxyHostname:              "",
		ProxyPort:                  8080,
		ProxyUsername:              "",
		ProxyPassword:              "",
		ProxyIgnoredAddresses:      "",
		ProxyBypassLocalAddresses:  true,
		LogLevel:                   "Info",
		ConsoleLogLevel:            "Info",
		LogSizeLimit:               1,
		LogRotate:                  5,
		InstanceName:               "AniList Arr Sync",
		LaunchBrowser:              true,
		EnableDesktopNotifications: false,
		UpdateBranch:               "main",
		UpdateAutomatically:        false,
		UpdateMechanism:            "Docker",
		BackupFolder:               "Backups",
		BackupInterval:             1,
		BackupRetention:            28,
		DatabasePath:               "sync.db",
		AniListUsername:            "",
		AniListTvStatus:            "CURRENT",
		AniListMovieStatus:         "CURRENT",
		AniListIncludeUnreleased:   false,
		RadarrURL:                  "",
		RadarrAPIKey:               "",
		RadarrQualityProfileID:     1,
		RadarrRootFolderPath:       "",
		RadarrMonitorOnAdd:         true,
		RadarrSearchOnAdd:          false,
		SonarrURL:                  "",
		SonarrAPIKey:               "",
		SonarrQualityProfileID:     1,
		SonarrRootFolderPath:       "",
		SonarrSeriesType:           "anime",
		SonarrMonitorOnAdd:         true,
		SonarrSearchOnAdd:          false,
		TagName:                    "anilist-sync",
		SyncIntervalHours:          4,
		DateToleranceDays:          14,
		ConfidenceThreshold:        0.60,
		UnmonitorDropped:           false,
		UnmonitorCompleted:         false,
		DeleteDropped:              false,
		DeleteCompleted:            false,
		UnmonitorWatched:           false,
		DeleteWatched:              false,
		FirstRunDryRun:             true,
	}
}

// SyncConfigured reports whether AniList and at least one Servarr connection are configured.
func (c *Config) SyncConfigured() bool {
	return strings.TrimSpace(c.AniListUsername) != "" &&
		((c.SonarrURL != "" && c.SonarrAPIKey != "") || (c.RadarrURL != "" && c.RadarrAPIKey != ""))
}

// Validate verifies that all configuration values fall within allowable ranges and formats.
func (c *Config) Validate() error {
	var errs []error

	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	check(c.Port >= 1 && c.Port <= 65535, "Port must be between 1 and 65535, got %d", c.Port)
	check(c.SslPort >= 1 && c.SslPort <= 65535, "SslPort must be between 1 and 65535, got %d", c.SslPort)

	if c.EnableSsl {
		check(strings.TrimSpace(c.SslCertPath) != "", "SslCertPath cannot be empty when EnableSsl is true")
		check(strings.TrimSpace(c.SslKeyPath) != "", "SslKeyPath cannot be empty when EnableSsl is true")
		check(c.Port != c.SslPort, "Port and SslPort must differ when EnableSsl is true, both are %d", c.Port)
	}

	switch c.AuthenticationMethod {
	case "Forms", "Basic":
	default:
		check(false, "AuthenticationMethod must be Forms or Basic, got %q", c.AuthenticationMethod)
	}

	switch c.AuthenticationRequired {
	case "Enabled", "DisabledForLocalAddresses":
	default:
		check(false, "AuthenticationRequired must be Enabled or DisabledForLocalAddresses, got %q", c.AuthenticationRequired)
	}

	if strings.TrimSpace(c.TrustedNetworks) != "" {
		for _, part := range strings.Split(c.TrustedNetworks, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !strings.Contains(part, "/") {
				if ip := net.ParseIP(part); ip == nil {
					check(false, "TrustedNetworks contains invalid IP address: %q", part)
				}
			} else {
				if _, _, err := net.ParseCIDR(part); err != nil {
					check(false, "TrustedNetworks contains invalid CIDR %q: %v", part, err)
				}
			}
		}
	}

	check(isValidLogLevel(c.LogLevel), "LogLevel must be Trace, Debug, Info, Warn, or Error, got %q", c.LogLevel)
	check(isValidLogLevel(c.ConsoleLogLevel), "ConsoleLogLevel must be Trace, Debug, Info, Warn, or Error, got %q", c.ConsoleLogLevel)
	check(c.LogSizeLimit > 0, "LogSizeLimit must be greater than 0, got %d", c.LogSizeLimit)
	check(c.LogRotate >= 0, "LogRotate cannot be negative, got %d", c.LogRotate)

	switch c.UpdateMechanism {
	case "BuiltIn", "Docker", "Package":
	default:
		check(false, "UpdateMechanism must be BuiltIn, Docker, or Package, got %q", c.UpdateMechanism)
	}

	check(strings.TrimSpace(c.DatabasePath) != "", "DatabasePath cannot be empty")
	if _, err := c.ProxyURL(); err != nil {
		errs = append(errs, err)
	}
	for _, endpoint := range []struct{ field, url string }{
		{"SonarrURL", c.SonarrURL},
		{"RadarrURL", c.RadarrURL},
	} {
		if endpoint.url != "" {
			_, err := servarr.NormalizeBaseURL(endpoint.url)
			check(err == nil, "%s must be a valid HTTP or HTTPS URL", endpoint.field)
		}
	}

	switch c.SonarrSeriesType {
	case "anime", "standard":
	default:
		check(false, "SonarrSeriesType must be anime or standard, got %q", c.SonarrSeriesType)
	}

	for _, selection := range []struct{ field, value string }{
		{"AniListTvStatus", c.AniListTvStatus},
		{"AniListMovieStatus", c.AniListMovieStatus},
	} {
		for _, status := range anilist.ParseListStatuses(selection.value) {
			switch status {
			case anilist.StatusCurrent, anilist.StatusPlanning, anilist.StatusCompleted,
				anilist.StatusDropped, anilist.StatusPaused, anilist.StatusRepeating:
			default:
				check(false, "%s contains invalid AniList status %q", selection.field, status)
			}
		}
	}

	check(c.SyncIntervalHours > 0, "SyncIntervalHours must be greater than 0, got %d", c.SyncIntervalHours)
	check(c.SyncIntervalHours <= int((1<<63-1)/time.Hour), "SyncIntervalHours exceeds maximum duration, got %d", c.SyncIntervalHours)
	check(c.BackupInterval > 0, "BackupInterval must be greater than 0, got %d", c.BackupInterval)
	check(c.BackupInterval <= int((1<<63-1)/(24*time.Hour)), "BackupInterval exceeds maximum duration, got %d", c.BackupInterval)
	check(c.DateToleranceDays >= 0, "DateToleranceDays cannot be negative, got %d", c.DateToleranceDays)
	check(!math.IsNaN(c.ConfidenceThreshold) && c.ConfidenceThreshold >= 0.0 && c.ConfidenceThreshold <= 1.0, "ConfidenceThreshold must be between 0.0 and 1.0, got %f", c.ConfidenceThreshold)

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("configuration validation failed:\n%w", err)
	}

	return nil
}

// ProxyURL validates and builds the configured proxy URL, returning nil when disabled.
func (c *Config) ProxyURL() (*url.URL, error) {
	if !c.UseProxy {
		return nil, nil
	}
	scheme := strings.ToLower(c.ProxyType)
	switch scheme {
	case "http", "https", "socks5":
	default:
		return nil, fmt.Errorf("ProxyType must be Http, Https, or Socks5, got %q", c.ProxyType)
	}
	if strings.TrimSpace(c.ProxyHostname) == "" {
		return nil, errors.New("ProxyHostname cannot be empty when UseProxy is true")
	}
	if c.ProxyPort < 1 || c.ProxyPort > 65535 {
		return nil, fmt.Errorf("ProxyPort must be between 1 and 65535, got %d", c.ProxyPort)
	}
	proxyURL := &url.URL{Scheme: scheme, Host: net.JoinHostPort(c.ProxyHostname, strconv.Itoa(c.ProxyPort))}
	if c.ProxyUsername != "" {
		proxyURL.User = url.UserPassword(c.ProxyUsername, c.ProxyPassword)
	}
	return proxyURL, nil
}

// isValidLogLevel checks whether the given log level string is supported.
func isValidLogLevel(level string) bool {
	switch strings.ToLower(level) {
	case "trace", "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

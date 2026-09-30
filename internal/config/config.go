// Package config manages application configuration matching Servarr conventions,
// environment variable overrides, file persistence, and validation.
package config

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Config represents the application configuration matching Servarr conventions.
type Config struct {
	// Server
	BindAddress string `xml:"BindAddress"`
	Port        int    `xml:"Port"`
	URLBase     string `xml:"UrlBase"`
	EnableSsl   bool   `xml:"EnableSsl"`
	SslPort     int    `xml:"SslPort"`
	SslCertPath string `xml:"SslCertPath"`
	SslKeyPath  string `xml:"SslKeyPath"`

	// Auth
	AuthenticationMethod   string `xml:"AuthenticationMethod"`
	AuthenticationRequired string `xml:"AuthenticationRequired"`
	APIKey                 string `xml:"ApiKey"`

	// Log
	LogLevel        string `xml:"LogLevel"`
	ConsoleLogLevel string `xml:"ConsoleLogLevel"`
	LogSizeLimit    int    `xml:"LogSizeLimit"`
	LogRotate       int    `xml:"LogRotate"`

	// App
	InstanceName               string `xml:"InstanceName"`
	LaunchBrowser              bool   `xml:"LaunchBrowser"`
	EnableDesktopNotifications bool   `xml:"EnableDesktopNotifications"`

	// Update
	UpdateAutomatically bool   `xml:"UpdateAutomatically"`
	UpdateMechanism     string `xml:"UpdateMechanism"`

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
	RadarrSearchOnAdd      bool   `xml:"RadarrSearchOnAdd"`

	// Sonarr
	SonarrURL              string `xml:"SonarrUrl"`
	SonarrAPIKey           string `xml:"SonarrApiKey"`
	SonarrQualityProfileID int    `xml:"SonarrQualityProfileId"`
	SonarrRootFolderPath   string `xml:"SonarrRootFolderPath"`
	SonarrSeriesType       string `xml:"SonarrSeriesType"`
	SonarrSearchOnAdd      bool   `xml:"SonarrSearchOnAdd"`

	// Sync
	TagName             string  `xml:"TagName"`
	SyncIntervalHours   int     `xml:"SyncIntervalHours"`
	DateToleranceDays   int     `xml:"DateToleranceDays"`
	ConfidenceThreshold float64 `xml:"ConfidenceThreshold"`
	UnmonitorDropped    bool    `xml:"UnmonitorDropped"`
	FirstRunDryRun      bool    `xml:"FirstRunDryRun"`
}

// NewDefault returns a Config initialized with documented defaults.
func NewDefault() *Config {
	return &Config{
		BindAddress:                "0.0.0.0",
		Port:                       7171,
		URLBase:                    "",
		EnableSsl:                  false,
		SslPort:                    7272,
		SslCertPath:                "",
		SslKeyPath:                 "",
		AuthenticationMethod:       "Forms",
		AuthenticationRequired:     "DisabledForLocalAddresses",
		APIKey:                     "",
		LogLevel:                   "Info",
		ConsoleLogLevel:            "Info",
		LogSizeLimit:               10,
		LogRotate:                  5,
		InstanceName:               "AniList Arr Sync",
		LaunchBrowser:              true,
		EnableDesktopNotifications: false,
		UpdateAutomatically:        true,
		UpdateMechanism:            "BuiltIn",
		DatabasePath:               "sync.db",
		AniListUsername:            "",
		AniListTvStatus:            "CURRENT",
		AniListMovieStatus:         "CURRENT",
		AniListIncludeUnreleased:   false,
		RadarrURL:                  "",
		RadarrAPIKey:               "",
		RadarrQualityProfileID:     1,
		RadarrRootFolderPath:       "",
		RadarrSearchOnAdd:          false,
		SonarrURL:                  "",
		SonarrAPIKey:               "",
		SonarrQualityProfileID:     1,
		SonarrRootFolderPath:       "",
		SonarrSeriesType:           "anime",
		SonarrSearchOnAdd:          false,
		TagName:                    "anilist-sync",
		SyncIntervalHours:          4,
		DateToleranceDays:          14,
		ConfidenceThreshold:        0.60,
		UnmonitorDropped:           false,
		FirstRunDryRun:             true,
	}
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
	case "Forms", "External":
	default:
		check(false, "AuthenticationMethod must be Forms or External, got %q", c.AuthenticationMethod)
	}

	switch c.AuthenticationRequired {
	case "Enabled", "DisabledForLocalAddresses":
	default:
		check(false, "AuthenticationRequired must be Enabled or DisabledForLocalAddresses, got %q", c.AuthenticationRequired)
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

	switch c.SonarrSeriesType {
	case "anime", "standard":
	default:
		check(false, "SonarrSeriesType must be anime or standard, got %q", c.SonarrSeriesType)
	}

	check(c.SyncIntervalHours > 0, "SyncIntervalHours must be greater than 0, got %d", c.SyncIntervalHours)
	check(c.DateToleranceDays >= 0, "DateToleranceDays cannot be negative, got %d", c.DateToleranceDays)
	check(!math.IsNaN(c.ConfidenceThreshold) && c.ConfidenceThreshold >= 0.0 && c.ConfidenceThreshold <= 1.0, "ConfidenceThreshold must be between 0.0 and 1.0, got %f", c.ConfidenceThreshold)

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("configuration validation failed:\n%w", err)
	}

	return nil
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

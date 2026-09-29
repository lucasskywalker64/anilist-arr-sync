package config

import (
	"fmt"
	"math"
	"strings"
)

// Config represents the application configuration matching Servarr conventions.
type Config struct {
	// Server
	BindAddress string `xml:"BindAddress"`
	Port        int    `xml:"Port"`
	UrlBase     string `xml:"UrlBase"`
	EnableSsl   bool   `xml:"EnableSsl"`
	SslPort     int    `xml:"SslPort"`
	SslCertPath string `xml:"SslCertPath"`
	SslKeyPath  string `xml:"SslKeyPath"`

	// Auth
	AuthenticationMethod   string `xml:"AuthenticationMethod"`
	AuthenticationRequired string `xml:"AuthenticationRequired"`
	ApiKey                 string `xml:"ApiKey"`

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
	RadarrUrl              string `xml:"RadarrUrl"`
	RadarrApiKey           string `xml:"RadarrApiKey"`
	RadarrQualityProfileId int    `xml:"RadarrQualityProfileId"`
	RadarrRootFolderPath   string `xml:"RadarrRootFolderPath"`
	RadarrSearchOnAdd      bool   `xml:"RadarrSearchOnAdd"`

	// Sonarr
	SonarrUrl              string `xml:"SonarrUrl"`
	SonarrApiKey           string `xml:"SonarrApiKey"`
	SonarrQualityProfileId int    `xml:"SonarrQualityProfileId"`
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
		UrlBase:                    "",
		EnableSsl:                  false,
		SslPort:                    7272,
		SslCertPath:                "",
		SslKeyPath:                 "",
		AuthenticationMethod:       "Forms",
		AuthenticationRequired:     "DisabledForLocalAddresses",
		ApiKey:                     "",
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
		RadarrUrl:                  "",
		RadarrApiKey:               "",
		RadarrQualityProfileId:     1,
		RadarrRootFolderPath:       "",
		RadarrSearchOnAdd:          false,
		SonarrUrl:                  "",
		SonarrApiKey:               "",
		SonarrQualityProfileId:     1,
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
	var errs []string

	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, fmt.Sprintf("Port must be between 1 and 65535, got %d", c.Port))
	}

	if c.SslPort < 1 || c.SslPort > 65535 {
		errs = append(errs, fmt.Sprintf("SslPort must be between 1 and 65535, got %d", c.SslPort))
	}

	if c.EnableSsl {
		if strings.TrimSpace(c.SslCertPath) == "" {
			errs = append(errs, "SslCertPath cannot be empty when EnableSsl is true")
		}
		if strings.TrimSpace(c.SslKeyPath) == "" {
			errs = append(errs, "SslKeyPath cannot be empty when EnableSsl is true")
		}
	}

	switch c.AuthenticationMethod {
	case "Forms", "External":
	default:
		errs = append(errs, fmt.Sprintf("AuthenticationMethod must be Forms or External, got %q", c.AuthenticationMethod))
	}

	switch c.AuthenticationRequired {
	case "Enabled", "DisabledForLocalAddresses":
	default:
		errs = append(errs, fmt.Sprintf("AuthenticationRequired must be Enabled or DisabledForLocalAddresses, got %q", c.AuthenticationRequired))
	}

	if !isValidLogLevel(c.LogLevel) {
		errs = append(errs, fmt.Sprintf("LogLevel must be Trace, Debug, Info, Warn, or Error, got %q", c.LogLevel))
	}

	if !isValidLogLevel(c.ConsoleLogLevel) {
		errs = append(errs, fmt.Sprintf("ConsoleLogLevel must be Trace, Debug, Info, Warn, or Error, got %q", c.ConsoleLogLevel))
	}

	if c.LogSizeLimit <= 0 {
		errs = append(errs, fmt.Sprintf("LogSizeLimit must be greater than 0, got %d", c.LogSizeLimit))
	}

	if c.LogRotate < 0 {
		errs = append(errs, fmt.Sprintf("LogRotate cannot be negative, got %d", c.LogRotate))
	}

	switch c.UpdateMechanism {
	case "BuiltIn", "Docker", "Package":
	default:
		errs = append(errs, fmt.Sprintf("UpdateMechanism must be BuiltIn, Docker, or Package, got %q", c.UpdateMechanism))
	}

	if strings.TrimSpace(c.DatabasePath) == "" {
		errs = append(errs, "DatabasePath cannot be empty")
	}

	switch c.SonarrSeriesType {
	case "anime", "standard":
	default:
		errs = append(errs, fmt.Sprintf("SonarrSeriesType must be anime or standard, got %q", c.SonarrSeriesType))
	}

	if c.SyncIntervalHours <= 0 {
		errs = append(errs, fmt.Sprintf("SyncIntervalHours must be greater than 0, got %d", c.SyncIntervalHours))
	}

	if c.DateToleranceDays < 0 {
		errs = append(errs, fmt.Sprintf("DateToleranceDays cannot be negative, got %d", c.DateToleranceDays))
	}

	if math.IsNaN(c.ConfidenceThreshold) || c.ConfidenceThreshold < 0.0 || c.ConfidenceThreshold > 1.0 {
		errs = append(errs, fmt.Sprintf("ConfidenceThreshold must be between 0.0 and 1.0, got %f", c.ConfidenceThreshold))
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

func isValidLogLevel(level string) bool {
	switch strings.ToLower(level) {
	case "trace", "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

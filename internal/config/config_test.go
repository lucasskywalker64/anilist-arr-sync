package config_test

import (
	"math"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestNewDefault(t *testing.T) {
	cfg := config.NewDefault()
	if cfg == nil {
		t.Fatal("expected non-nil default config")
	}

	check := func(name string, got, want any) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}

	check("BindAddress", cfg.BindAddress, "0.0.0.0")
	check("Port", cfg.Port, 7171)
	check("UrlBase", cfg.UrlBase, "")
	check("EnableSsl", cfg.EnableSsl, false)
	check("SslPort", cfg.SslPort, 7272)
	check("AuthenticationMethod", cfg.AuthenticationMethod, "Forms")
	check("AuthenticationRequired", cfg.AuthenticationRequired, "DisabledForLocalAddresses")
	check("LogLevel", cfg.LogLevel, "Info")
	check("ConsoleLogLevel", cfg.ConsoleLogLevel, "Info")
	check("LogSizeLimit", cfg.LogSizeLimit, 10)
	check("LogRotate", cfg.LogRotate, 5)
	check("InstanceName", cfg.InstanceName, "AniList Arr Sync")
	check("LaunchBrowser", cfg.LaunchBrowser, true)
	check("EnableDesktopNotifications", cfg.EnableDesktopNotifications, false)
	check("UpdateAutomatically", cfg.UpdateAutomatically, true)
	check("UpdateMechanism", cfg.UpdateMechanism, "BuiltIn")
	check("DatabasePath", cfg.DatabasePath, "sync.db")
	check("AniListTvStatus", cfg.AniListTvStatus, "CURRENT")
	check("AniListMovieStatus", cfg.AniListMovieStatus, "CURRENT")
	check("AniListIncludeUnreleased", cfg.AniListIncludeUnreleased, false)
	check("RadarrQualityProfileId", cfg.RadarrQualityProfileId, 1)
	check("RadarrSearchOnAdd", cfg.RadarrSearchOnAdd, false)
	check("SonarrQualityProfileId", cfg.SonarrQualityProfileId, 1)
	check("SonarrSeriesType", cfg.SonarrSeriesType, "anime")
	check("SonarrSearchOnAdd", cfg.SonarrSearchOnAdd, false)
	check("TagName", cfg.TagName, "anilist-sync")
	check("SyncIntervalHours", cfg.SyncIntervalHours, 4)
	check("DateToleranceDays", cfg.DateToleranceDays, 14)
	check("ConfidenceThreshold", cfg.ConfidenceThreshold, 0.60)
	check("UnmonitorDropped", cfg.UnmonitorDropped, false)
	check("FirstRunDryRun", cfg.FirstRunDryRun, true)

	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config failed validation: %v", err)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(c *config.Config)
		wantError string
	}{
		{
			name: "invalid port 0",
			modify: func(c *config.Config) {
				c.Port = 0
			},
			wantError: "Port must be between 1 and 65535",
		},
		{
			name: "invalid port 70000",
			modify: func(c *config.Config) {
				c.Port = 70000
			},
			wantError: "Port must be between 1 and 65535",
		},
		{
			name: "invalid ssl port",
			modify: func(c *config.Config) {
				c.SslPort = 65536
			},
			wantError: "SslPort must be between 1 and 65535",
		},
		{
			name: "ssl enabled without cert path",
			modify: func(c *config.Config) {
				c.EnableSsl = true
				c.SslKeyPath = "server.key"
			},
			wantError: "SslCertPath cannot be empty when EnableSsl is true",
		},
		{
			name: "ssl enabled without key path",
			modify: func(c *config.Config) {
				c.EnableSsl = true
				c.SslCertPath = "server.crt"
			},
			wantError: "SslKeyPath cannot be empty when EnableSsl is true",
		},
		{
			name: "invalid authentication method",
			modify: func(c *config.Config) {
				c.AuthenticationMethod = "OAuth"
			},
			wantError: "AuthenticationMethod must be Forms or External",
		},
		{
			name: "invalid authentication required mode",
			modify: func(c *config.Config) {
				c.AuthenticationRequired = "Never"
			},
			wantError: "AuthenticationRequired must be Enabled or DisabledForLocalAddresses",
		},
		{
			name: "invalid log level",
			modify: func(c *config.Config) {
				c.LogLevel = "Verbose"
			},
			wantError: "LogLevel must be Trace, Debug, Info, Warn, or Error",
		},
		{
			name: "invalid console log level",
			modify: func(c *config.Config) {
				c.ConsoleLogLevel = "Critical"
			},
			wantError: "ConsoleLogLevel must be Trace, Debug, Info, Warn, or Error",
		},
		{
			name: "invalid log size limit",
			modify: func(c *config.Config) {
				c.LogSizeLimit = 0
			},
			wantError: "LogSizeLimit must be greater than 0",
		},
		{
			name: "invalid log rotate count",
			modify: func(c *config.Config) {
				c.LogRotate = -1
			},
			wantError: "LogRotate cannot be negative",
		},
		{
			name: "invalid update mechanism",
			modify: func(c *config.Config) {
				c.UpdateMechanism = "Apt"
			},
			wantError: "UpdateMechanism must be BuiltIn, Docker, or Package",
		},
		{
			name: "empty database path",
			modify: func(c *config.Config) {
				c.DatabasePath = ""
			},
			wantError: "DatabasePath cannot be empty",
		},
		{
			name: "invalid sonarr series type",
			modify: func(c *config.Config) {
				c.SonarrSeriesType = "documentary"
			},
			wantError: "SonarrSeriesType must be anime or standard",
		},
		{
			name: "invalid sync interval",
			modify: func(c *config.Config) {
				c.SyncIntervalHours = 0
			},
			wantError: "SyncIntervalHours must be greater than 0",
		},
		{
			name: "negative date tolerance",
			modify: func(c *config.Config) {
				c.DateToleranceDays = -1
			},
			wantError: "DateToleranceDays cannot be negative",
		},
		{
			name: "confidence threshold above 1",
			modify: func(c *config.Config) {
				c.ConfidenceThreshold = 1.05
			},
			wantError: "ConfidenceThreshold must be between 0.0 and 1.0",
		},
		{
			name: "confidence threshold below 0",
			modify: func(c *config.Config) {
				c.ConfidenceThreshold = -0.1
			},
			wantError: "ConfidenceThreshold must be between 0.0 and 1.0",
		},
		{
			name: "confidence threshold NaN",
			modify: func(c *config.Config) {
				c.ConfidenceThreshold = math.NaN()
			},
			wantError: "ConfidenceThreshold must be between 0.0 and 1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewDefault()
			tt.modify(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected validation error containing %q, got nil", tt.wantError)
			}
			if !containsSubstring(err.Error(), tt.wantError) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantError)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && searchSubstr(s, substr)))
}

func searchSubstr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

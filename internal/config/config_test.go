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

	if cfg.BindAddress != "0.0.0.0" {
		t.Errorf("BindAddress = %q, want %q", cfg.BindAddress, "0.0.0.0")
	}
	if cfg.Port != 7171 {
		t.Errorf("Port = %d, want %d", cfg.Port, 7171)
	}
	if cfg.UrlBase != "" {
		t.Errorf("UrlBase = %q, want empty", cfg.UrlBase)
	}
	if cfg.EnableSsl != false {
		t.Errorf("EnableSsl = %v, want false", cfg.EnableSsl)
	}
	if cfg.SslPort != 7272 {
		t.Errorf("SslPort = %d, want %d", cfg.SslPort, 7272)
	}
	if cfg.AuthenticationMethod != "Forms" {
		t.Errorf("AuthenticationMethod = %q, want %q", cfg.AuthenticationMethod, "Forms")
	}
	if cfg.AuthenticationRequired != "DisabledForLocalAddresses" {
		t.Errorf("AuthenticationRequired = %q, want %q", cfg.AuthenticationRequired, "DisabledForLocalAddresses")
	}
	if cfg.LogLevel != "Info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "Info")
	}
	if cfg.ConsoleLogLevel != "Info" {
		t.Errorf("ConsoleLogLevel = %q, want %q", cfg.ConsoleLogLevel, "Info")
	}
	if cfg.LogSizeLimit != 10 {
		t.Errorf("LogSizeLimit = %d, want %d", cfg.LogSizeLimit, 10)
	}
	if cfg.LogRotate != 5 {
		t.Errorf("LogRotate = %d, want %d", cfg.LogRotate, 5)
	}
	if cfg.InstanceName != "AniList Arr Sync" {
		t.Errorf("InstanceName = %q, want %q", cfg.InstanceName, "AniList Arr Sync")
	}
	if cfg.LaunchBrowser != true {
		t.Errorf("LaunchBrowser = %v, want true", cfg.LaunchBrowser)
	}
	if cfg.EnableDesktopNotifications != false {
		t.Errorf("EnableDesktopNotifications = %v, want false", cfg.EnableDesktopNotifications)
	}
	if cfg.UpdateAutomatically != true {
		t.Errorf("UpdateAutomatically = %v, want true", cfg.UpdateAutomatically)
	}
	if cfg.UpdateMechanism != "BuiltIn" {
		t.Errorf("UpdateMechanism = %q, want %q", cfg.UpdateMechanism, "BuiltIn")
	}
	if cfg.DatabasePath != "sync.db" {
		t.Errorf("DatabasePath = %q, want %q", cfg.DatabasePath, "sync.db")
	}
	if cfg.AniListTvStatus != "CURRENT" {
		t.Errorf("AniListTvStatus = %q, want %q", cfg.AniListTvStatus, "CURRENT")
	}
	if cfg.AniListMovieStatus != "CURRENT" {
		t.Errorf("AniListMovieStatus = %q, want %q", cfg.AniListMovieStatus, "CURRENT")
	}
	if cfg.AniListIncludeUnreleased != false {
		t.Errorf("AniListIncludeUnreleased = %v, want false", cfg.AniListIncludeUnreleased)
	}
	if cfg.RadarrQualityProfileId != 1 {
		t.Errorf("RadarrQualityProfileId = %d, want 1", cfg.RadarrQualityProfileId)
	}
	if cfg.RadarrSearchOnAdd != false {
		t.Errorf("RadarrSearchOnAdd = %v, want false", cfg.RadarrSearchOnAdd)
	}
	if cfg.SonarrQualityProfileId != 1 {
		t.Errorf("SonarrQualityProfileId = %d, want 1", cfg.SonarrQualityProfileId)
	}
	if cfg.SonarrSeriesType != "anime" {
		t.Errorf("SonarrSeriesType = %q, want %q", cfg.SonarrSeriesType, "anime")
	}
	if cfg.SonarrSearchOnAdd != false {
		t.Errorf("SonarrSearchOnAdd = %v, want false", cfg.SonarrSearchOnAdd)
	}
	if cfg.TagName != "anilist-sync" {
		t.Errorf("TagName = %q, want %q", cfg.TagName, "anilist-sync")
	}
	if cfg.SyncIntervalHours != 4 {
		t.Errorf("SyncIntervalHours = %d, want %d", cfg.SyncIntervalHours, 4)
	}
	if cfg.DateToleranceDays != 14 {
		t.Errorf("DateToleranceDays = %d, want %d", cfg.DateToleranceDays, 14)
	}
	if cfg.ConfidenceThreshold != 0.60 {
		t.Errorf("ConfidenceThreshold = %f, want 0.60", cfg.ConfidenceThreshold)
	}
	if cfg.UnmonitorDropped != false {
		t.Errorf("UnmonitorDropped = %v, want false", cfg.UnmonitorDropped)
	}
	if cfg.FirstRunDryRun != true {
		t.Errorf("FirstRunDryRun = %v, want true", cfg.FirstRunDryRun)
	}

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

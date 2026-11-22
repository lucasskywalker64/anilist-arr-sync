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

	check := newChecker(t)

	check("BindAddress", cfg.BindAddress, "0.0.0.0")
	check("Port", cfg.Port, 7171)
	check("URLBase", cfg.URLBase, "")
	check("EnableSsl", cfg.EnableSsl, false)
	check("SslPort", cfg.SslPort, 7272)
	check("AuthenticationMethod", cfg.AuthenticationMethod, "Forms")
	check("AuthenticationRequired", cfg.AuthenticationRequired, "DisabledForLocalAddresses")
	check("TrustedNetworks", cfg.TrustedNetworks, "127.0.0.1/32,::1/128,172.16.0.0/12,192.168.0.0/16,10.0.0.0/8")
	check("LogLevel", cfg.LogLevel, "Info")
	check("ConsoleLogLevel", cfg.ConsoleLogLevel, "Info")
	check("LogSizeLimit", cfg.LogSizeLimit, 1)
	check("LogRotate", cfg.LogRotate, 5)
	check("InstanceName", cfg.InstanceName, "AniList Arr Sync")
	check("LaunchBrowser", cfg.LaunchBrowser, true)
	check("EnableDesktopNotifications", cfg.EnableDesktopNotifications, false)
	check("UpdateBranch", cfg.UpdateBranch, "main")
	check("UpdateAutomatically", cfg.UpdateAutomatically, false)
	check("UpdateMechanism", cfg.UpdateMechanism, "BuiltIn")
	check("BackupFolder", cfg.BackupFolder, "Backups")
	check("BackupInterval", cfg.BackupInterval, 1)
	check("BackupRetention", cfg.BackupRetention, 28)
	check("DatabasePath", cfg.DatabasePath, "sync.db")
	check("AniListTvStatus", cfg.AniListTvStatus, "CURRENT")
	check("AniListMovieStatus", cfg.AniListMovieStatus, "CURRENT")
	check("AniListIncludeUnreleased", cfg.AniListIncludeUnreleased, false)
	check("RadarrQualityProfileID", cfg.RadarrQualityProfileID, 1)
	check("RadarrSearchOnAdd", cfg.RadarrSearchOnAdd, false)
	check("SonarrQualityProfileID", cfg.SonarrQualityProfileID, 1)
	check("SonarrSeriesType", cfg.SonarrSeriesType, "anime")
	check("SonarrSearchOnAdd", cfg.SonarrSearchOnAdd, false)
	check("TagName", cfg.TagName, "anilist-sync")
	check("SyncIntervalHours", cfg.SyncIntervalHours, 4)
	check("DateToleranceDays", cfg.DateToleranceDays, 14)
	check("ConfidenceThreshold", cfg.ConfidenceThreshold, 0.60)
	check("UnmonitorDropped", cfg.UnmonitorDropped, false)
	check("UnmonitorCompleted", cfg.UnmonitorCompleted, false)
	check("DeleteDropped", cfg.DeleteDropped, false)
	check("DeleteCompleted", cfg.DeleteCompleted, false)
	check("UnmonitorWatched", cfg.UnmonitorWatched, false)
	check("DeleteWatched", cfg.DeleteWatched, false)
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
			name: "port collision when ssl enabled",
			modify: func(c *config.Config) {
				c.EnableSsl = true
				c.SslCertPath = "server.crt"
				c.SslKeyPath = "server.key"
				c.Port = 7171
				c.SslPort = 7171
			},
			wantError: "Port and SslPort must differ when EnableSsl is true",
		},
		{
			name: "invalid authentication method",
			modify: func(c *config.Config) {
				c.AuthenticationMethod = "OAuth"
			},
			wantError: "AuthenticationMethod must be Forms or Basic",
		},
		{
			name: "external authentication method rejected",
			modify: func(c *config.Config) {
				c.AuthenticationMethod = "External"
			},
			wantError: "AuthenticationMethod must be Forms or Basic",
		},
		{
			name: "invalid authentication required mode",
			modify: func(c *config.Config) {
				c.AuthenticationRequired = "Never"
			},
			wantError: "AuthenticationRequired must be Enabled or DisabledForLocalAddresses",
		},
		{
			name: "invalid trusted networks",
			modify: func(c *config.Config) {
				c.TrustedNetworks = "127.0.0.1/32,not-a-valid-cidr"
			},
			wantError: "TrustedNetworks contains invalid",
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

func TestConfigValidation_QualityProfileOverrides(t *testing.T) {
	t.Run("normalizes lowercase country codes to uppercase", func(t *testing.T) {
		cfg := config.NewDefault()
		cfg.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"cn": 4, " kr ": 5}
		cfg.RadarrQualityProfileOverrides = config.CountryQualityProfiles{"tw": 6}

		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
		if cfg.SonarrQualityProfileOverrides["CN"] != 4 || cfg.SonarrQualityProfileOverrides["KR"] != 5 {
			t.Errorf("unexpected normalized Sonarr overrides: %+v", cfg.SonarrQualityProfileOverrides)
		}
		if cfg.RadarrQualityProfileOverrides["TW"] != 6 {
			t.Errorf("unexpected normalized Radarr overrides: %+v", cfg.RadarrQualityProfileOverrides)
		}
	})

	invalidCases := []struct {
		name      string
		modify    func(c *config.Config)
		wantError string
	}{
		{
			name: "sonarr empty country code",
			modify: func(c *config.Config) {
				c.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"": 4}
			},
			wantError: "SonarrQualityProfileOverrides",
		},
		{
			name: "sonarr three letter country code",
			modify: func(c *config.Config) {
				c.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"CHN": 4}
			},
			wantError: "SonarrQualityProfileOverrides",
		},
		{
			name: "sonarr numeric country code",
			modify: func(c *config.Config) {
				c.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"C1": 4}
			},
			wantError: "SonarrQualityProfileOverrides",
		},
		{
			name: "sonarr non-ascii country code",
			modify: func(c *config.Config) {
				c.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"ÄÖ": 4}
			},
			wantError: "SonarrQualityProfileOverrides",
		},
		{
			name: "sonarr zero profile id",
			modify: func(c *config.Config) {
				c.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"CN": 0}
			},
			wantError: "SonarrQualityProfileOverrides",
		},
		{
			name: "radarr negative profile id",
			modify: func(c *config.Config) {
				c.RadarrQualityProfileOverrides = config.CountryQualityProfiles{"KR": -2}
			},
			wantError: "RadarrQualityProfileOverrides",
		},
		{
			name: "duplicate country code after normalization",
			modify: func(c *config.Config) {
				c.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"cn": 4, "CN": 5}
			},
			wantError: "duplicate",
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.NewDefault()
			tc.modify(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected validation error containing %q, got nil", tc.wantError)
			}
			if !containsSubstring(err.Error(), tc.wantError) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantError)
			}
		})
	}
}

func TestConfigResolveQualityProfileID(t *testing.T) {
	cfg := config.NewDefault()
	cfg.SonarrQualityProfileID = 1
	cfg.RadarrQualityProfileID = 2
	cfg.SonarrQualityProfileOverrides = config.CountryQualityProfiles{"CN": 4, "KR": 5}
	cfg.RadarrQualityProfileOverrides = config.CountryQualityProfiles{"CN": 6}

	if got := cfg.ResolveSonarrQualityProfileID("CN"); got != 4 {
		t.Errorf("ResolveSonarrQualityProfileID(CN) = %d, want 4", got)
	}
	if got := cfg.ResolveSonarrQualityProfileID("kr"); got != 5 {
		t.Errorf("ResolveSonarrQualityProfileID(kr) = %d, want 5", got)
	}
	if got := cfg.ResolveSonarrQualityProfileID("JP"); got != 1 {
		t.Errorf("ResolveSonarrQualityProfileID(JP) = %d, want default 1", got)
	}
	if got := cfg.ResolveSonarrQualityProfileID(""); got != 1 {
		t.Errorf("ResolveSonarrQualityProfileID('') = %d, want default 1", got)
	}
	if got := cfg.ResolveRadarrQualityProfileID("CN"); got != 6 {
		t.Errorf("ResolveRadarrQualityProfileID(CN) = %d, want 6", got)
	}
	if got := cfg.ResolveRadarrQualityProfileID("JP"); got != 2 {
		t.Errorf("ResolveRadarrQualityProfileID(JP) = %d, want default 2", got)
	}

	cloned := cfg.Clone()
	cloned.SonarrQualityProfileOverrides["CN"] = 99
	if cfg.SonarrQualityProfileOverrides["CN"] != 4 {
		t.Errorf("Clone did not isolate SonarrQualityProfileOverrides map")
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

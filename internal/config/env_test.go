package config_test

import (
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestApplyEnv_Overrides(t *testing.T) {
	cfg := config.NewDefault()

	environ := []string{
		"UNRELATED_VAR=foo",
		"ANILIST_SYNC__SERVER__PORT=8080",
		"ANILIST_SYNC__SERVER__BINDADDRESS=127.0.0.1",
		"ANILIST_SYNC__SERVER__ENABLESSL=true",
		"ANILIST_SYNC__SERVER__SSLCERTPATH=/certs/app.crt",
		"ANILIST_SYNC__SERVER__SSLKEYPATH=/certs/app.key",
		"ANILIST_SYNC__AUTH__METHOD=External",
		"ANILIST_SYNC__AUTH__APIKEY=my-secret-token",
		"ANILIST_SYNC__LOG__LEVEL=Debug",
		"ANILIST_SYNC__LOG__SIZELIMIT=50",
		"ANILIST_SYNC__APP__INSTANCENAME=Docker Anime Sync",
		"ANILIST_SYNC__APP__LAUNCHBROWSER=false",
		"ANILIST_SYNC__UPDATE__MECHANISM=Docker",
		"ANILIST_SYNC__STORAGE__DATABASEPATH=/config/sync.db",
		"ANILIST_SYNC__ANILIST__USERNAME=kenshiro",
		"ANILIST_SYNC__RADARR__URL=http://radarr:7878",
		"ANILIST_SYNC__RADARR__SEARCHONADD=true",
		"ANILIST_SYNC__SONARR__SERIESTYPE=standard",
		"ANILIST_SYNC__SYNC__INTERVALHOURS=8",
		"ANILIST_SYNC__SYNC__CONFIDENCETHRESHOLD=0.75",
		"ANILIST_SYNC__SYNC__FIRSTRUNDRYRUN=false",
	}

	if err := cfg.ApplyEnv(environ); err != nil {
		t.Fatalf("unexpected error applying env: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.BindAddress != "127.0.0.1" {
		t.Errorf("BindAddress = %q, want 127.0.0.1", cfg.BindAddress)
	}
	if !cfg.EnableSsl {
		t.Errorf("EnableSsl = %v, want true", cfg.EnableSsl)
	}
	if cfg.SslCertPath != "/certs/app.crt" {
		t.Errorf("SslCertPath = %q, want /certs/app.crt", cfg.SslCertPath)
	}
	if cfg.SslKeyPath != "/certs/app.key" {
		t.Errorf("SslKeyPath = %q, want /certs/app.key", cfg.SslKeyPath)
	}
	if cfg.AuthenticationMethod != "External" {
		t.Errorf("AuthenticationMethod = %q, want External", cfg.AuthenticationMethod)
	}
	if cfg.ApiKey != "my-secret-token" {
		t.Errorf("ApiKey = %q, want my-secret-token", cfg.ApiKey)
	}
	if cfg.LogLevel != "Debug" {
		t.Errorf("LogLevel = %q, want Debug", cfg.LogLevel)
	}
	if cfg.LogSizeLimit != 50 {
		t.Errorf("LogSizeLimit = %d, want 50", cfg.LogSizeLimit)
	}
	if cfg.InstanceName != "Docker Anime Sync" {
		t.Errorf("InstanceName = %q, want Docker Anime Sync", cfg.InstanceName)
	}
	if cfg.LaunchBrowser {
		t.Errorf("LaunchBrowser = %v, want false", cfg.LaunchBrowser)
	}
	if cfg.UpdateMechanism != "Docker" {
		t.Errorf("UpdateMechanism = %q, want Docker", cfg.UpdateMechanism)
	}
	if cfg.DatabasePath != "/config/sync.db" {
		t.Errorf("DatabasePath = %q, want /config/sync.db", cfg.DatabasePath)
	}
	if cfg.AniListUsername != "kenshiro" {
		t.Errorf("AniListUsername = %q, want kenshiro", cfg.AniListUsername)
	}
	if cfg.RadarrUrl != "http://radarr:7878" {
		t.Errorf("RadarrUrl = %q, want http://radarr:7878", cfg.RadarrUrl)
	}
	if !cfg.RadarrSearchOnAdd {
		t.Errorf("RadarrSearchOnAdd = %v, want true", cfg.RadarrSearchOnAdd)
	}
	if cfg.SonarrSeriesType != "standard" {
		t.Errorf("SonarrSeriesType = %q, want standard", cfg.SonarrSeriesType)
	}
	if cfg.SyncIntervalHours != 8 {
		t.Errorf("SyncIntervalHours = %d, want 8", cfg.SyncIntervalHours)
	}
	if cfg.ConfidenceThreshold != 0.75 {
		t.Errorf("ConfidenceThreshold = %f, want 0.75", cfg.ConfidenceThreshold)
	}
	if cfg.FirstRunDryRun {
		t.Errorf("FirstRunDryRun = %v, want false", cfg.FirstRunDryRun)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("config after env overrides failed validation: %v", err)
	}
}

func TestApplyEnv_InvalidValues(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		wantError string
	}{
		{
			name:      "invalid integer syntax",
			env:       "ANILIST_SYNC__SERVER__PORT=not-a-number",
			wantError: "invalid integer",
		},
		{
			name:      "invalid boolean syntax",
			env:       "ANILIST_SYNC__SERVER__ENABLESSL=maybe",
			wantError: "invalid boolean",
		},
		{
			name:      "invalid float syntax",
			env:       "ANILIST_SYNC__SYNC__CONFIDENCETHRESHOLD=high",
			wantError: "invalid float",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewDefault()
			err := cfg.ApplyEnv([]string{tt.env})
			if err == nil {
				t.Fatalf("expected error parsing %q, got nil", tt.env)
			}
			if !containsSubstring(err.Error(), tt.wantError) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantError)
			}
		})
	}
}

package config_test

import (
	"strings"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestCleanupSettingsIndependent(t *testing.T) {
	fields := []struct {
		name  string
		value func(*config.Config) *bool
	}{
		{"UnmonitorCompleted", func(c *config.Config) *bool { return &c.UnmonitorCompleted }},
		{"DeleteDropped", func(c *config.Config) *bool { return &c.DeleteDropped }},
		{"DeleteCompleted", func(c *config.Config) *bool { return &c.DeleteCompleted }},
		{"UnmonitorWatched", func(c *config.Config) *bool { return &c.UnmonitorWatched }},
		{"DeleteWatched", func(c *config.Config) *bool { return &c.DeleteWatched }},
	}
	check := func(t *testing.T, cfg *config.Config, enabled string) {
		t.Helper()
		for _, field := range fields {
			if got, want := *field.value(cfg), field.name == enabled; got != want {
				t.Errorf("%s = %v, want %v", field.name, got, want)
			}
		}
	}
	for _, selected := range fields {
		t.Run(selected.name, func(t *testing.T) {
			t.Run("environment", func(t *testing.T) {
				cfg := config.NewDefault()
				if err := cfg.ApplyEnv([]string{"ANILIST_SYNC__SYNC__" + strings.ToUpper(selected.name) + "=true"}); err != nil {
					t.Fatal(err)
				}
				check(t, cfg, selected.name)
			})
			t.Run("XML input", func(t *testing.T) {
				cfg, err := config.ParseXML([]byte("<Config><" + selected.name + ">True</" + selected.name + "></Config>"))
				if err != nil {
					t.Fatal(err)
				}
				check(t, cfg, selected.name)
			})
			t.Run("XML output", func(t *testing.T) {
				cfg := config.NewDefault()
				*selected.value(cfg) = true
				data, err := cfg.ToXML()
				if err != nil {
					t.Fatal(err)
				}
				for _, field := range fields {
					want := "False"
					if field.name == selected.name {
						want = "True"
					}
					if !strings.Contains(string(data), "<"+field.name+">"+want+"</"+field.name+">") {
						t.Errorf("XML must set %s to %s: %s", field.name, want, data)
					}
				}
			})
		})
	}
}

func TestApplyEnv_Overrides(t *testing.T) {
	cfg := config.NewDefault()

	environ := []string{
		"UNRELATED_VAR=foo",
		"ANILIST_SYNC__SERVER__PORT=8080",
		"ANILIST_SYNC__SERVER__BINDADDRESS=127.0.0.1",
		"ANILIST_SYNC__SERVER__ENABLESSL=true",
		"ANILIST_SYNC__SERVER__SSLCERTPATH=/certs/app.crt",
		"ANILIST_SYNC__SERVER__SSLKEYPATH=/certs/app.key",
		"ANILIST_SYNC__AUTH__METHOD=Basic",
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
		"ANILIST_SYNC__SYNC__UNMONITORCOMPLETED=true",
		"ANILIST_SYNC__SYNC__DELETEDROPPED=true",
		"ANILIST_SYNC__SYNC__DELETECOMPLETED=true",
		"ANILIST_SYNC__SYNC__UNMONITORWATCHED=true",
		"ANILIST_SYNC__SYNC__DELETEWATCHED=true",
		"ANILIST_SYNC__SYNC__FIRSTRUNDRYRUN=false",
	}

	if err := cfg.ApplyEnv(environ); err != nil {
		t.Fatalf("unexpected error applying env: %v", err)
	}

	check := newChecker(t)

	check("Port", cfg.Port, 8080)
	check("BindAddress", cfg.BindAddress, "127.0.0.1")
	check("EnableSsl", cfg.EnableSsl, true)
	check("SslCertPath", cfg.SslCertPath, "/certs/app.crt")
	check("SslKeyPath", cfg.SslKeyPath, "/certs/app.key")
	check("AuthenticationMethod", cfg.AuthenticationMethod, "Basic")
	check("APIKey", cfg.APIKey, "my-secret-token")
	check("LogLevel", cfg.LogLevel, "Debug")
	check("LogSizeLimit", cfg.LogSizeLimit, 50)
	check("InstanceName", cfg.InstanceName, "Docker Anime Sync")
	check("LaunchBrowser", cfg.LaunchBrowser, false)
	check("UpdateMechanism", cfg.UpdateMechanism, "Docker")
	check("DatabasePath", cfg.DatabasePath, "/config/sync.db")
	check("AniListUsername", cfg.AniListUsername, "kenshiro")
	check("RadarrURL", cfg.RadarrURL, "http://radarr:7878")
	check("RadarrSearchOnAdd", cfg.RadarrSearchOnAdd, true)
	check("SonarrSeriesType", cfg.SonarrSeriesType, "standard")
	check("SyncIntervalHours", cfg.SyncIntervalHours, 8)
	check("ConfidenceThreshold", cfg.ConfidenceThreshold, 0.75)
	check("UnmonitorCompleted", cfg.UnmonitorCompleted, true)
	check("DeleteDropped", cfg.DeleteDropped, true)
	check("DeleteCompleted", cfg.DeleteCompleted, true)
	check("UnmonitorWatched", cfg.UnmonitorWatched, true)
	check("DeleteWatched", cfg.DeleteWatched, true)
	check("FirstRunDryRun", cfg.FirstRunDryRun, false)

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

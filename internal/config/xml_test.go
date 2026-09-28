package config_test

import (
	"strings"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestParseXML_DocumentedDefaults(t *testing.T) {
	// Minimal XML with only Port specified; all other elements should retain documented defaults
	xmlContent := []byte(`<Config>
  <Port>8080</Port>
</Config>`)

	cfg, err := config.ParseXML(xmlContent)
	if err != nil {
		t.Fatalf("unexpected error parsing partial XML: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.BindAddress != "0.0.0.0" {
		t.Errorf("BindAddress = %q, want default 0.0.0.0", cfg.BindAddress)
	}
	if cfg.LogLevel != "Info" {
		t.Errorf("LogLevel = %q, want default Info", cfg.LogLevel)
	}
	if cfg.SyncIntervalHours != 4 {
		t.Errorf("SyncIntervalHours = %d, want default 4", cfg.SyncIntervalHours)
	}
	if cfg.ConfidenceThreshold != 0.60 {
		t.Errorf("ConfidenceThreshold = %f, want default 0.60", cfg.ConfidenceThreshold)
	}
}

func TestParseXML_ServarrSample(t *testing.T) {
	servarrXML := []byte(`<Config>
  <BindAddress>127.0.0.1</BindAddress>
  <Port>7171</Port>
  <UrlBase>/sync</UrlBase>
  <EnableSsl>True</EnableSsl>
  <SslPort>7272</SslPort>
  <SslCertPath>/etc/ssl/cert.pem</SslCertPath>
  <SslKeyPath>/etc/ssl/key.pem</SslKeyPath>
  <AuthenticationMethod>Forms</AuthenticationMethod>
  <AuthenticationRequired>Enabled</AuthenticationRequired>
  <ApiKey>secret-api-key</ApiKey>
  <LogLevel>Debug</LogLevel>
  <ConsoleLogLevel>Warn</ConsoleLogLevel>
  <LogSizeLimit>20</LogSizeLimit>
  <LogRotate>10</LogRotate>
  <InstanceName>Custom Sync</InstanceName>
  <LaunchBrowser>False</LaunchBrowser>
  <EnableDesktopNotifications>True</EnableDesktopNotifications>
  <UpdateAutomatically>False</UpdateAutomatically>
  <UpdateMechanism>Docker</UpdateMechanism>
  <DatabasePath>/data/sync.db</DatabasePath>
  <AniListUsername>otaku42</AniListUsername>
  <AniListTvStatus>CURRENT,PLANNING</AniListTvStatus>
  <AniListMovieStatus>COMPLETED</AniListMovieStatus>
  <AniListIncludeUnreleased>True</AniListIncludeUnreleased>
  <RadarrUrl>http://localhost:7878</RadarrUrl>
  <RadarrApiKey>radarr-key</RadarrApiKey>
  <RadarrQualityProfileId>2</RadarrQualityProfileId>
  <RadarrRootFolderPath>/movies</RadarrRootFolderPath>
  <RadarrSearchOnAdd>True</RadarrSearchOnAdd>
  <SonarrUrl>http://localhost:8989</SonarrUrl>
  <SonarrApiKey>sonarr-key</SonarrApiKey>
  <SonarrQualityProfileId>3</SonarrQualityProfileId>
  <SonarrRootFolderPath>/tv</SonarrRootFolderPath>
  <SonarrSeriesType>standard</SonarrSeriesType>
  <SonarrSearchOnAdd>True</SonarrSearchOnAdd>
  <TagName>custom-tag</TagName>
  <SyncIntervalHours>6</SyncIntervalHours>
  <DateToleranceDays>7</DateToleranceDays>
  <ConfidenceThreshold>0.85</ConfidenceThreshold>
  <UnmonitorDropped>True</UnmonitorDropped>
  <FirstRunDryRun>False</FirstRunDryRun>
</Config>`)

	cfg, err := config.ParseXML(servarrXML)
	if err != nil {
		t.Fatalf("unexpected error parsing Servarr sample XML: %v", err)
	}

	if cfg.BindAddress != "127.0.0.1" {
		t.Errorf("BindAddress = %q, want 127.0.0.1", cfg.BindAddress)
	}
	if !cfg.EnableSsl {
		t.Errorf("EnableSsl = %v, want true", cfg.EnableSsl)
	}
	if cfg.SslCertPath != "/etc/ssl/cert.pem" {
		t.Errorf("SslCertPath = %q, want /etc/ssl/cert.pem", cfg.SslCertPath)
	}
	if cfg.AniListUsername != "otaku42" {
		t.Errorf("AniListUsername = %q, want otaku42", cfg.AniListUsername)
	}
	if cfg.SonarrSeriesType != "standard" {
		t.Errorf("SonarrSeriesType = %q, want standard", cfg.SonarrSeriesType)
	}
	if cfg.ConfidenceThreshold != 0.85 {
		t.Errorf("ConfidenceThreshold = %f, want 0.85", cfg.ConfidenceThreshold)
	}
	if !cfg.UnmonitorDropped {
		t.Errorf("UnmonitorDropped = %v, want true", cfg.UnmonitorDropped)
	}
	if cfg.FirstRunDryRun {
		t.Errorf("FirstRunDryRun = %v, want false", cfg.FirstRunDryRun)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("parsed config failed validation: %v", err)
	}
}

func TestToXML_RoundTrip(t *testing.T) {
	orig := config.NewDefault()
	orig.InstanceName = "Roundtrip Test"
	orig.Port = 9999
	orig.RadarrUrl = "http://radarr:7878"

	data, err := orig.ToXML()
	if err != nil {
		t.Fatalf("ToXML failed: %v", err)
	}

	xmlStr := string(data)
	if !strings.HasPrefix(xmlStr, "<Config>") {
		t.Errorf("expected XML to start with <Config>, got:\n%s", xmlStr)
	}
	if !strings.Contains(xmlStr, "<InstanceName>Roundtrip Test</InstanceName>") {
		t.Errorf("expected XML to contain InstanceName element, got:\n%s", xmlStr)
	}

	loaded, err := config.ParseXML(data)
	if err != nil {
		t.Fatalf("ParseXML of serialized XML failed: %v", err)
	}

	if loaded.InstanceName != orig.InstanceName {
		t.Errorf("InstanceName = %q, want %q", loaded.InstanceName, orig.InstanceName)
	}
	if loaded.Port != orig.Port {
		t.Errorf("Port = %d, want %d", loaded.Port, orig.Port)
	}
	if loaded.RadarrUrl != orig.RadarrUrl {
		t.Errorf("RadarrUrl = %q, want %q", loaded.RadarrUrl, orig.RadarrUrl)
	}
	if loaded.LaunchBrowser != orig.LaunchBrowser {
		t.Errorf("LaunchBrowser = %v, want %v", loaded.LaunchBrowser, orig.LaunchBrowser)
	}
}

func TestParseXML_InvalidXML(t *testing.T) {
	malformed := []byte(`<Config><Port>not-a-number</Port></Config>`)
	_, err := config.ParseXML(malformed)
	if err == nil {
		t.Fatal("expected error parsing malformed XML port, got nil")
	}

	invalidSyntax := []byte(`<Config><UnclosedTag></Config>`)
	_, err = config.ParseXML(invalidSyntax)
	if err == nil {
		t.Fatal("expected error parsing invalid XML syntax, got nil")
	}
}

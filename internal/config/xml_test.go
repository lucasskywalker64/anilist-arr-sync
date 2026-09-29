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

	check := newChecker(t)

	check("Port", cfg.Port, 8080)
	check("BindAddress", cfg.BindAddress, "0.0.0.0")
	check("LogLevel", cfg.LogLevel, "Info")
	check("SyncIntervalHours", cfg.SyncIntervalHours, 4)
	check("ConfidenceThreshold", cfg.ConfidenceThreshold, 0.60)
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

	check := newChecker(t)

	check("BindAddress", cfg.BindAddress, "127.0.0.1")
	check("EnableSsl", cfg.EnableSsl, true)
	check("SslCertPath", cfg.SslCertPath, "/etc/ssl/cert.pem")
	check("AniListUsername", cfg.AniListUsername, "otaku42")
	check("SonarrSeriesType", cfg.SonarrSeriesType, "standard")
	check("ConfidenceThreshold", cfg.ConfidenceThreshold, 0.85)
	check("UnmonitorDropped", cfg.UnmonitorDropped, true)
	check("FirstRunDryRun", cfg.FirstRunDryRun, false)

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

	check := newChecker(t)

	check("InstanceName", loaded.InstanceName, orig.InstanceName)
	check("Port", loaded.Port, orig.Port)
	check("RadarrUrl", loaded.RadarrUrl, orig.RadarrUrl)
	check("LaunchBrowser", loaded.LaunchBrowser, orig.LaunchBrowser)
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

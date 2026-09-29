package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestLoad_NonExistentFileCreatesDefault(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.xml")

	cfg, err := config.Load(configPath, nil)
	if err != nil {
		t.Fatalf("Load failed for non-existent file: %v", err)
	}

	check := newChecker(t)

	check("Port", cfg.Port, 7171)
	check("BindAddress", cfg.BindAddress, "0.0.0.0")

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("expected config file to be created on first load: %v", err)
	}

	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("file permissions = %04o, want 0600", perm)
		}
	}
}

func TestLoad_NonExistentFileWithEnvDoesNotPersistEnvToDisk(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.xml")

	environ := []string{
		"ANILIST_SYNC__SERVER__PORT=9090",
		"ANILIST_SYNC__AUTH__APIKEY=super-secret-key",
	}

	cfg, err := config.Load(configPath, environ)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	check := newChecker(t)

	// In-memory config has overrides
	check("Port", cfg.Port, 9090)
	check("ApiKey", cfg.ApiKey, "super-secret-key")

	// File on disk must NOT contain environment overrides, but default values
	fileData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read created config file: %v", err)
	}

	fileCfg, err := config.ParseXML(fileData)
	if err != nil {
		t.Fatalf("failed to parse file on disk: %v", err)
	}

	check("file Port on disk", fileCfg.Port, 7171)
	check("file ApiKey on disk", fileCfg.ApiKey, "")
}

func TestLoad_ExistingFileWithEnvOverrides(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.xml")

	initialXML := []byte(`<Config>
  <Port>7575</Port>
  <InstanceName>Existing Server</InstanceName>
</Config>`)
	if err := os.WriteFile(configPath, initialXML, 0600); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	environ := []string{
		"ANILIST_SYNC__SERVER__PORT=8888",
	}

	cfg, err := config.Load(configPath, environ)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	check := newChecker(t)

	// Environment variable should override file setting
	check("Port", cfg.Port, 8888)
	// File setting preserved
	check("InstanceName", cfg.InstanceName, "Existing Server")
	// Missing XML fields populated from defaults
	check("SyncIntervalHours", cfg.SyncIntervalHours, 4)
}

func TestSave_PreservesElementStructureAndValidates(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.xml")

	cfg := config.NewDefault()
	cfg.Port = 8081
	cfg.InstanceName = "Production Node"
	cfg.RadarrUrl = "http://radarr:7878"

	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("config file was not written: %v", err)
	}

	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("file permissions = %04o, want 0600", perm)
		}
	}

	loaded, err := config.Load(configPath, nil)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}

	check := newChecker(t)

	check("Port", loaded.Port, 8081)
	check("InstanceName", loaded.InstanceName, "Production Node")
	check("RadarrUrl", loaded.RadarrUrl, "http://radarr:7878")
}

func TestSave_RejectsInvalidConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.xml")

	cfg := config.NewDefault()
	cfg.Port = 99999 // out of range

	err := config.Save(configPath, cfg)
	if err == nil {
		t.Fatal("expected Save to fail validation with invalid port, got nil")
	}

	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Errorf("file should not exist after failed Save validation")
	}
}

func TestLoad_RejectsCorruptedFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.xml")

	if err := os.WriteFile(configPath, []byte("NOT_XML"), 0600); err != nil {
		t.Fatalf("failed to write corrupted file: %v", err)
	}

	_, err := config.Load(configPath, nil)
	if err == nil {
		t.Fatal("expected Load to fail on corrupted XML, got nil")
	}
}

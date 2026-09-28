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

	if cfg.Port != 7171 {
		t.Errorf("Port = %d, want default 7171", cfg.Port)
	}
	if cfg.BindAddress != "0.0.0.0" {
		t.Errorf("BindAddress = %q, want default 0.0.0.0", cfg.BindAddress)
	}

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

	// Environment variable should override file setting
	if cfg.Port != 8888 {
		t.Errorf("Port = %d, want overridden 8888", cfg.Port)
	}
	// File setting preserved
	if cfg.InstanceName != "Existing Server" {
		t.Errorf("InstanceName = %q, want Existing Server", cfg.InstanceName)
	}
	// Missing XML fields populated from defaults
	if cfg.SyncIntervalHours != 4 {
		t.Errorf("SyncIntervalHours = %d, want default 4", cfg.SyncIntervalHours)
	}
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

	if loaded.Port != 8081 {
		t.Errorf("Port = %d, want 8081", loaded.Port)
	}
	if loaded.InstanceName != "Production Node" {
		t.Errorf("InstanceName = %q, want Production Node", loaded.InstanceName)
	}
	if loaded.RadarrUrl != "http://radarr:7878" {
		t.Errorf("RadarrUrl = %q, want http://radarr:7878", loaded.RadarrUrl)
	}
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

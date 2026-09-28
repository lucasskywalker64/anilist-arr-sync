package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Load reads config.xml from the specified path, applies documented defaults,
// layers environment variable overrides on top, and validates the final configuration.
// If the configuration file does not exist, Load creates it with default values
// and permissions locked to POSIX 0600 on Unix systems.
func Load(path string, environ []string) (*Config, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		cfg := NewDefault()
		if len(environ) > 0 {
			if err := cfg.ApplyEnv(environ); err != nil {
				return nil, err
			}
		}
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		if err := Save(path, cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to inspect config file %s: %w", path, err)
	}

	if err := enforceFilePermissions(path); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	cfg, err := ParseXML(data)
	if err != nil {
		return nil, err
	}

	if len(environ) > 0 {
		if err := cfg.ApplyEnv(environ); err != nil {
			return nil, err
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save serializes the given Config to XML and writes it to disk with POSIX 0600 permissions.
// If the configuration fails validation, Save returns an error without touching the disk.
func Save(path string, cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("refusing to save invalid configuration: %w", err)
	}

	data, err := cfg.ToXML()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, "config.*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary config file in %s: %w", dir, err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write config data to temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close config temp file: %w", err)
	}

	if err := os.Chmod(tmpPath, 0600); err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("failed to set 0600 permissions on temp config: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		if runtime.GOOS == "windows" {
			_ = os.Remove(path)
			if err := os.Rename(tmpPath, path); err != nil {
				return fmt.Errorf("failed to write config file %s: %w", path, err)
			}
		} else {
			return fmt.Errorf("failed to write config file %s: %w", path, err)
		}
	}
	tmpPath = ""

	return enforceFilePermissions(path)
}

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

type runtimePaths struct{ data, config string }

func resolvePaths(o options, platform string, container bool) (runtimePaths, error) {
	dir := o.dataDir
	if dir == "" {
		switch {
		case container:
			dir = "/config"
		case platform == "windows":
			base := os.Getenv("ProgramData")
			if base == "" {
				base = `C:\ProgramData`
			}
			dir = filepath.Join(base, "AniList-Arr-Sync")
			if err := writableDirectory(dir); err != nil {
				base = os.Getenv("APPDATA")
				if base == "" {
					return runtimePaths{}, err
				}
				dir = filepath.Join(base, "AniList-Arr-Sync")
			}
		default:
			base := os.Getenv("XDG_CONFIG_HOME")
			if base == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return runtimePaths{}, err
				}
				base = filepath.Join(home, ".config")
			}
			dir = filepath.Join(base, "anilist-arr-sync")
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return runtimePaths{}, err
	}
	if err := writableDirectory(dir); err != nil {
		return runtimePaths{}, err
	}
	configPath := o.configPath
	if configPath == "" {
		configPath = filepath.Join(dir, "config.xml")
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return runtimePaths{}, err
	}
	return runtimePaths{data: dir, config: configPath}, nil
}

func writableDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(path, ".write-check-*")
	if err != nil {
		return fmt.Errorf("data directory is not writable: %w", err)
	}
	closeErr := f.Close()
	removeErr := os.Remove(f.Name())
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

func loadConfig(o options, paths runtimePaths) (*config.Config, error) {
	cfg, err := config.Load(paths.config, os.Environ())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		key := make([]byte, 16)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		cfg.APIKey = hex.EncodeToString(key)
		if err := config.Save(paths.config, cfg); err != nil {
			return nil, err
		}
	}
	if o.portSet {
		cfg.Port = o.port
	}
	if o.bindSet {
		cfg.BindAddress = o.bindAddress
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func dataPath(dataDir, configured string) string {
	if filepath.IsAbs(configured) {
		return configured
	}
	return filepath.Join(dataDir, configured)
}

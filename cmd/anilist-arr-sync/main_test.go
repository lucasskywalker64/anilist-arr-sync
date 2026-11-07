package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
)

func TestCLIConfiguration(t *testing.T) {
	dir := t.TempDir()
	opts, err := parseFlags([]string{"--data-dir", dir, "--port", "9123", "--bind-address", "127.0.0.1", "--no-browser"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := resolvePaths(opts, "linux", false)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	key, err := hex.DecodeString(cfg.APIKey)
	if err != nil || len(key) != 16 {
		t.Fatalf("expected random 32-character hex key, got %q", cfg.APIKey)
	}
	if opts.port != 9123 || !opts.portSet || opts.bindAddress != "127.0.0.1" || !opts.bindSet {
		t.Fatalf("CLI overrides not parsed: %+v", opts)
	}
	persisted, err := config.Load(paths.config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.APIKey != cfg.APIKey || persisted.Port == 9123 {
		t.Fatal("key must persist; CLI overrides must remain transient")
	}
	cfg2, err := loadConfig(paths)
	if err != nil || cfg2.APIKey != cfg.APIKey {
		t.Fatal("startup replaced the existing key")
	}
	persisted.APIKey = ""
	if err := config.Save(paths.config, persisted); err != nil {
		t.Fatal(err)
	}
	healed, err := loadConfig(paths)
	if err != nil || len(healed.APIKey) != 32 || healed.APIKey == cfg.APIKey {
		t.Fatal("missing API key was not healed")
	}
}

func TestAPIKeyHealingDoesNotPersistEnvironment(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.xml")
			if existing {
				if err := config.Save(path, config.NewDefault()); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("ANILIST_SYNC__SONARR__APIKEY", "environment-key")
			t.Setenv("ANILIST_SYNC__SERVER__PORT", "9123")
			cfg, err := loadConfig(runtimePaths{config: path})
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := config.Load(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.APIKey == "" || persisted.APIKey != cfg.APIKey {
				t.Fatal("generated key was not persisted")
			}
			if cfg.SonarrAPIKey != "environment-key" || cfg.Port != 9123 {
				t.Fatal("environment overrides were not applied")
			}
			if persisted.SonarrAPIKey != "" || persisted.Port != config.NewDefault().Port {
				t.Fatal("key healing persisted environment overrides")
			}
		})
	}
}

func TestAPIKeyHealingPreservesEnvBackedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.xml")
	cfg := config.NewDefault()
	cfg.Port = 0
	data, err := cfg.ToXML()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANILIST_SYNC__SERVER__PORT", "9123")
	effective, err := loadConfig(runtimePaths{config: path})
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := config.ParseXML(data)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Port != 0 || effective.Port != 9123 || persisted.APIKey == "" || persisted.APIKey != effective.APIKey {
		t.Fatal("key healing changed the environment-backed file configuration")
	}
}

func TestCLIVersionAndInvalidFlags(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"--version"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), version) || !strings.Contains(out.String(), commit) || !strings.Contains(out.String(), date) {
		t.Fatalf("missing build metadata: %s", out.String())
	}
	for _, args := range [][]string{{"--unknown"}, {"extra"}, {"--port", "0"}, {"--port", "65536"}} {
		if _, err := parseFlags(args, &out); err == nil {
			t.Fatalf("accepted invalid flags: %v", args)
		}
	}
}

func TestDataPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	paths, err := resolvePaths(options{}, "linux", false)
	if err != nil || paths.data != filepath.Join(root, "xdg", "anilist-arr-sync") {
		t.Fatalf("XDG path: %+v, %v", paths, err)
	}
	t.Setenv("ProgramData", filepath.Join(root, "program"))
	t.Setenv("APPDATA", filepath.Join(root, "app"))
	paths, err = resolvePaths(options{}, "windows", false)
	if err != nil || paths.data != filepath.Join(root, "program", "AniList-Arr-Sync") {
		t.Fatalf("Windows path: %+v, %v", paths, err)
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ProgramData", blocked)
	paths, err = resolvePaths(options{}, "windows", false)
	if err != nil || paths.data != filepath.Join(root, "app", "AniList-Arr-Sync") {
		t.Fatalf("Windows fallback: %+v, %v", paths, err)
	}
	paths, err = resolvePaths(options{dataDir: root, configPath: filepath.Join(root, "other.xml")}, "linux", true)
	if err != nil || paths.data != root || paths.config != filepath.Join(root, "other.xml") {
		t.Fatalf("explicit paths: %+v, %v", paths, err)
	}
}

func TestCLIProcess(t *testing.T) {
	if os.Getenv("ANILIST_TEST_PROCESS") == "1" {
		for i, arg := range os.Args {
			if arg == "--" {
				os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
				break
			}
		}
		main()
		os.Exit(0)
	}
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"--version"}, 0},
		{[]string{"--sync", "--data-dir", t.TempDir()}, 1},
		{[]string{"--port", "0"}, 1},
	} {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestCLIProcess$", "--"}, tc.args...)...)
		cmd.Env = append(os.Environ(), "ANILIST_TEST_PROCESS=1")
		output, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if code != tc.code {
			t.Fatalf("%v: exit %d, want %d: %s", tc.args, code, tc.code, output)
		}
	}
}

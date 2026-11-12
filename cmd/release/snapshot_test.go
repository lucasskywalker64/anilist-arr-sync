package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/updater"
)

// Explicitly opt into real tool execution. All signing material is created in
// this test process. No production environment values or key files are consumed.
func TestSnapshot(t *testing.T) {
	if os.Getenv("RELEASE_SNAPSHOT_TEST") != "1" {
		t.Skip("set RELEASE_SNAPSHOT_TEST=1 with GoReleaser OSS and Inno on PATH")
	}
	t.Chdir(filepath.Join("..", ".."))
	public, private, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("fixture key generation failed")
	}
	encrypted, err := minisign.EncryptKey("disposable-fixture-password", private)
	if err != nil {
		t.Fatal("fixture key encryption failed")
	}
	t.Setenv("MINISIGN_PUBLIC_KEY", public.String())
	t.Setenv("MINISIGN_PRIVATE_KEY", string(encrypted))
	t.Setenv("MINISIGN_PASSWORD", "disposable-fixture-password")
	t.Setenv("RELEASE_MODE", "fixture")
	t.Setenv("RELEASE_VERSION", "v0.0.1")
	t.Setenv("PACKAGE_LICENSE", "LicenseRef-Fixture")
	commit, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RELEASE_COMMIT", strings.TrimSpace(string(commit)))
	for _, name := range []string{"SIGNPATH_API_TOKEN", "GITHUB_TOKEN", "SCOOP_BUCKET_TOKEN", "AUR_SSH_PRIVATE_KEY", "DOCKERHUB_TOKEN"} {
		t.Setenv(name, "")
	}
	for _, mode := range []string{"fixture", "nightly", "bootstrap"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("RELEASE_MODE", mode)
			version := "v0.0.1"
			if mode == "nightly" {
				version += "-nightly.20261112010101." + os.Getenv("RELEASE_COMMIT")
			}
			if mode == "bootstrap" {
				version += "-bootstrap.1"
			}
			t.Setenv("RELEASE_VERSION", version)
			id, err := identity()
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command("goreleaser", "release", "--snapshot", "--clean", "--skip=publish,docker", "--parallelism=1")
			if out, err := command.CombinedOutput(); err != nil {
				t.Fatalf("snapshot failed: %v\n%s", err, out[max(0, len(out)-6000):])
			}
			if err := run([]string{"verify", "dist"}); err != nil {
				t.Fatal(err)
			}
			if err := run([]string{"export", "dist"}); err != nil {
				t.Fatal(err)
			}
			verifyUpdaterArtifacts(t, id, public.String())
			verifyNativeHealth(t, id)
			if mode == "fixture" {
				verifyManagerReferences(t, id)
				if os.Getenv("RELEASE_CONTAINER_TEST") == "1" {
					verifyContainers(t, id)
				}
			}
			t.Run("missing signing configuration", func(t *testing.T) {
				t.Setenv("MINISIGN_PRIVATE_KEY", "")
				_ = os.Remove("dist/checksums.txt.minisig")
				if err := run([]string{"sign", "dist/checksums.txt"}); err == nil {
					t.Fatal("missing signing key did not fail closed")
				}
				if _, err := os.Stat("dist/checksums.txt.minisig"); !os.IsNotExist(err) {
					t.Fatal("created a signature without signing configuration")
				}
			})
			if err := run([]string{"sign", "dist/checksums.txt"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func verifyUpdaterArtifacts(t *testing.T, id updater.Identity, public string) {
	t.Helper()
	targetDir, err := filepath.Abs(".release/final")
	if err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/releases/") {
			assets := []map[string]string{}
			for _, name := range append(expectedFiles(id, true), "checksums.txt", "checksums.txt.minisig") {
				assets = append(assets, map[string]string{"name": name, "browser_download_url": server.URL + "/" + name})
			}
			tag := id.Version
			prerelease := strings.Contains(id.Version, "-")
			if id.Channel == "develop" {
				tag = "nightly"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "prerelease": prerelease, "assets": assets})
			return
		}
		http.ServeFile(w, r, filepath.Join(targetDir, filepath.Base(r.URL.Path)))
	}))
	defer server.Close()
	for _, target := range targets {
		for _, tampered := range []bool{false, true} {
			t.Run(target.os+"/"+target.arch+"/tampered="+map[bool]string{false: "no", true: "yes"}[tampered], func(t *testing.T) {
				dir := t.TempDir()
				db, err := storage.Open(filepath.Join(dir, "sync.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = db.Close() }()
				cfg := config.NewDefault()
				cfg.UpdateBranch = id.Channel
				e := &updater.Engine{DB: db, Client: server.Client(), APIBase: server.URL, PublicKey: public,
					Platform: target.os, Arch: target.arch, Current: updater.Identity{Version: "v0.0.0", Channel: "main"},
					Executable: filepath.Join(dir, "anilist-arr-sync"), Staging: filepath.Join(dir, "staging"),
					Config: func() config.Config { return *cfg }, Container: func(string) bool { return false }}
				if err := os.WriteFile(e.Executable, []byte("previous"), 0700); err != nil {
					t.Fatal(err)
				}
				name, _ := updater.ArchiveName(id.Version, target.os, target.arch)
				path := filepath.Join(targetDir, name)
				original, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if tampered {
					if err := os.WriteFile(path, append(original, 'x'), 0600); err != nil {
						t.Fatal(err)
					}
					defer func() { _ = os.WriteFile(path, original, 0644) }()
				}
				launched := false
				e.Launch = func(_ context.Context, planPath string) error {
					launched = true
					planBytes, err := os.ReadFile(planPath)
					if err != nil {
						return err
					}
					var plan updater.Plan
					if err := json.Unmarshal(planBytes, &plan); err != nil {
						return err
					}
					if plan.Expected != id {
						t.Fatal("updater lost build identity")
					}
					next, err := os.ReadFile(plan.Next)
					if err != nil {
						return err
					}
					want, err := archiveBinary(path, target.os)
					if err != nil {
						return err
					}
					if !bytes.Equal(next, want) {
						t.Fatal("updater staged different executable bytes")
					}
					return nil
				}
				err = e.Apply(context.Background())
				rejected := tampered || strings.Contains(id.Version, "-bootstrap.")
				if rejected && (err == nil || launched) {
					t.Fatal("tampered final archive passed updater")
				}
				if !rejected && (err != nil || !launched) {
					t.Fatalf("final artifact rejected: %v", err)
				}
			})
		}
	}
}

func verifyManagerReferences(t *testing.T, id updater.Identity) {
	t.Helper()
	command := exec.Command("python", "packaging/manifests.py", ".release/final")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("installer manifests: %v\n%s", err, out)
	}
	data, err := os.ReadFile("dist/scoop/anilist-arr-sync.json")
	if err != nil {
		t.Fatal(err)
	}
	var scoop struct {
		Architecture map[string]struct{ URL, Hash string }
	}
	if err := json.Unmarshal(data, &scoop); err != nil {
		t.Fatal(err)
	}
	aur, err := os.ReadFile("dist/aur/anilist-arr-sync-bin.pkgbuild")
	if err != nil {
		t.Fatal(err)
	}
	winget, err := os.ReadFile(".release/winget/lucasskywalker64.AniList-Arr-Sync.installer.yaml")
	if err != nil {
		t.Fatal(err)
	}
	choco, err := os.ReadFile(".release/chocolatey/tools/chocolateyinstall.ps1")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		name, _ := updater.ArchiveName(id.Version, target.os, target.arch)
		data, err := os.ReadFile(filepath.Join(".release/final", name))
		if err != nil {
			t.Fatal(err)
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(data))
		if target.os == "windows" {
			arch := "64bit"
			if target.arch == "arm64" {
				arch = "arm64"
			}
			entry := scoop.Architecture[arch]
			want := "https://github.com/lucasskywalker64/anilist-arr-sync/releases/download/" + id.Version + "/" + name
			if entry.URL != want || entry.Hash != hash {
				t.Fatal("Scoop does not reference final published archive/hash")
			}
			installer := installerName(id.Version, target.arch)
			data, err := os.ReadFile(filepath.Join(".release/final", installer))
			if err != nil {
				t.Fatal(err)
			}
			hash = fmt.Sprintf("%x", sha256.Sum256(data))
			for _, recipe := range [][]byte{winget, choco} {
				if !bytes.Contains(recipe, []byte(hash)) || !bytes.Contains(recipe, []byte(installer)) {
					t.Fatal("installer consumer does not reference final published bytes")
				}
			}
		} else {
			resolved := strings.ReplaceAll(string(aur), "${pkgver}", strings.TrimPrefix(id.Version, "v"))
			want := "https://github.com/lucasskywalker64/anilist-arr-sync/releases/download/" + id.Version + "/" + name
			if !strings.Contains(resolved, want) || !bytes.Contains(aur, []byte(hash)) {
				t.Fatal("AUR does not reference final Linux archive URL/hash")
			}
		}
	}
}

func verifyNativeHealth(t *testing.T, id updater.Identity) {
	t.Helper()
	name, err := updater.ArchiveName(id.Version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip("host is outside release matrix")
	}
	data, err := archiveBinary(filepath.Join(".release/final", name), runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "app")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, data, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strings.Split(listener.Addr().String(), ":")[1]
	_ = listener.Close()
	command := exec.Command(exe, "--no-browser", "--data-dir", filepath.Join(dir, "data"), "--bind-address", "127.0.0.1", "--port", port)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	client := &http.Client{Timeout: time.Second}
	base := "http://127.0.0.1:" + port
	deadline := time.Now().Add(30 * time.Second)
	for {
		response, err := client.Get(base + "/api/v1/health")
		if err == nil {
			var health struct{ Version, Commit, InstanceID string }
			_ = json.NewDecoder(response.Body).Decode(&health)
			_ = response.Body.Close()
			if response.StatusCode == 200 && health.Version == id.Version && health.Commit == id.Commit && health.InstanceID != "" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("packaged executable did not report exact health identity")
		}
		time.Sleep(100 * time.Millisecond)
	}
	response, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || !bytes.Contains(body, []byte("/assets/")) {
		t.Fatal("packaged frontend is missing")
	}
}

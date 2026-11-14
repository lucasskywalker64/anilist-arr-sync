package main

import (
	"archive/zip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aead.dev/minisign"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/updater"
)

func TestReleaseSourcesMustMatchCleanCommit(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("web/dist", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("web/dist/index.html", []byte("fixture frontend"), 0600); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		args = append([]string{"-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + filepath.Join(dir, "no-hooks"), "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git command failed: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet")
	git("add", "web")
	git("commit", "--quiet", "-m", "fixture source")
	head := git("rev-parse", "HEAD")
	public, _, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("fixture key generation failed")
	}
	t.Setenv("MINISIGN_PUBLIC_KEY", public.String())
	t.Setenv("RELEASE_MODE", "bootstrap")
	t.Setenv("RELEASE_VERSION", "v1.2.3-bootstrap.1")
	t.Setenv("RELEASE_COMMIT", head)
	t.Setenv("RELEASE_SNAPSHOT_TEST", "")
	if err := os.WriteFile("web/dist/index.html", []byte("uncommitted change"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"prepare"}); err == nil || !strings.Contains(err.Error(), "clean commit") {
		t.Fatal("release accepted sources that differ from the public commit")
	}
	git("checkout", "--", "web/dist/index.html")
	t.Setenv("RELEASE_COMMIT", strings.Repeat("a", 40))
	if err := run([]string{"prepare"}); err == nil || !strings.Contains(err.Error(), "source commit") {
		t.Fatal("release accepted an incorrect source identity")
	}
	t.Setenv("RELEASE_COMMIT", head)
	if err := run([]string{"prepare"}); err != nil {
		t.Fatal(err)
	}
}

func TestFinalArtifactsFailClosed(t *testing.T) {
	public, private, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("fixture key generation failed")
	}
	t.Setenv("MINISIGN_PUBLIC_KEY", public.String())
	id := updater.Identity{Version: "v1.2.3", Commit: strings.Repeat("a", 40), Channel: "main"}
	for _, damage := range []string{"none", "missing installer", "tampered archive", "duplicate checksum", "missing signature", "missing key", "wrong identity"} {
		t.Run(damage, func(t *testing.T) {
			dir := t.TempDir()
			manifest, _ := json.Marshal(id)
			assets := map[string][]byte{"release.json": manifest}
			for _, target := range targets {
				name, _ := updater.ArchiveName(id.Version, target.os, target.arch)
				assets[name] = []byte("final archive bytes")
			}
			for _, arch := range []string{"amd64", "arm64"} {
				assets[installerName(id.Version, arch)] = []byte("final installer bytes")
			}
			var sums strings.Builder
			for name, data := range assets {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(data), name)
			}
			checksum := []byte(sums.String())
			if damage == "duplicate checksum" {
				checksum = append(checksum, checksum...)
			}
			if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), checksum, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "checksums.txt.minisig"), minisign.Sign(private, checksum), 0600); err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "missing installer":
				_ = os.Remove(filepath.Join(dir, installerName(id.Version, "arm64")))
			case "tampered archive":
				name, _ := updater.ArchiveName(id.Version, "windows", "amd64")
				_ = os.WriteFile(filepath.Join(dir, name), []byte("changed after hashing"), 0600)
			case "missing signature":
				_ = os.Remove(filepath.Join(dir, "checksums.txt.minisig"))
			case "missing key":
				t.Setenv("MINISIGN_PUBLIC_KEY", "")
			case "wrong identity":
				id.Commit = strings.Repeat("b", 40)
			}
			err := verifyFiles(dir, id, true)
			if (err != nil) != (damage != "none") {
				t.Fatalf("verification for %s: %v", damage, err)
			}
		})
	}
}

func TestPortableArchiveRejectsOwnershipAndNestedExecutables(t *testing.T) {
	for _, entries := range [][]string{{"anilist-arr-sync.exe"}, {"sub/anilist-arr-sync.exe"}, {"anilist-arr-sync.exe", "update-owner"}, {"anilist-arr-sync.exe", "anilist-arr-sync.exe"}} {
		dir := t.TempDir()
		path := filepath.Join(dir, "portable.zip")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		w := zip.NewWriter(file)
		for _, name := range entries {
			entry, err := w.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = entry.Write([]byte("executable"))
		}
		_ = w.Close()
		_ = file.Close()
		_, err = archiveBinary(path, "windows")
		if (err == nil) != (len(entries) == 1 && entries[0] == "anilist-arr-sync.exe") {
			t.Fatalf("archive entries %v: %v", entries, err)
		}
	}
}

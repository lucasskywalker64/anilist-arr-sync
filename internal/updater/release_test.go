package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func TestApplyPrunesOnlyCompletedInactiveAttempts(t *testing.T) {
	f := newFixture(t, newBuild, "windows", "amd64", []byte("new"))
	e := fixtureEngine(t, f, "windows", "amd64")
	for _, name := range []string{"update-completed", "update-active", "update-unfinished"} {
		dir := filepath.Join(e.Staging, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "before.db"), []byte("recovery snapshot"), 0600); err != nil {
			t.Fatal(err)
		}
		if name != "update-unfinished" {
			pid := 2147483647
			if name == "update-active" {
				pid = os.Getpid()
			}
			if err := os.WriteFile(filepath.Join(dir, "completed"), []byte(strconv.Itoa(pid)), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	e.Launch = func(context.Context, string) error { return nil }
	if err := e.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.Staging, "update-completed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed attempt accumulated")
	}
	for _, name := range []string{"update-active", "update-unfinished"} {
		if _, err := os.Stat(filepath.Join(e.Staging, name, "before.db")); err != nil {
			t.Fatal("pruned active or incomplete recovery data")
		}
	}
}

func TestUnconfirmedWorkerRecoveryRetainsStaging(t *testing.T) {
	f := newFixture(t, newBuild, "windows", "amd64", []byte("new"))
	e := fixtureEngine(t, f, "windows", "amd64")
	var snapshot string
	e.Launch = func(_ context.Context, path string) error {
		snapshot = filepath.Join(filepath.Dir(path), "before.db")
		return ErrWorkerActive
	}
	if err := e.Apply(context.Background()); !errors.Is(err, ErrWorkerActive) {
		t.Fatalf("missing recovery error: %v", err)
	}
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatal("deleted recovery snapshot before worker restoration")
	}
	status, err := e.Status(context.Background())
	if err != nil || status.CanUpdate || !strings.Contains(status.Reason, "incomplete recovery") {
		t.Fatal("unconfirmed worker did not block retries")
	}
}

var oldBuild = Identity{Version: "v1.0.0", Commit: strings.Repeat("a", 40), Channel: "main"}
var newBuild = Identity{Version: "v1.1.0", Commit: strings.Repeat("b", 40), Channel: "main"}

type fixture struct {
	assets   map[string][]byte
	release  release
	server   *httptest.Server
	public   string
	private  minisign.PrivateKey
	requests []string
}

func archive(t *testing.T, platform string, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if platform == "windows" {
		w := zip.NewWriter(&buf)
		f, err := w.Create("anilist-arr-sync.exe")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(binary); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: "anilist-arr-sync", Mode: 0755, Size: int64(len(binary))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(binary); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func newFixture(t *testing.T, id Identity, platform, arch string, binary []byte) *fixture {
	t.Helper()
	public, private, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("fixture key generation failed")
	}
	f := &fixture{assets: make(map[string][]byte), public: public.String(), private: private}
	manifest, _ := json.Marshal(id)
	name, err := ArchiveName(id.Version, platform, arch)
	if err != nil {
		t.Fatal(err)
	}
	f.assets["release.json"], f.assets[name] = manifest, archive(t, platform, binary)
	f.sign()
	f.release.Tag = id.Version
	if id.Channel == "develop" {
		f.release.Tag, f.release.Prerelease = "nightly", true
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/releases/") {
			_ = json.NewEncoder(w).Encode(f.release)
			return
		}
		b, ok := f.assets[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(f.server.Close)
	for asset := range f.assets {
		f.release.Assets = append(f.release.Assets, struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}{asset, f.server.URL + "/" + asset})
	}
	return f
}

func (f *fixture) sign() {
	var b strings.Builder
	for name, data := range f.assets {
		if name == "checksums.txt" || name == "checksums.txt.minisig" {
			continue
		}
		fmt.Fprintf(&b, "%x  %s\n", sha256.Sum256(data), name)
	}
	f.assets["checksums.txt"] = []byte(b.String())
	r := minisign.NewReader(bytes.NewReader(f.assets["checksums.txt"]))
	_, _ = io.Copy(io.Discard, r)
	f.assets["checksums.txt.minisig"] = r.Sign(f.private)
}

func fixtureEngine(t *testing.T, f *fixture, platform, arch string) *Engine {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.NewDefault()
	cfg.UpdateBranch = fIdentity(t, f).Channel
	e := &Engine{DB: db, Client: f.server.Client(), APIBase: f.server.URL, PublicKey: f.public,
		Platform: platform, Arch: arch, Current: oldBuild, Executable: filepath.Join(dir, "app"), Staging: filepath.Join(dir, "staging"),
		Args:   []string{"--config", filepath.Join(dir, "custom.xml"), "--data-dir", dir, "--port", "7171"},
		Config: func() config.Config { return *cfg }, Container: func(string) bool { return false }}
	if err := os.WriteFile(e.Executable, []byte("old binary"), 0700); err != nil {
		t.Fatal(err)
	}
	return e
}

func fIdentity(t *testing.T, f *fixture) Identity {
	t.Helper()
	var id Identity
	if err := json.Unmarshal(f.assets["release.json"], &id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestReleaseVerification(t *testing.T) {
	for _, branch := range []string{"main", "develop"} {
		id := newBuild
		if branch == "develop" {
			id = Identity{Version: "v1.2.0-nightly.20261111010000.bbbbbbb", Commit: newBuild.Commit, Channel: branch}
		}
		for _, platform := range []string{"linux", "windows"} {
			for _, arch := range []string{"amd64", "arm64"} {
				t.Run(branch+"/"+platform+"/"+arch, func(t *testing.T) {
					for _, damage := range []string{"none", "archive", "signature", "checksums", "manifest", "missing signature", "missing archive checksum", "duplicate checksum", "wrong key", "missing key", "invalid key"} {
						t.Run(damage, func(t *testing.T) {
							f := newFixture(t, id, platform, arch, []byte("new executable"))
							e := fixtureEngine(t, f, platform, arch)
							name, _ := ArchiveName(id.Version, platform, arch)
							switch damage {
							case "archive":
								f.assets[name] = []byte("tampered")
							case "signature":
								f.assets["checksums.txt.minisig"] = []byte("invalid")
							case "checksums":
								f.assets["checksums.txt"] = append(f.assets["checksums.txt"], '\n', 'x')
							case "manifest":
								f.assets["release.json"] = []byte(`{"version":"v99.0.0"}`)
							case "missing signature":
								delete(f.assets, "checksums.txt.minisig")
							case "missing archive checksum":
								delete(f.assets, name)
								f.sign()
								f.assets[name] = archive(t, platform, []byte("new executable"))
							case "duplicate checksum":
								f.assets["checksums.txt"] = append(f.assets["checksums.txt"], f.assets["checksums.txt"]...)
								f.assets["checksums.txt.minisig"] = minisign.Sign(f.private, f.assets["checksums.txt"])
							case "wrong key":
								k, _, err := minisign.GenerateKey(rand.Reader)
								if err != nil {
									t.Fatal("fixture key generation failed")
								}
								b, _ := k.MarshalText()
								e.PublicKey = string(b)
							case "missing key":
								e.PublicKey = ""
							case "invalid key":
								e.PublicKey = "invalid"
							}
							launched := false
							e.Launch = func(_ context.Context, path string) error {
								launched = true
								b, err := os.ReadFile(path)
								if err != nil {
									return err
								}
								var p Plan
								if err := json.Unmarshal(b, &p); err != nil {
									return err
								}
								if p.Expected != id || strings.Join(p.Args, "|") != strings.Join(e.Args, "|") {
									t.Error("lost build identity or launch arguments")
								}
								if err := storage.CheckFileIntegrity(context.Background(), p.Snapshot); err != nil {
									t.Error(err)
								}
								staged, err := os.ReadFile(p.Next)
								if err != nil || string(staged) != "new executable" {
									t.Error("incorrect staged executable")
								}
								return nil
							}
							err := e.Apply(context.Background())
							if damage == "none" {
								if err != nil || !launched {
									t.Fatalf("valid release failed: %v", err)
								}
							} else {
								if err == nil || launched {
									t.Fatalf("accepted %s", damage)
								}
								if _, err := os.Stat(e.Staging); !os.IsNotExist(err) {
									t.Error("rejected release reached snapshot/staging")
								}
							}
							b, _ := os.ReadFile(e.Executable)
							if string(b) != "old binary" {
								t.Error("modified running executable")
							}
						})
					}
				})
			}
		}
	}
}

func TestChannelIsolationAndNightlyCooldown(t *testing.T) {
	id := Identity{Version: "v1.2.0-nightly.20261111010000.bbbbbbb", Commit: newBuild.Commit, Channel: "develop"}
	f := newFixture(t, id, "linux", "amd64", []byte("new"))
	e := fixtureEngine(t, f, "linux", "amd64")
	status, err := e.Status(context.Background())
	if err != nil || !status.CanUpdate {
		t.Fatalf("nightly check: %v", err)
	}
	if f.requests[0] != "/releases/tags/nightly" {
		t.Fatal("wrong nightly endpoint")
	}
	if e.Config().UpdateAutomatically {
		t.Fatal("channel enabled automatic updates")
	}
	if err := record(context.Background(), e.DB.Path(), id, fmt.Errorf("failed fixture"), time.Now()); err != nil {
		t.Fatal(err)
	}
	status, err = e.Status(context.Background())
	if err != nil || status.CanUpdate {
		t.Fatalf("cooldown missing: %v", err)
	}
	later := id
	later.Commit = strings.Repeat("c", 40)
	blocked, err := Blocked(context.Background(), e.DB, later, time.Now())
	if err != nil || blocked {
		t.Fatal("prior nightly blocked a different commit")
	}
	blocked, err = Blocked(context.Background(), e.DB, id, time.Now().Add(25*time.Hour))
	if err != nil || blocked {
		t.Fatal("cooldown did not expire")
	}
	manifest, _ := json.Marshal(later)
	f.assets["release.json"] = manifest
	f.sign()
	status, err = e.Status(context.Background())
	if err != nil || !status.CanUpdate || status.LatestCommit != later.Commit {
		t.Fatal("later authenticated nightly was blocked")
	}
	cfg := e.Config()
	cfg.UpdateBranch = "main"
	e.Config = func() config.Config { return cfg }
	f.requests = nil
	if _, err := e.Status(context.Background()); err == nil {
		t.Fatal("stable accepted nightly")
	}
	if f.requests[0] != "/releases/latest" {
		t.Fatal("wrong stable endpoint")
	}
	f.release.Prerelease = false
	if _, err := e.Status(context.Background()); err == nil {
		t.Fatal("stable accepted rolling tag")
	}
}

func TestInstallationRestrictions(t *testing.T) {
	for _, owner := range []string{"scoop", "aur", "winget", "chocolatey", "another-manager", "", "unreadable", "legacy-package", "docker"} {
		t.Run(owner, func(t *testing.T) {
			f := newFixture(t, newBuild, "windows", "amd64", []byte("new"))
			e := fixtureEngine(t, f, "windows", "amd64")
			marker := filepath.Join(filepath.Dir(e.Executable), "update-owner")
			switch owner {
			case "docker":
				e.Container = func(string) bool { return true }
			case "legacy-package":
				cfg := e.Config()
				cfg.UpdateMechanism = "Package"
				e.Config = func() config.Config { return cfg }
			case "unreadable":
				if err := os.Mkdir(marker, 0700); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(marker, []byte(owner), 0600); err != nil {
					t.Fatal(err)
				}
			}
			status, err := e.Status(context.Background())
			if err != nil || status.CanUpdate || status.Reason == "" {
				t.Fatal("restriction not exposed")
			}
			if err := e.Apply(context.Background()); err == nil {
				t.Fatal("restricted apply succeeded")
			}
			if len(f.requests) != 0 {
				t.Fatal("restricted installation contacted releases")
			}
		})
	}
}

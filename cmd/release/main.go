// Command release validates the artifact boundary shared with the updater.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"aead.dev/minisign"
	licensing "github.com/lucasskywalker64/anilist-arr-sync"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/updater"
)

var targets = []struct{ os, arch string }{{"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}}
var stable = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var nightly = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-nightly\.[0-9]+\.[0-9a-f]+$`)
var bootstrap = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-bootstrap\.[0-9]+$`)
var sha = regexp.MustCompile(`^[0-9a-f]{40}$`)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func identity() (updater.Identity, error) {
	id := updater.Identity{Version: os.Getenv("RELEASE_VERSION"), Commit: os.Getenv("RELEASE_COMMIT"), Channel: "main"}
	valid := false
	switch os.Getenv("RELEASE_MODE") {
	case "production":
		valid = stable.MatchString(id.Version)
	case "bootstrap":
		valid = bootstrap.MatchString(id.Version)
	case "nightly":
		id.Channel = "develop"
		valid = nightly.MatchString(id.Version) && strings.HasSuffix(id.Version, "."+id.Commit)
	case "fixture":
		valid = stable.MatchString(id.Version)
	}
	if !valid || !sha.MatchString(id.Commit) {
		return id, errors.New("invalid release mode, version, or full commit identity")
	}
	return id, nil
}

func publicKey() (minisign.PublicKey, error) {
	var key minisign.PublicKey
	value := os.Getenv("MINISIGN_PUBLIC_KEY")
	if strings.ContainsAny(value, " \r\n\t") || key.UnmarshalText([]byte(value)) != nil {
		return key, errors.New("MINISIGN_PUBLIC_KEY must be the trusted Minisign base64 public-key line")
	}
	return key, nil
}

func installerName(version, arch string) string {
	return "anilist-arr-sync_" + version + "_windows_" + arch + "_setup.exe"
}

func expectedFiles(id updater.Identity, installers bool) []string {
	names := []string{"release.json"}
	for _, target := range targets {
		name, _ := updater.ArchiveName(id.Version, target.os, target.arch)
		names = append(names, name)
	}
	if installers {
		for _, arch := range []string{"amd64", "arm64"} {
			names = append(names, installerName(id.Version, arch))
		}
	}
	return names
}

func checksumEntries(data []byte) (map[string]string, error) {
	sums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		parts := strings.Split(line, "  ")
		if len(parts) != 2 || len(parts[0]) != 64 || strings.ToLower(parts[0]) != parts[0] || filepath.Base(parts[1]) != parts[1] || strings.ContainsAny(parts[1], "\\/\r\n") {
			return nil, errors.New("invalid checksum line")
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return nil, errors.New("invalid SHA256")
		}
		if _, exists := sums[parts[1]]; exists {
			return nil, errors.New("duplicate checksum entry")
		}
		sums[parts[1]] = parts[0]
	}
	return sums, nil
}

func verifyFiles(dir string, id updater.Identity, installers bool) error {
	key, err := publicKey()
	if err != nil {
		return err
	}
	checksums, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(filepath.Join(dir, "checksums.txt.minisig"))
	if err != nil || !minisign.Verify(key, checksums, sig) {
		return errors.New("missing or invalid checksum signature")
	}
	return verifyHashes(dir, id, installers, checksums)
}

func verifyHashes(dir string, id updater.Identity, installers bool, checksums []byte) error {
	sums, err := checksumEntries(checksums)
	if err != nil {
		return err
	}
	names := expectedFiles(id, installers)
	if len(names) != len(sums) {
		return errors.New("checksum coverage differs from required final artifacts")
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != sums[name] {
			return fmt.Errorf("final checksum mismatch: %s", name)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "release.json"))
	if err != nil {
		return err
	}
	var actual updater.Identity
	if err := json.Unmarshal(data, &actual); err != nil || actual != id {
		return errors.New("signed release identity does not match expected source")
	}
	return nil
}

func archiveBinary(path, platform string) ([]byte, error) {
	name := "anilist-arr-sync"
	if platform == "windows" {
		name += ".exe"
		z, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = z.Close() }()
		if len(z.File) != 1 || z.File[0].Name != name || !z.File[0].Mode().IsRegular() {
			return nil, errors.New("portable ZIP must contain only the root executable")
		}
		r, err := z.File[0].Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = r.Close() }()
		return io.ReadAll(io.LimitReader(r, 256<<20))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	h, err := tr.Next()
	if err != nil || h.Name != name || h.Typeflag != tar.TypeReg || h.Mode&0111 == 0 {
		return nil, errors.New("portable tar must contain the executable at its root")
	}
	data, err := io.ReadAll(io.LimitReader(tr, 256<<20))
	if err != nil {
		return nil, err
	}
	if _, err := tr.Next(); err != io.EOF {
		return nil, errors.New("unexpected portable tar entry")
	}
	return data, nil
}

func verifyBinaries(dir string, id updater.Identity) error {
	key := os.Getenv("MINISIGN_PUBLIC_KEY")
	for _, target := range targets {
		name, _ := updater.ArchiveName(id.Version, target.os, target.arch)
		data, err := archiveBinary(filepath.Join(dir, name), target.os)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		info, err := buildinfo.Read(bytes.NewReader(data))
		if err != nil {
			return err
		}
		settings := map[string]string{}
		for _, setting := range info.Settings {
			settings[setting.Key] = setting.Value
		}
		if settings["GOOS"] != target.os || settings["GOARCH"] != target.arch || settings["CGO_ENABLED"] != "0" {
			return fmt.Errorf("incorrect binary target: %s", name)
		}
		for _, input := range []string{"main.version=" + id.Version, "main.commit=" + id.Commit, "main.updatePublicKey=" + key, "main.date="} {
			if !strings.Contains(settings["-ldflags"], "-X "+input) {
				return fmt.Errorf("missing binary identity/trust input in %s", name)
			}
		}
		if target.os == "windows" && runtime.GOOS == "windows" && os.Getenv("RELEASE_MODE") == "production" {
			if err := verifyAuthenticode(data); err != nil {
				return err
			}
		}
	}
	return nil
}

func verifyAuthenticode(data []byte) error {
	dir, err := os.MkdirTemp(".release", "verify-signature-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(filepath.Join(dir, "app.exe")); _ = os.Remove(dir) }()
	path := filepath.Join(dir, "app.exe")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	return exec.Command("pwsh", "-NoProfile", "-File", "packaging/verify-authenticode.ps1", "-Path", path).Run()
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected prepare, sign, verify, or export")
	}
	if args[0] == "verify-nightly" && len(args) == 2 {
		return verifyNightly(args[1])
	}
	if args[0] == "check-key" && len(args) == 1 {
		_, err := publicKey()
		return err
	}
	id, err := identity()
	if err != nil {
		return err
	}
	if _, err := publicKey(); err != nil {
		return err
	}
	switch args[0] {
	case "config":
		// Scoop's license field is literal. Render common public package metadata;
		// signing inputs remain environment references.
		license := os.Getenv("PACKAGE_LICENSE")
		if os.Getenv("RELEASE_MODE") == "production" && license == "" {
			return errors.New("PACKAGE_LICENSE is required for production publishers")
		}
		if os.Getenv("RELEASE_MODE") == "production" && license != licensing.Identifier {
			return errors.New("PACKAGE_LICENSE must match the application's AGPL-3.0-only license")
		}
		data, err := os.ReadFile(".goreleaser.yaml")
		if err != nil {
			return err
		}
		literal, err := json.Marshal(license)
		if err != nil {
			return err
		}
		data = bytes.ReplaceAll(data, []byte("'{{ .Env.PACKAGE_LICENSE }}'"), literal)
		if err := os.MkdirAll(".release-tools", 0755); err != nil {
			return err
		}
		return os.WriteFile(".release-tools/goreleaser.yaml", data, 0644)
	case "prepare":
		if _, err := os.Stat("web/dist/index.html"); err != nil {
			return errors.New("build the frontend before release compilation")
		}
		if os.Getenv("RELEASE_MODE") != "fixture" && os.Getenv("RELEASE_SNAPSHOT_TEST") != "1" {
			head, err := exec.Command("git", "rev-parse", "HEAD").Output()
			if err != nil || strings.TrimSpace(string(head)) != id.Commit {
				return errors.New("release source commit does not match the embedded identity")
			}
			status, err := exec.Command("git", "status", "--porcelain", "--untracked-files=normal").Output()
			if err != nil || len(bytes.TrimSpace(status)) != 0 {
				return errors.New("release source must be a clean commit so the source offer matches the binaries")
			}
		}
		if err := os.MkdirAll(".release", 0755); err != nil {
			return err
		}
		stale, err := filepath.Glob(".release/*_setup.exe")
		if err != nil {
			return err
		}
		for _, path := range stale {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
		for _, arch := range []string{"amd64", "arm64"} {
			if err := os.Remove(filepath.Join(".release", "installer-verified-"+arch)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		b, err := json.Marshal(id)
		if err != nil {
			return err
		}
		return os.WriteFile(".release/release.json", append(b, '\n'), 0644)
	case "sign":
		if len(args) != 2 {
			return errors.New("expected sign <checksums path>")
		}
		dir := filepath.Dir(args[1])
		if err := copyExtras(dir); err != nil {
			return err
		}
		checksums, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		if err := verifyHashes(dir, id, true, checksums); err != nil {
			return err
		}
		if err := verifyBinaries(dir, id); err != nil {
			return err
		}
		if os.Getenv("RELEASE_MODE") == "production" {
			for _, arch := range []string{"amd64", "arm64"} {
				command := exec.Command("pwsh", "-NoProfile", "-File", "packaging/verify-authenticode.ps1", "-Path", filepath.Join(dir, installerName(id.Version, arch)))
				if err := command.Run(); err != nil {
					return errors.New("final installer Authenticode verification failed")
				}
				receipt, err := os.ReadFile(filepath.Join(".release", "installer-verified-"+arch))
				if err != nil {
					return errors.New("both installer lifecycles must pass before signing checksums")
				}
				installer, err := os.ReadFile(filepath.Join(dir, installerName(id.Version, arch)))
				if err != nil {
					return err
				}
				if strings.TrimSpace(string(receipt)) != fmt.Sprintf("%s %x", id.Commit, sha256.Sum256(installer)) {
					return errors.New("installer verification receipt does not cover final bytes and source commit")
				}
			}
		}
		private := []byte(os.Getenv("MINISIGN_PRIVATE_KEY"))
		password := os.Getenv("MINISIGN_PASSWORD")
		if len(private) == 0 || password == "" || !minisign.IsEncrypted(private) {
			return errors.New("password-protected MINISIGN_PRIVATE_KEY and MINISIGN_PASSWORD are required")
		}
		secret, err := minisign.DecryptKey(password, private)
		if err != nil {
			return errors.New("cannot decrypt configured Minisign key")
		}
		key, _ := publicKey()
		if !key.Equal(secret.Public()) {
			return errors.New("signing key does not match embedded verification key")
		}
		if err := os.WriteFile(args[1]+".minisig", minisign.Sign(secret, checksums), 0644); err != nil {
			return err
		}
		return verifyFiles(dir, id, true)
	case "verify", "export":
		if len(args) != 2 {
			return errors.New("expected verify/export <artifact directory>")
		}
		if err := verifyFiles(args[1], id, true); err != nil {
			return err
		}
		if err := verifyBinaries(args[1], id); err != nil {
			return err
		}
		if args[0] == "export" {
			if err := os.MkdirAll(".release/final", 0755); err != nil {
				return err
			}
			old, err := os.ReadDir(".release/final")
			if err != nil {
				return err
			}
			for _, entry := range old {
				if !entry.Type().IsRegular() {
					return errors.New("unexpected entry in final output directory")
				}
				if err := os.Remove(filepath.Join(".release/final", entry.Name())); err != nil {
					return err
				}
			}
			for _, name := range append(expectedFiles(id, true), "checksums.txt", "checksums.txt.minisig") {
				b, err := os.ReadFile(filepath.Join(args[1], name))
				if err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(".release/final", name), b, 0644); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return errors.New("unknown release operation")
	}
}

func verifyNightly(dir string) error {
	key, err := publicKey()
	if err != nil {
		return err
	}
	sums, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(filepath.Join(dir, "checksums.txt.minisig"))
	if err != nil || !minisign.Verify(key, sums, sig) {
		return errors.New("previous nightly signature is missing or invalid")
	}
	entries, err := checksumEntries(sums)
	if err != nil {
		return err
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "release.json"))
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(manifest)) != entries["release.json"] {
		return errors.New("previous nightly manifest checksum mismatch")
	}
	var id updater.Identity
	if json.Unmarshal(manifest, &id) != nil || !nightly.MatchString(id.Version) || !sha.MatchString(id.Commit) || id.Channel != "develop" {
		return errors.New("invalid previous nightly identity")
	}
	return nil
}

func copyExtras(dir string) error {
	files, err := filepath.Glob(".release/*_setup.exe")
	if err != nil {
		return err
	}
	for _, path := range append(files, ".release/release.json") {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(path)), data, 0644); err != nil {
			return err
		}
	}
	return nil
}

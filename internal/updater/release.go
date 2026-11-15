// Package updater verifies releases and coordinates recoverable binary updates.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"aead.dev/minisign"
	"golang.org/x/mod/semver"
)

// Identity identifies the actual build, independently of a rolling release tag.
type Identity struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Channel string `json:"channel"`
}

// Key identifies an exact build for history and cooldown persistence.
func (i Identity) Key() string { return i.Channel + ":" + i.Version + ":" + i.Commit }

var stableVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var nightlyVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-nightly\.[0-9]+\.[0-9a-f]+$`)

// ArchiveName defines the filenames shared with the release producer.
func ArchiveName(version, platform, arch string) (string, error) {
	if (platform != "linux" && platform != "windows") || (arch != "amd64" && arch != "arm64") {
		return "", errors.New("unsupported update platform")
	}
	ext := ".tar.gz"
	if platform == "windows" {
		ext = ".zip"
	}
	return "anilist-arr-sync_" + version + "_" + platform + "_" + arch + ext, nil
}

type release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Body       string `json:"body"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type candidate struct {
	Identity
	archive, url, notes string
	sums                map[string]string
}

func fetch(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release download: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("release asset exceeds size limit")
	}
	return b, err
}

func checksums(data []byte) (map[string]string, error) {
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != 64 {
			return nil, errors.New("invalid checksum line")
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return nil, err
		}
		if _, exists := result[fields[1]]; exists {
			return nil, errors.New("duplicate checksum entry")
		}
		result[fields[1]] = fields[0]
	}
	return result, nil
}

func verifyChecksum(data []byte, name string, sums map[string]string) error {
	want, ok := sums[name]
	if !ok {
		return fmt.Errorf("missing checksum for %s", name)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

func (e *Engine) discover(ctx context.Context, branch string) (*candidate, error) {
	var key minisign.PublicKey
	if err := key.UnmarshalText([]byte(e.PublicKey)); err != nil {
		return nil, errors.New("trusted Minisign public key is missing or invalid")
	}
	endpoint := "/releases/latest"
	if branch == "develop" {
		endpoint = "/releases/tags/nightly"
	} else if branch != "main" {
		return nil, errors.New("unsupported update branch")
	}
	b, err := fetch(ctx, e.Client, e.APIBase+endpoint, 2<<20)
	if err != nil {
		return nil, err
	}
	var r release
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if r.Draft || (branch == "main" && (r.Prerelease || !stableVersion.MatchString(r.Tag))) ||
		(branch == "develop" && (!r.Prerelease || r.Tag != "nightly")) {
		return nil, errors.New("release is outside selected channel")
	}
	assets := make(map[string]string)
	for _, a := range r.Assets {
		if _, exists := assets[a.Name]; exists {
			return nil, errors.New("duplicate release asset")
		}
		assets[a.Name] = a.URL
	}
	get := func(name string) ([]byte, error) {
		if assets[name] == "" {
			return nil, fmt.Errorf("missing release asset %s", name)
		}
		return fetch(ctx, e.Client, assets[name], 2<<20)
	}
	signed, err := get("checksums.txt")
	if err != nil {
		return nil, err
	}
	sig, err := get("checksums.txt.minisig")
	if err != nil {
		return nil, err
	}
	if !minisign.Verify(key, signed, sig) {
		return nil, errors.New("invalid Minisign checksum signature")
	}
	sums, err := checksums(signed)
	if err != nil {
		return nil, err
	}
	b, err = get("release.json")
	if err != nil {
		return nil, err
	}
	if err := verifyChecksum(b, "release.json", sums); err != nil {
		return nil, err
	}
	var id Identity
	if err := json.Unmarshal(b, &id); err != nil {
		return nil, err
	}
	if id.Channel != branch || !commitSHA.MatchString(id.Commit) ||
		(branch == "main" && id.Version != r.Tag) ||
		(branch == "develop" && (!nightlyVersion.MatchString(id.Version) || !semver.IsValid(id.Version))) {
		return nil, errors.New("invalid signed release identity")
	}
	name, err := ArchiveName(id.Version, e.Platform, e.Arch)
	if err != nil {
		return nil, err
	}
	if assets[name] == "" || sums[name] == "" {
		return nil, errors.New("missing exact archive or checksum")
	}
	return &candidate{Identity: id, archive: name, url: assets[name], notes: r.Body, sums: sums}, nil
}

// executable reads only a single regular root executable from an authenticated archive.
// Nothing from the archive is ever extracted by its supplied path.
func executable(data []byte, platform string) ([]byte, error) {
	name := "anilist-arr-sync"
	if platform == "windows" {
		name += ".exe"
	}
	var result []byte
	read := func(r io.Reader) error {
		if result != nil {
			return errors.New("duplicate executable in archive")
		}
		b, err := io.ReadAll(io.LimitReader(r, (256<<20)+1))
		if len(b) == 0 || len(b) > 256<<20 {
			return errors.New("invalid executable size")
		}
		result = b
		return err
	}
	if platform == "windows" {
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range z.File {
			if f.Name != name {
				continue
			}
			if !f.Mode().IsRegular() {
				return nil, errors.New("archive executable is not regular")
			}
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			err = read(r)
			closeErr := r.Close()
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer func() { _ = gz.Close() }()
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if h.Name != name {
				continue
			}
			if h.Typeflag != tar.TypeReg {
				return nil, errors.New("archive executable is not regular")
			}
			if err := read(tr); err != nil {
				return nil, err
			}
		}
	}
	if result == nil {
		return nil, errors.New("root executable missing from archive")
	}
	return result, nil
}

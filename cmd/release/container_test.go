package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/updater"
)

func TestContainerArtifacts(t *testing.T) {
	if os.Getenv("RELEASE_CONTAINER_TEST") != "1" {
		t.Skip("set RELEASE_CONTAINER_TEST=1 with verified release artifacts and Docker available")
	}
	t.Chdir(filepath.Join("..", ".."))
	id, err := identity()
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"verify", ".release/final"}); err != nil {
		t.Fatal(err)
	}
	verifyContainers(t, id)
}

// These images are local, uniquely named, and removed. No login or push occurs.
func verifyContainers(t *testing.T, id updater.Identity) {
	t.Helper()
	context := t.TempDir()
	dockerfile, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(context, "Dockerfile"), dockerfile, 0600); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(context, "packaging", "container-config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".keep"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		name, _ := updater.ArchiveName(id.Version, "linux", arch)
		binary, err := archiveBinary(filepath.Join(".release/final", name), "linux")
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(context, "linux", arch)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "anilist-arr-sync"), binary, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run("container/"+arch, func(t *testing.T) {
			tag := fmt.Sprintf("anilist-arr-sync:fixture-%d-%s", time.Now().UnixNano(), arch)
			build := exec.Command("docker", "buildx", "build", "--load", "--platform", "linux/"+arch, "--tag", tag, context)
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("local container build: %v\n%s", err, out[max(0, len(out)-3000):])
			}
			defer func() { _ = exec.Command("docker", "image", "rm", tag).Run() }()
			out, err := exec.Command("docker", "run", "--detach", "--platform", "linux/"+arch, "--publish", "127.0.0.1::7171", tag).Output()
			if err != nil {
				t.Fatalf("container start: %v", err)
			}
			container := strings.TrimSpace(string(out))
			defer func() { _ = exec.Command("docker", "rm", "--force", "--volumes", container).Run() }()
			port, err := exec.Command("docker", "port", container, "7171/tcp").Output()
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Timeout: time.Second}
			deadline := time.Now().Add(time.Minute)
			for {
				response, err := client.Get("http://" + strings.TrimSpace(string(port)) + "/api/v1/health")
				if err == nil {
					var health struct{ Version, Commit, InstanceID string }
					_ = json.NewDecoder(response.Body).Decode(&health)
					_ = response.Body.Close()
					if response.StatusCode == 200 && health.Version == id.Version && health.Commit == id.Commit && health.InstanceID != "" {
						break
					}
				}
				if time.Now().After(deadline) {
					logs, _ := exec.Command("docker", "logs", "--tail", "10", container).CombinedOutput()
					t.Fatalf("container did not start with writable data volume and exact identity: %s", logs)
				}
				time.Sleep(100 * time.Millisecond)
			}
		})
	}
}

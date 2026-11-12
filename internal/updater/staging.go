package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func completeAttempt(path string, p Plan) error {
	// The running Windows worker cannot delete itself. Remove bulky recovery
	// files now, then let the next attempt prune this directory after worker exit.
	err := os.WriteFile(filepath.Join(filepath.Dir(path), "completed"), []byte(strconv.Itoa(os.Getpid())), 0600)
	return errors.Join(err, os.Remove(p.Next), os.Remove(p.Snapshot))
}

func pruneCompleted(staging string) error {
	entries, err := os.ReadDir(staging)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "update-") {
			continue
		}
		dir := filepath.Join(staging, entry.Name())
		b, err := os.ReadFile(filepath.Join(dir, "completed"))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(string(b))
		if err != nil || pid <= 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		exited := waitProcess(ctx, pid) == nil
		cancel()
		if exited {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

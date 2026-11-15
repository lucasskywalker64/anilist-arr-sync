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
	var cleanupErr error
	for _, file := range []string{p.Next, p.Snapshot} {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	err := os.WriteFile(filepath.Join(filepath.Dir(path), "completed"), []byte(strconv.Itoa(os.Getpid())), 0600)
	return errors.Join(cleanupErr, err)
}

func completedWorker(dir string) int {
	b, err := os.ReadFile(filepath.Join(dir, "completed"))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(string(b))
	return pid
}

func finishAttempt(path string, p Plan) error {
	err := completeAttempt(path, p)
	if completedWorker(filepath.Dir(path)) == os.Getpid() {
		if lockErr := os.Remove(p.Lock); lockErr != nil && !errors.Is(lockErr, os.ErrNotExist) {
			err = errors.Join(err, lockErr)
		}
	}
	return err
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
		pid := completedWorker(dir)
		if pid <= 0 {
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

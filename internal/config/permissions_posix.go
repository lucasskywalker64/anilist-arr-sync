//go:build !windows

package config

import (
	"fmt"
	"os"
)

// enforceFilePermissions locks down file permissions to POSIX 0600.
func enforceFilePermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat config file %s: %w", path, err)
	}

	if info.Mode().Perm() != 0600 {
		if err := os.Chmod(path, 0600); err != nil {
			return fmt.Errorf("failed to enforce 0600 permissions on %s: %w", path, err)
		}
	}

	return nil
}

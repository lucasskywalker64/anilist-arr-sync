//go:build windows

package config

// enforceFilePermissions is a no-op on Windows systems as POSIX file mode bits do not apply.
func enforceFilePermissions(path string) error {
	return nil
}

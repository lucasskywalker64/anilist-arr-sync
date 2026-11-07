package backup

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// Type represents the classification of a backup archive.
type Type string

const (
	// TypeScheduled represents an automated scheduled backup.
	TypeScheduled Type = "scheduled"
	// TypeManual represents a user-triggered on-demand backup.
	TypeManual Type = "manual"
	// TypeUpdate represents an automated pre-update backup snapshot.
	TypeUpdate Type = "update"

	// TimeFormat defines the timestamp pattern used in backup filenames.
	TimeFormat = "2006.01.02_15.04.05"
	// Prefix defines the standard backup file name prefix.
	Prefix = "anilist-arr-sync_backup_"
	// Extension defines the backup zip file extension.
	Extension = ".zip"
	// DefaultRetention defines the default retention window in days.
	DefaultRetention = 7
)

var filenameRegex = regexp.MustCompile(`^anilist-arr-sync_backup_([a-zA-Z0-9_]+)_(\d{4}\.\d{2}\.\d{2}_\d{2}\.\d{2}\.\d{2})\.zip$`)

// Info captures metadata about a backup archive.
type Info struct {
	Filename  string    `json:"filename"`
	Path      string    `json:"path"`
	Type      Type      `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	SizeBytes int64     `json:"size_bytes,omitempty"`
}

// FormatBackupFilename generates the canonical archive name:
// anilist-arr-sync_backup_{type}_YYYY.MM.DD_HH.MM.SS.zip
func FormatBackupFilename(backupType Type, t time.Time) string {
	if backupType == "" {
		backupType = TypeManual
	}
	timestamp := t.UTC().Format(TimeFormat)
	return fmt.Sprintf("%s%s_%s%s", Prefix, backupType, timestamp, Extension)
}

// ParseBackupFilename extracts the backup type and creation timestamp from an archive filename.
func ParseBackupFilename(path string) (*Info, error) {
	filename := filepath.Base(path)
	matches := filenameRegex.FindStringSubmatch(filename)
	if len(matches) != 3 {
		return nil, fmt.Errorf("backup: invalid archive filename format: %q", filename)
	}

	createdAt, err := time.Parse(TimeFormat, matches[2])
	if err != nil {
		return nil, fmt.Errorf("backup: invalid timestamp in filename %q: %w", filename, err)
	}

	return &Info{
		Filename:  filename,
		Path:      path,
		Type:      Type(matches[1]),
		CreatedAt: createdAt,
	}, nil
}

// SelectBackupsToPrune returns archives at least retentionDays old,
// ordered from oldest to newest.
func SelectBackupsToPrune(backups []Info, retentionDays int, now time.Time) []Info {
	if retentionDays <= 0 {
		retentionDays = DefaultRetention
	}
	cutoff := now.UTC().AddDate(0, 0, -retentionDays)
	var expired []Info
	for _, item := range backups {
		if !item.CreatedAt.After(cutoff) {
			expired = append(expired, item)
		}
	}
	sort.Slice(expired, func(i, j int) bool {
		return expired[i].CreatedAt.Before(expired[j].CreatedAt)
	})
	return expired
}

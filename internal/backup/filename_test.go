package backup_test

import (
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
)

func TestFormatBackupFilename(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 10, 9, 23, 15, 30, 0, time.UTC)
	tests := []struct {
		name       string
		backupType backup.Type
		expected   string
	}{
		{
			name:       "scheduled backup",
			backupType: backup.TypeScheduled,
			expected:   "anilist-arr-sync_backup_scheduled_2026.10.09_23.15.30.zip",
		},
		{
			name:       "manual backup",
			backupType: backup.TypeManual,
			expected:   "anilist-arr-sync_backup_manual_2026.10.09_23.15.30.zip",
		},
		{
			name:       "update backup",
			backupType: backup.TypeUpdate,
			expected:   "anilist-arr-sync_backup_update_2026.10.09_23.15.30.zip",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := backup.FormatBackupFilename(tc.backupType, ts)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestParseBackupFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		filename    string
		wantType    backup.Type
		wantTime    time.Time
		expectError bool
	}{
		{
			name:        "valid scheduled filename",
			filename:    "anilist-arr-sync_backup_scheduled_2026.10.09_23.15.30.zip",
			wantType:    backup.TypeScheduled,
			wantTime:    time.Date(2026, 10, 9, 23, 15, 30, 0, time.UTC),
			expectError: false,
		},
		{
			name:        "valid manual filename",
			filename:    "anilist-arr-sync_backup_manual_2026.01.02_03.04.05.zip",
			wantType:    backup.TypeManual,
			wantTime:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			expectError: false,
		},
		{
			name:        "valid path with directory prefix",
			filename:    "/path/to/backups/anilist-arr-sync_backup_update_2026.05.20_12.00.00.zip",
			wantType:    backup.TypeUpdate,
			wantTime:    time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
			expectError: false,
		},
		{
			name:        "invalid prefix",
			filename:    "other_app_backup_scheduled_2026.10.09_23.15.30.zip",
			expectError: true,
		},
		{
			name:        "invalid extension",
			filename:    "anilist-arr-sync_backup_scheduled_2026.10.09_23.15.30.tar.gz",
			expectError: true,
		},
		{
			name:        "malformed timestamp",
			filename:    "anilist-arr-sync_backup_scheduled_2026-10-09-23-15-30.zip",
			expectError: true,
		},
		{
			name:        "empty filename",
			filename:    "",
			expectError: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			info, err := backup.ParseBackupFilename(tc.filename)
			if tc.expectError {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tc.filename)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.filename, err)
			}
			if info.Type != tc.wantType {
				t.Errorf("expected type %q, got %q", tc.wantType, info.Type)
			}
			if !info.CreatedAt.Equal(tc.wantTime) {
				t.Errorf("expected time %v, got %v", tc.wantTime, info.CreatedAt)
			}
		})
	}
}

func TestSelectBackupsToPrune(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var backups []backup.Info
	for i := 0; i < 10; i++ {
		ts := baseTime.Add(time.Duration(i) * 24 * time.Hour)
		name := backup.FormatBackupFilename(backup.TypeScheduled, ts)
		backups = append(backups, backup.Info{
			Filename:  name,
			Path:      "/backups/" + name,
			Type:      backup.TypeScheduled,
			CreatedAt: ts,
		})
	}

	// Retention of 7 should prune the 3 oldest backups
	toPrune := backup.SelectBackupsToPrune(backups, 7)
	if len(toPrune) != 3 {
		t.Fatalf("expected 3 backups to prune, got %d", len(toPrune))
	}

	// Verify the pruned ones are days 0, 1, 2 (the oldest)
	for i, p := range toPrune {
		expectedTime := baseTime.Add(time.Duration(i) * 24 * time.Hour)
		if !p.CreatedAt.Equal(expectedTime) {
			t.Errorf("expected pruned backup %d to be %v, got %v", i, expectedTime, p.CreatedAt)
		}
	}

	// Retention of 10 should prune nothing
	if len(backup.SelectBackupsToPrune(backups, 10)) != 0 {
		t.Error("expected 0 backups to prune with retention 10")
	}

	// Retention of 12 should prune nothing
	if len(backup.SelectBackupsToPrune(backups, 12)) != 0 {
		t.Error("expected 0 backups to prune with retention 12")
	}
}

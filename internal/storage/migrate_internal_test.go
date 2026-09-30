package storage

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadMigrationsFS_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		fs          fstest.MapFS
		wantErr     bool
		errContains string
		wantCount   int
	}{
		{
			name: "valid sequence",
			fs: fstest.MapFS{
				"migrations/0001_first.sql":  &fstest.MapFile{Data: []byte("SELECT 1;")},
				"migrations/0002_second.sql": &fstest.MapFile{Data: []byte("SELECT 2;")},
			},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name: "duplicate version numbers",
			fs: fstest.MapFS{
				"migrations/0002_add_roles.sql":  &fstest.MapFile{Data: []byte("SELECT 1;")},
				"migrations/0002_add_tokens.sql": &fstest.MapFile{Data: []byte("SELECT 2;")},
			},
			wantErr:     true,
			errContains: "duplicate migration version 2",
		},
		{
			name: "invalid filename format missing underscore",
			fs: fstest.MapFS{
				"migrations/0001first.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
			},
			wantErr:     true,
			errContains: "invalid migration filename",
		},
		{
			name: "invalid version prefix not an integer",
			fs: fstest.MapFS{
				"migrations/abcd_first.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
			},
			wantErr:     true,
			errContains: "invalid migration version prefix",
		},
		{
			name: "ignores non-sql files and directories",
			fs: fstest.MapFS{
				"migrations/0001_first.sql":   &fstest.MapFile{Data: []byte("SELECT 1;")},
				"migrations/notes.txt":        &fstest.MapFile{Data: []byte("notes")},
				"migrations/sub/0002_sub.sql": &fstest.MapFile{Data: []byte("nested")},
			},
			wantErr:   false,
			wantCount: 1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			list, err := loadMigrations(tc.fs)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got %q", tc.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(list) != tc.wantCount {
				t.Errorf("expected %d migrations, got %d", tc.wantCount, len(list))
			}
		})
	}
}

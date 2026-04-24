package versioncontrolops

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestExtractAddressConflictName(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "nil error",
			err:  nil,
			want: "",
		},
		{
			name: "unrelated error",
			err:  fmt.Errorf("connection refused"),
			want: "",
		},
		{
			name: "standard conflict",
			err:  fmt.Errorf("Error 1105: address conflict with a remote: 'default' -> file:///backup"),
			want: "default",
		},
		{
			name: "full dolt error format from doc comment",
			err:  fmt.Errorf("Error 1105: address conflict with a remote: 'backup_export' -> file:///some/path"),
			want: "backup_export",
		},
		{
			name: "missing closing quote",
			err:  fmt.Errorf("address conflict with a remote: 'oops"),
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractAddressConflictName(tt.err); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBackupRestoreURL(t *testing.T) {
	t.Run("remote URL passes through", func(t *testing.T) {
		const source = "https://doltremoteapi.dolthub.com/user/repo"
		got, err := BackupRestoreURL(source)
		if err != nil {
			t.Fatalf("BackupRestoreURL(%q) error = %v", source, err)
		}
		if got != source {
			t.Fatalf("BackupRestoreURL(%q) = %q, want passthrough", source, got)
		}
	})

	t.Run("file URL passes through without client stat", func(t *testing.T) {
		const source = "file:///server/visible/backup"
		got, err := BackupRestoreURL(source)
		if err != nil {
			t.Fatalf("BackupRestoreURL(%q) error = %v", source, err)
		}
		if got != source {
			t.Fatalf("BackupRestoreURL(%q) = %q, want passthrough", source, got)
		}
	})

	t.Run("local directory becomes file URL", func(t *testing.T) {
		dir := t.TempDir()
		got, err := BackupRestoreURL(dir)
		if err != nil {
			t.Fatalf("BackupRestoreURL(%q) error = %v", dir, err)
		}
		if !strings.HasPrefix(got, "file://") {
			t.Fatalf("BackupRestoreURL(%q) = %q, want file URL", dir, got)
		}
	})

	t.Run("missing local path fails", func(t *testing.T) {
		_, err := BackupRestoreURL("/path/that/does/not/exist")
		if err == nil {
			t.Fatal("expected missing local path to fail")
		}
	})

	t.Run("local file fails", func(t *testing.T) {
		file, err := os.CreateTemp(t.TempDir(), "backup-file-*")
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		_, err = BackupRestoreURL(file.Name())
		if err == nil {
			t.Fatal("expected local file to fail")
		}
	})
}

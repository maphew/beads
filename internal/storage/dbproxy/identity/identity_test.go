package identity

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootID_StableAcrossSymlink(t *testing.T) {
	target := t.TempDir()
	first, err := RootID(target)
	require.NoError(t, err)
	second, err := RootID(target)
	require.NoError(t, err)
	assert.Equal(t, first, second)

	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	link := filepath.Join(t.TempDir(), "workspace-link")
	require.NoError(t, os.Symlink(target, link))
	linked, err := RootID(link)
	require.NoError(t, err)
	assert.Equal(t, first, linked)
}

func TestSecret_WriteReadAndRotate(t *testing.T) {
	root := t.TempDir()
	first, err := WriteSecret(root)
	require.NoError(t, err)
	got, err := ReadSecret(root)
	require.NoError(t, err)
	assert.Equal(t, first, got)

	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(root, SecretFileName))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	second, err := WriteSecret(root)
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
	got, err = ReadSecret(root)
	require.NoError(t, err)
	assert.Equal(t, second, got)
}

func TestReadSecret_RejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{name: "short", value: "0123"},
		{name: "non hex", value: "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, SecretFileName), []byte(tc.value), 0o600))
			_, err := ReadSecret(root)
			require.Error(t, err)
		})
	}
}

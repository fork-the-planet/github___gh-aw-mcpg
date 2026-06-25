package logger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAtomicWriteFile exercises the three reachable code paths in atomicWriteFile:
//
//  1. Success – the temp file is written and renamed into place.
//  2. WriteFile fails – the parent directory does not exist.
//  3. Rename fails, cleanup succeeds – the target path is an existing directory,
//     which causes os.Rename to return EISDIR on Linux (the temp file is cleaned up
//     by os.Remove, and the rename error is returned).
//
// The fourth path – rename fails AND cleanup fails – requires the temp file to become
// a non-removable, non-NotExist entry after os.WriteFile succeeds, which cannot be
// triggered deterministically in a portable unit test.
func TestAtomicWriteFile(t *testing.T) {
	data := []byte(`{"key":"value"}`)

	t.Run("success", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "output.json")

		err := atomicWriteFile(filePath, data, 0600)
		require.NoError(t, err, "atomicWriteFile should succeed")

		got, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, data, got, "written content should match input")

		_, statErr := os.Stat(filePath + ".tmp")
		assert.True(t, os.IsNotExist(statErr), "temp file should be cleaned up after rename")
	})

	t.Run("write file fails – nonexistent directory", func(t *testing.T) {
		filePath := filepath.Join("/nonexistent/directory/that/does/not/exist", "output.json")

		err := atomicWriteFile(filePath, data, 0600)
		require.Error(t, err, "should fail when parent directory does not exist")
		assert.Contains(t, err.Error(), "failed to write temp file")
	})

	t.Run("rename fails cleanup succeeds – target is a directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "output.json")

		// Occupy the target path with a directory so os.Rename returns EISDIR.
		require.NoError(t, os.MkdirAll(filePath, 0755))

		err := atomicWriteFile(filePath, data, 0600)
		require.Error(t, err, "should fail when rename target is a directory")
		assert.Contains(t, err.Error(), "failed to rename temp file")

		// The temp file should be cleaned up even though rename failed.
		_, statErr := os.Stat(filePath + ".tmp")
		assert.True(t, os.IsNotExist(statErr), "temp file should be removed after failed rename")
	})

	t.Run("idempotent – second write overwrites first", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "output.json")

		first := []byte(`{"version":1}`)
		second := []byte(`{"version":2}`)

		require.NoError(t, atomicWriteFile(filePath, first, 0600))
		require.NoError(t, atomicWriteFile(filePath, second, 0600))

		got, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, second, got, "second write should overwrite first")
	})

	t.Run("empty data", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "empty.json")

		err := atomicWriteFile(filePath, []byte{}, 0600)
		require.NoError(t, err, "should succeed with empty data")

		got, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Empty(t, got, "file should be empty")
	})
}

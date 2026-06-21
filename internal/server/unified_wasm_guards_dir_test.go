package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindServerWASMGuardFile(t *testing.T) {
	t.Run("returns not found when env var is unset", func(t *testing.T) {
		t.Setenv(wasmGuardsDirEnvVar, "")

		path, found, err := guard.FindServerWASMGuardFile("github")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns not found when server subdirectory does not exist", func(t *testing.T) {
		rootDir := t.TempDir()
		t.Setenv(wasmGuardsDirEnvVar, rootDir)

		path, found, err := guard.FindServerWASMGuardFile("github")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns first wasm file found in server subdirectory", func(t *testing.T) {
		rootDir := t.TempDir()
		t.Setenv(wasmGuardsDirEnvVar, rootDir)

		serverDir := filepath.Join(rootDir, "github")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))

		require.NoError(t, os.WriteFile(filepath.Join(serverDir, "z-ignore.txt"), []byte("x"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(serverDir, "a-first.wasm"), []byte("not-a-valid-wasm"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(serverDir, "b-second.wasm"), []byte("not-a-valid-wasm"), 0o644))

		path, found, err := guard.FindServerWASMGuardFile("github")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, filepath.Join(serverDir, "a-first.wasm"), path)
	})

	t.Run("returns error when server subdirectory path is not readable as a directory", func(t *testing.T) {
		rootDir := t.TempDir()
		t.Setenv(wasmGuardsDirEnvVar, rootDir)

		serverPath := filepath.Join(rootDir, "github")
		require.NoError(t, os.WriteFile(serverPath, []byte("not-a-directory"), 0o644))

		path, found, err := guard.FindServerWASMGuardFile("github")
		require.Error(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})
}

package guard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetWASMGuardsRootDir(t *testing.T) {
	tests := []struct {
		name   string
		envVal string
		want   string
	}{
		{
			name:   "empty env var returns empty string",
			envVal: "",
			want:   "",
		},
		{
			name:   "plain path returned as-is",
			envVal: "/tmp/guards",
			want:   "/tmp/guards",
		},
		{
			name:   "leading and trailing whitespace is trimmed",
			envVal: "  /tmp/guards  ",
			want:   "/tmp/guards",
		},
		{
			name:   "only whitespace returns empty string",
			envVal: "   ",
			want:   "",
		},
		{
			name:   "tab-only whitespace returns empty string",
			envVal: "\t",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(wasmGuardsDirEnvVar, tt.envVal)
			assert.Equal(t, tt.want, GetWASMGuardsRootDir())
		})
	}
}

func TestFindServerWASMGuardFile(t *testing.T) {
	t.Run("returns empty when env var is not set", func(t *testing.T) {
		old, ok := os.LookupEnv(wasmGuardsDirEnvVar)
		require.NoError(t, os.Unsetenv(wasmGuardsDirEnvVar))
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(wasmGuardsDirEnvVar, old)
			}
		})
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns empty when env var contains only whitespace", func(t *testing.T) {
		t.Setenv(wasmGuardsDirEnvVar, "   ")
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns empty when server directory does not exist", func(t *testing.T) {
		rootDir := t.TempDir()
		t.Setenv(wasmGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile("no-such-server")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns empty when server directory exists but is empty", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "myserver")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		t.Setenv(wasmGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns empty when server directory contains only non-wasm files", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "myserver")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(serverDir, "readme.txt"), []byte("readme"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(serverDir, "data.json"), []byte("{}"), 0o644))
		t.Setenv(wasmGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns wasm file path when a .wasm file exists", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "myserver")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		wasmPath := filepath.Join(serverDir, "policy.wasm")
		require.NoError(t, os.WriteFile(wasmPath, []byte{0x00, 0x61, 0x73, 0x6d}, 0o644))
		t.Setenv(wasmGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, wasmPath, path)
	})

	t.Run("matches .wasm file with uppercase extension", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "myserver")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		wasmPath := filepath.Join(serverDir, "policy.WASM")
		require.NoError(t, os.WriteFile(wasmPath, []byte{0x00, 0x61, 0x73, 0x6d}, 0o644))
		t.Setenv(wasmGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, wasmPath, path)
	})

	t.Run("skips subdirectory named with .wasm extension", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "myserver")
		// A directory named "guard.wasm" should not be returned as a match.
		require.NoError(t, os.MkdirAll(filepath.Join(serverDir, "guard.wasm"), 0o755))
		t.Setenv(wasmGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns first .wasm file alphabetically when multiple exist", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "myserver")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		// ReadDir returns entries in alphabetical order, so aaa.wasm comes first.
		firstWasm := filepath.Join(serverDir, "aaa.wasm")
		require.NoError(t, os.WriteFile(firstWasm, []byte{0x00, 0x61, 0x73, 0x6d}, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(serverDir, "bbb.wasm"), []byte{0x00, 0x61, 0x73, 0x6d}, 0o644))
		t.Setenv(wasmGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, firstWasm, path)
	})

	t.Run("skips non-wasm files and subdirectories before finding .wasm file", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "myserver")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(serverDir, "aaa-subdir"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(serverDir, "bbb-readme.txt"), []byte("readme"), 0o644))
		wasmPath := filepath.Join(serverDir, "ccc-policy.wasm")
		require.NoError(t, os.WriteFile(wasmPath, []byte{0x00, 0x61, 0x73, 0x6d}, 0o644))
		t.Setenv(wasmGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, wasmPath, path)
	})

	t.Run("returns error when server path exists as a file instead of directory", func(t *testing.T) {
		rootDir := t.TempDir()
		// Create a regular file where a server directory is expected.
		serverFile := filepath.Join(rootDir, "myserver")
		require.NoError(t, os.WriteFile(serverFile, []byte("not a directory"), 0o644))
		t.Setenv(wasmGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile("myserver")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "myserver")
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("trims whitespace from env var when resolving root directory", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "myserver")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		wasmPath := filepath.Join(serverDir, "policy.wasm")
		require.NoError(t, os.WriteFile(wasmPath, []byte{0x00, 0x61, 0x73, 0x6d}, 0o644))
		t.Setenv(wasmGuardsDirEnvVar, "  "+rootDir+"  ")
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, wasmPath, path)
	})

	t.Run("handles server ID containing only valid characters", func(t *testing.T) {
		rootDir := t.TempDir()
		serverID := "github-mcp-server-v2"
		serverDir := filepath.Join(rootDir, serverID)
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		wasmPath := filepath.Join(serverDir, "guard.wasm")
		require.NoError(t, os.WriteFile(wasmPath, []byte{0x00, 0x61, 0x73, 0x6d}, 0o644))
		t.Setenv(wasmGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile(serverID)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, wasmPath, path)
	})
}

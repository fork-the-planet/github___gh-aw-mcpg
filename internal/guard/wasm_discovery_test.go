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
			t.Setenv(WASMGuardsDirEnvVar, tt.envVal)
			assert.Equal(t, tt.want, GetWASMGuardsRootDir())
		})
	}
}

func TestFindServerWASMGuardFile(t *testing.T) {
	t.Run("returns empty when env var is not set", func(t *testing.T) {
		old, ok := os.LookupEnv(WASMGuardsDirEnvVar)
		require.NoError(t, os.Unsetenv(WASMGuardsDirEnvVar))
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(WASMGuardsDirEnvVar, old)
			}
		})
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns empty when env var contains only whitespace", func(t *testing.T) {
		t.Setenv(WASMGuardsDirEnvVar, "   ")
		path, found, err := FindServerWASMGuardFile("myserver")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns empty when server directory does not exist", func(t *testing.T) {
		rootDir := t.TempDir()
		t.Setenv(WASMGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile("no-such-server")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns empty when server directory exists but is empty", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "myserver")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		t.Setenv(WASMGuardsDirEnvVar, rootDir)
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
		t.Setenv(WASMGuardsDirEnvVar, rootDir)
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
		t.Setenv(WASMGuardsDirEnvVar, rootDir)
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
		t.Setenv(WASMGuardsDirEnvVar, rootDir)
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
		t.Setenv(WASMGuardsDirEnvVar, rootDir)
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
		t.Setenv(WASMGuardsDirEnvVar, rootDir)
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
		t.Setenv(WASMGuardsDirEnvVar, rootDir)
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
		t.Setenv(WASMGuardsDirEnvVar, rootDir)
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
		t.Setenv(WASMGuardsDirEnvVar, "  "+rootDir+"  ")
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
		t.Setenv(WASMGuardsDirEnvVar, rootDir)
		path, found, err := FindServerWASMGuardFile(serverID)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, wasmPath, path)
	})
}

func TestFindGuardFile(t *testing.T) {
	t.Run("non-github server skips container path and uses env var", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "myserver")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		wasmPath := filepath.Join(serverDir, "policy.wasm")
		require.NoError(t, os.WriteFile(wasmPath, []byte{0x00, 0x61, 0x73, 0x6d}, 0o644))
		t.Setenv(WASMGuardsDirEnvVar, rootDir)

		path, found, err := FindGuardFile("myserver")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, wasmPath, path)
	})

	t.Run("github server falls back to env var when container path absent", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "github")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		wasmPath := filepath.Join(serverDir, "00-github-guard.wasm")
		require.NoError(t, os.WriteFile(wasmPath, []byte{0x00, 0x61, 0x73, 0x6d}, 0o644))
		t.Setenv(WASMGuardsDirEnvVar, rootDir)

		statFn := func(_ string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		}

		path, found, err := findGuardFile("github", statFn)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, wasmPath, path)
	})

	t.Run("github server prefers baked-in container path over env var fallback", func(t *testing.T) {
		rootDir := t.TempDir()
		serverDir := filepath.Join(rootDir, "github")
		require.NoError(t, os.MkdirAll(serverDir, 0o755))
		envWasmPath := filepath.Join(serverDir, "00-github-guard.wasm")
		require.NoError(t, os.WriteFile(envWasmPath, []byte{0x00, 0x61, 0x73, 0x6d}, 0o644))
		t.Setenv(WASMGuardsDirEnvVar, rootDir)

		statCalls := 0
		statFn := func(path string) (os.FileInfo, error) {
			statCalls++
			require.Equal(t, ContainerGuardWasmPath, path)
			return nil, nil
		}

		path, found, err := findGuardFile("github", statFn)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, ContainerGuardWasmPath, path)
		assert.Equal(t, 1, statCalls)
	})

	t.Run("github server skips baked-in path when env var explicitly set blank", func(t *testing.T) {
		t.Setenv(WASMGuardsDirEnvVar, "")

		statCalls := 0
		statFn := func(_ string) (os.FileInfo, error) {
			statCalls++
			return nil, os.ErrNotExist
		}

		path, found, err := findGuardFile("github", statFn)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
		assert.Zero(t, statCalls, "baked-in path should not be probed when env var is explicitly blank")
	})

	t.Run("returns empty when nothing found for non-github server", func(t *testing.T) {
		t.Setenv(WASMGuardsDirEnvVar, "")

		path, found, err := FindGuardFile("myserver")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})

	t.Run("returns empty when nothing found for github server without container path or env var", func(t *testing.T) {
		t.Setenv(WASMGuardsDirEnvVar, "")

		statFn := func(_ string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		}

		path, found, err := findGuardFile("github", statFn)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, path)
	})
}

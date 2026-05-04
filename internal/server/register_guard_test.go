package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMinimalUnifiedServerForGuardTest returns a UnifiedServer with the minimum fields
// initialised for registerGuard tests (cfg + guardRegistry only).
func newMinimalUnifiedServerForGuardTest(cfg *config.Config) *UnifiedServer {
	return &UnifiedServer{
		cfg:           cfg,
		guardRegistry: guard.NewRegistry(),
	}
}

// ─── registerGuard: no guard configured ──────────────────────────────────────

// TestRegisterGuard_NoServerInConfig registers a noop guard when the serverID is
// absent from cfg.Servers (the outer else branch at line 664).
func TestRegisterGuard_NoServerInConfig(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"other-server": {Type: "http", URL: "https://example.com/mcp"},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name())
}

// TestRegisterGuard_NilServersMap registers a noop guard when cfg.Servers is nil.
func TestRegisterGuard_NilServersMap(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{Servers: nil}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name())
}

// TestRegisterGuard_EmptyGuardField registers a noop guard when the server exists
// in config but its Guard field is empty (line 664 – no guard name set).
func TestRegisterGuard_EmptyGuardField(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", URL: "https://example.com/mcp", Guard: ""},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name())
}

// ─── registerGuard: guard config found, type = noop / empty ──────────────────

// TestRegisterGuard_GuardConfigType_Noop exercises the "noop"/"" case inside
// createGuardFromConfig (line 770-771), called via the Guard config path.
func TestRegisterGuard_GuardConfigType_Noop(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: "my-guard"},
		},
		Guards: map[string]*config.GuardConfig{
			"my-guard": {Type: "noop"},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name())
}

// TestRegisterGuard_GuardConfigType_Empty exercises the empty-string type case
// inside createGuardFromConfig, which also creates a NoopGuard.
func TestRegisterGuard_GuardConfigType_Empty(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: "my-guard"},
		},
		Guards: map[string]*config.GuardConfig{
			"my-guard": {Type: ""},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name())
}

// ─── registerGuard: wasm guard config path (requires empty path → error) ────

// TestRegisterGuard_GuardConfigType_WasmEmptyPath covers the case where the guard
// config specifies type="wasm" but has no path set.  createGuardFromConfig returns
// an error and registerGuard falls back to a noop guard (line 652-653).
func TestRegisterGuard_GuardConfigType_WasmEmptyPath(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: "wasm-guard"},
		},
		Guards: map[string]*config.GuardConfig{
			"wasm-guard": {Type: "wasm", Path: ""},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	// Should not error – falls back to noop on wasm path failure
	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name())
}

// ─── registerGuard: guard name set but no Guard config ───────────────────────

// TestRegisterGuard_GuardNameSet_NoGuardConfig_UnknownType exercises the path at
// line 658 where the Guard name is not in cfg.Guards.  CreateGuard("unknown") fails
// and the guard falls back to noop (line 660-661).
func TestRegisterGuard_GuardNameSet_NoGuardConfig_UnknownType(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: "unregistered-guard"},
		},
		Guards: map[string]*config.GuardConfig{}, // empty – guard name not present
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name())
}

// TestRegisterGuard_GuardNameSet_NoGuardConfig_RegisteredType exercises the path
// where the Guard name is not in cfg.Guards but IS registered via RegisterGuardType.
// The guard returned by the factory should be registered.
func TestRegisterGuard_GuardNameSet_NoGuardConfig_RegisteredType(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	const guardTypeName = "test-registered-guard-type"

	// Register a temporary custom guard factory
	guard.RegisterGuardType(guardTypeName, func() (guard.Guard, error) {
		return guard.NewNoopGuard(), nil // factory returns a noop-like guard
	})

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: guardTypeName},
		},
		// Guards map absent – factory path will be used
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.NoError(t, err)
	// The guard registered via factory was used; noop guard was returned by factory
	registeredGuard := us.guardRegistry.Get("github")
	require.NotNil(t, registeredGuard)
}

// ─── registerGuard: write-sink policy path ───────────────────────────────────

// TestRegisterGuard_WriteSinkPolicy_CreatesWriteSinkGuard verifies that when the
// server's resolved guard policy is a write-sink policy (lines 633-636), a
// WriteSinkGuard is registered instead of a noop guard.
func TestRegisterGuard_WriteSinkPolicy_CreatesWriteSinkGuard(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"safe-outputs": {Type: "http", URL: "https://safe.example.com/mcp"},
		},
		// Global write-sink policy: resolveWriteSinkPolicy will return this
		GuardPolicy: &config.GuardPolicy{
			WriteSink: &config.WriteSinkPolicy{
				Accept: []string{"*"},
			},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("safe-outputs")

	require.NoError(t, err)
	registeredGuard := us.guardRegistry.Get("safe-outputs")
	require.NotNil(t, registeredGuard)
	assert.Equal(t, "write-sink", registeredGuard.Name(),
		"write-sink policy should create a WriteSinkGuard")
}

// TestRegisterGuard_WriteSinkPolicy_MultipleAcceptPatterns ensures write-sink guard
// creation works correctly with multiple accept patterns.
func TestRegisterGuard_WriteSinkPolicy_MultipleAcceptPatterns(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"safe-outputs": {Type: "http"},
		},
		GuardPolicy: &config.GuardPolicy{
			WriteSink: &config.WriteSinkPolicy{
				Accept: []string{"private:myorg/repo1", "private:myorg/repo2"},
			},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("safe-outputs")

	require.NoError(t, err)
	registeredGuard := us.guardRegistry.Get("safe-outputs")
	require.NotNil(t, registeredGuard)
	assert.Equal(t, "write-sink", registeredGuard.Name())
}

// ─── registerGuard: policy validation error ──────────────────────────────────

// TestRegisterGuard_InvalidPolicy_ReturnsError verifies that when resolveGuardPolicy
// returns an error (invalid AllowOnly policy), registerGuard propagates the error.
// NOTE: The guard must be non-noop because requireGuardPolicyIfGuardEnabled
// short-circuits for noop guards without validating the policy (by design).
func TestRegisterGuard_InvalidPolicy_ReturnsError(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	// Register a non-noop guard type so the policy validation path is reached.
	const guardType = "test-non-noop-for-invalid-policy"
	guard.RegisterGuardType(guardType, func() (guard.Guard, error) {
		return guard.NewWriteSinkGuard([]string{"*"}), nil
	})

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: "my-guard"},
		},
		Guards: map[string]*config.GuardConfig{
			"my-guard": {Type: guardType},
		},
		// Invalid global policy: min-integrity value is not recognised
		GuardPolicy: &config.GuardPolicy{
			AllowOnly: &config.AllowOnlyPolicy{
				MinIntegrity: "not-a-real-level",
				Repos:        "public",
			},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.Error(t, err, "invalid policy should propagate as an error")
}

// ─── registerGuard: WASM guards directory integration ────────────────────────

// TestRegisterGuard_WasmDirNotSet_UsesConfigGuard verifies that when the
// MCP_GATEWAY_WASM_GUARDS_DIR env var is empty (the default), no WASM discovery
// occurs and the function falls through to the guard config path.
func TestRegisterGuard_WasmDirNotSet_UsesConfigGuard(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: ""},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name(),
		"no WASM dir and no guard config should yield a noop guard")
}

// TestRegisterGuard_WasmDirSet_ServerSubdirMissing verifies that when the WASM
// guards root directory exists but has no subdirectory for the given serverID, the
// function falls through to the guard config path without error (line 738-739).
func TestRegisterGuard_WasmDirSet_ServerSubdirMissing(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv(wasmGuardsDirEnvVar, rootDir)

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: ""},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name(),
		"missing server subdirectory should fall through to noop guard")
}

// TestRegisterGuard_WasmDirSet_InvalidWasmFile_FallsBackToConfigGuard verifies that
// when a WASM file is found (lines 620-628) but guard.NewWasmGuard fails to load it
// (the file is not a valid WASM binary), g stays nil and the function continues to
// check the guard config, eventually ending with a noop.
func TestRegisterGuard_WasmDirSet_InvalidWasmFile_FallsBackToConfigGuard(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv(wasmGuardsDirEnvVar, rootDir)

	// Create a WASM file with invalid content so NewWasmGuard fails
	serverDir := filepath.Join(rootDir, "github")
	require.NoError(t, os.MkdirAll(serverDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(serverDir, "invalid.wasm"),
		[]byte("this is not a valid WASM binary"),
		0o644,
	))

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: ""},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	// WASM load failure is a warning, not an error – falls back gracefully
	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name(),
		"invalid WASM file should fall back to noop guard")
}

// TestRegisterGuard_WasmDirSet_ReadDirError_WarnsAndContinues covers the error
// branch (line 619) inside findServerWASMGuardFile where the server path exists
// as a regular file rather than a directory, causing ReadDir to fail.  The error
// is logged as a WARNING and registerGuard continues with the guard config path.
func TestRegisterGuard_WasmDirSet_ReadDirError_WarnsAndContinues(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv(wasmGuardsDirEnvVar, rootDir)

	// Write a *file* at the server directory path to force ReadDir to fail
	serverPath := filepath.Join(rootDir, "github")
	require.NoError(t, os.WriteFile(serverPath, []byte("not-a-directory"), 0o644))

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: ""},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	// A read-dir error for the WASM guard is a WARNING, not a fatal error
	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name(),
		"read-dir error on WASM path should fall back to noop guard")
}

// ─── registerGuard: guard config found with Guard in cfg.Guards ──────────────

// TestRegisterGuard_GuardConfigFound_NoopType_GlobalAllowOnlyPolicy verifies the
// interaction between guard config discovery and global allow-only policy.
// A "noop" guard from config is returned by createGuardFromConfig, and since it is
// "noop", requireGuardPolicyIfGuardEnabled returns it without policy validation.
func TestRegisterGuard_GuardConfigFound_NoopType_GlobalAllowOnlyPolicy(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: "my-guard"},
		},
		Guards: map[string]*config.GuardConfig{
			"my-guard": {Type: "noop"},
		},
		GuardPolicy: &config.GuardPolicy{
			AllowOnly: &config.AllowOnlyPolicy{
				MinIntegrity: config.IntegrityNone,
				Repos:        "public",
			},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.NoError(t, err)
	// noop guard is returned as-is by requireGuardPolicyIfGuardEnabled
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name())
}

// TestRegisterGuard_GuardNameSet_NilGuardsMap covers line 646 where cfg.Guards is
// nil – hasGuardCfg will be false so the registered-type path (line 658) is taken.
// Since the guard name is not registered, it falls back to noop.
func TestRegisterGuard_GuardNameSet_NilGuardsMap(t *testing.T) {
	t.Setenv(wasmGuardsDirEnvVar, "")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http", Guard: "some-guard"},
		},
		Guards: nil,
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	err := us.registerGuard("github")

	require.NoError(t, err)
	assert.Equal(t, "noop", us.guardRegistry.Get("github").Name(),
		"nil Guards map with unregistered guard name should fall back to noop")
}

// ─── createGuardFromConfig direct tests ──────────────────────────────────────

// TestCreateGuardFromConfig_Noop verifies the "noop" type branch returns a noop guard.
func TestCreateGuardFromConfig_Noop(t *testing.T) {
	us := newMinimalUnifiedServerForGuardTest(&config.Config{})

	g, err := us.createGuardFromConfig("my-guard", &config.GuardConfig{Type: "noop"})

	require.NoError(t, err)
	require.NotNil(t, g)
	assert.Equal(t, "noop", g.Name())
}

// TestCreateGuardFromConfig_EmptyType verifies the empty-type branch also returns
// a noop guard (the "noop" and "" cases share a single switch arm).
func TestCreateGuardFromConfig_EmptyType(t *testing.T) {
	us := newMinimalUnifiedServerForGuardTest(&config.Config{})

	g, err := us.createGuardFromConfig("my-guard", &config.GuardConfig{Type: ""})

	require.NoError(t, err)
	require.NotNil(t, g)
	assert.Equal(t, "noop", g.Name())
}

// TestCreateGuardFromConfig_WasmType_EmptyPath verifies the wasm branch returns an
// error when cfg.Path is empty (line 775-777 in unified.go).
func TestCreateGuardFromConfig_WasmType_EmptyPath(t *testing.T) {
	us := newMinimalUnifiedServerForGuardTest(&config.Config{})

	g, err := us.createGuardFromConfig("wasm-guard", &config.GuardConfig{Type: "wasm", Path: ""})

	require.Error(t, err)
	assert.Nil(t, g)
	assert.ErrorContains(t, err, "requires a 'path' field")
}

// TestCreateGuardFromConfig_WasmType_InvalidPath verifies the wasm branch returns an
// error when cfg.Path points to an invalid / non-existent WASM binary (line 782-784).
func TestCreateGuardFromConfig_WasmType_InvalidPath(t *testing.T) {
	us := newMinimalUnifiedServerForGuardTest(&config.Config{})

	// Use a temp file with garbage content so NewWasmGuard fails
	tmpFile, err := os.CreateTemp("", "invalid-*.wasm")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.WriteString("this is not valid WASM")
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	g, err := us.createGuardFromConfig("wasm-guard", &config.GuardConfig{
		Type: "wasm",
		Path: tmpFile.Name(),
	})

	require.Error(t, err)
	assert.Nil(t, g)
	assert.ErrorContains(t, err, "failed to load WASM guard")
}

// TestCreateGuardFromConfig_Default_UnknownType verifies the default branch for an
// unregistered guard type returns an error from guard.CreateGuard (line 790).
func TestCreateGuardFromConfig_Default_UnknownType(t *testing.T) {
	us := newMinimalUnifiedServerForGuardTest(&config.Config{})

	g, err := us.createGuardFromConfig("my-guard", &config.GuardConfig{Type: "does-not-exist"})

	require.Error(t, err)
	assert.Nil(t, g)
	assert.ErrorContains(t, err, "unknown guard type")
}

// TestCreateGuardFromConfig_Default_RegisteredType verifies the default branch
// succeeds when the guard type name has been registered via RegisterGuardType.
func TestCreateGuardFromConfig_Default_RegisteredType(t *testing.T) {
	const customType = "test-custom-type-direct"

	guard.RegisterGuardType(customType, func() (guard.Guard, error) {
		return guard.NewNoopGuard(), nil
	})

	us := newMinimalUnifiedServerForGuardTest(&config.Config{})

	g, err := us.createGuardFromConfig("my-guard", &config.GuardConfig{Type: customType})

	require.NoError(t, err)
	require.NotNil(t, g)
}

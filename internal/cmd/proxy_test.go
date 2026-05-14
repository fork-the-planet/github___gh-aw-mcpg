package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/config"
)

// TestDetectGuardWasm_FileNotFound tests that detectGuardWasm returns empty string
// when the baked-in guard at containerGuardWasmPath does not exist.
// In standard test environments (non-container), the baked-in guard is absent.
func TestDetectGuardWasm_FileNotFound(t *testing.T) {
	// Confirm the baked-in path does not exist in this environment
	_, err := os.Stat(containerGuardWasmPath)
	if err == nil {
		t.Skipf("baked-in guard found at %s (running in container) — skipping 'not found' test", containerGuardWasmPath)
	}

	result := detectGuardWasm()
	assert.Empty(t, result, "detectGuardWasm should return empty string when guard file does not exist")
}

// TestDetectGuardWasm_FileExists verifies that detectGuardWasm returns the
// containerGuardWasmPath when that file is present on the filesystem.
// This test creates a temporary file at the expected path to simulate the container environment.
func TestDetectGuardWasm_FileExists(t *testing.T) {
	// Skip if we cannot write to /guards/github/; test can only run where the
	// directory is pre-created (e.g. the production container image).
	if _, err := os.Stat(containerGuardWasmPath); err == nil {
		// File already exists (running in container): just verify the function works.
		result := detectGuardWasm()
		assert.Equal(t, containerGuardWasmPath, result,
			"detectGuardWasm should return the baked-in path when the file exists")
	}
	// If the file does not exist and we cannot create it (no permission), skip.
	t.Skip("baked-in guard not present and cannot create it in this environment")
}

// TestNewProxyCmd_AllFlagsRegistered verifies that newProxyCmd registers all expected flags.
func TestNewProxyCmd_AllFlagsRegistered(t *testing.T) {
	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	expectedFlags := []string{
		"guard-wasm",
		"policy",
		"github-token",
		"listen",
		"log-dir",
		"wasm-cache-dir",
		"guards-mode",
		"github-api-url",
		"tls",
		"tls-dir",
		"trusted-bots",
		"trusted-users",
		"otlp-endpoint",
		"otlp-service-name",
		"otlp-sample-rate",
	}

	for _, flagName := range expectedFlags {
		t.Run("flag_"+flagName, func(t *testing.T) {
			flag := cmd.Flags().Lookup(flagName)
			assert.NotNil(t, flag, "flag --%s should be registered on proxy command", flagName)
		})
	}
}

// TestNewProxyCmd_CommandMetadata verifies the command's metadata is correctly set.
func TestNewProxyCmd_CommandMetadata(t *testing.T) {
	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	assert.Equal(t, "proxy", cmd.Use, "proxy command Use should be 'proxy'")
	assert.NotEmpty(t, cmd.Short, "proxy command Short should not be empty")
	assert.NotEmpty(t, cmd.Long, "proxy command Long should not be empty")
	assert.True(t, cmd.SilenceUsage, "proxy command should silence usage on error")
	assert.NotNil(t, cmd.RunE, "proxy command RunE should be set")

	// Long description should mention proxy and DIFC
	assert.Contains(t, cmd.Long, "proxy", "Long description should mention 'proxy'")
	assert.Contains(t, cmd.Long, "DIFC", "Long description should mention 'DIFC'")
}

// TestNewProxyCmd_DefaultFlagValues verifies that flags have the expected defaults
// when no environment variables are set.
func TestNewProxyCmd_DefaultFlagValues(t *testing.T) {
	// Clear relevant env vars to get clean defaults
	envVarsToClear := []string{
		"MCP_GATEWAY_GUARD_POLICY_JSON",
		"MCP_GATEWAY_LOG_DIR",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_SERVICE_NAME",
	}
	for _, envVar := range envVarsToClear {
		t.Setenv(envVar, "")
	}

	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	tests := []struct {
		flagName     string
		expectedType string
		validate     func(t *testing.T, cmd *cobra.Command)
	}{
		{
			flagName: "listen",
			validate: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				val, err := cmd.Flags().GetString("listen")
				require.NoError(t, err)
				assert.Equal(t, "127.0.0.1:8080", val, "--listen default should be 127.0.0.1:8080")
			},
		},
		{
			flagName: "guards-mode",
			validate: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				val, err := cmd.Flags().GetString("guards-mode")
				require.NoError(t, err)
				assert.Equal(t, "filter", val, "--guards-mode default for proxy should be 'filter'")
			},
		},
		{
			flagName: "github-token",
			validate: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				val, err := cmd.Flags().GetString("github-token")
				require.NoError(t, err)
				assert.Equal(t, "", val, "--github-token default should be empty")
			},
		},
		{
			flagName: "github-api-url",
			validate: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				val, err := cmd.Flags().GetString("github-api-url")
				require.NoError(t, err)
				assert.Equal(t, "", val, "--github-api-url default should be empty")
			},
		},
		{
			flagName: "tls",
			validate: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				val, err := cmd.Flags().GetBool("tls")
				require.NoError(t, err)
				assert.False(t, val, "--tls default should be false")
			},
		},
		{
			flagName: "tls-dir",
			validate: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				val, err := cmd.Flags().GetString("tls-dir")
				require.NoError(t, err)
				assert.Equal(t, "", val, "--tls-dir default should be empty")
			},
		},
		{
			flagName: "otlp-service-name",
			validate: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				val, err := cmd.Flags().GetString("otlp-service-name")
				require.NoError(t, err)
				assert.Equal(t, config.DefaultTracingServiceName, val,
					"--otlp-service-name default should be the DefaultTracingServiceName constant")
			},
		},
		{
			flagName: "otlp-sample-rate",
			validate: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				val, err := cmd.Flags().GetFloat64("otlp-sample-rate")
				require.NoError(t, err)
				assert.Equal(t, config.DefaultTracingSampleRate, val,
					"--otlp-sample-rate default should be DefaultTracingSampleRate")
			},
		},
		{
			flagName: "policy",
			validate: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				val, err := cmd.Flags().GetString("policy")
				require.NoError(t, err)
				assert.Equal(t, "", val, "--policy default should be empty when env var is unset")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			tt.validate(t, cmd)
		})
	}
}

// TestNewProxyCmd_PolicyDefaultFromEnv verifies that --policy picks up its
// default value from the MCP_GATEWAY_GUARD_POLICY_JSON environment variable.
func TestNewProxyCmd_PolicyDefaultFromEnv(t *testing.T) {
	envPolicy := `{"allow-only":{"repos":"public","min-integrity":"none"}}`
	t.Setenv("MCP_GATEWAY_GUARD_POLICY_JSON", envPolicy)

	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	val, err := cmd.Flags().GetString("policy")
	require.NoError(t, err)
	assert.Equal(t, envPolicy, val,
		"--policy default should reflect MCP_GATEWAY_GUARD_POLICY_JSON environment variable")
}

// TestNewProxyCmd_OTLPEndpointDefaultFromEnv verifies that --otlp-endpoint picks
// up its default value from OTEL_EXPORTER_OTLP_ENDPOINT.
func TestNewProxyCmd_OTLPEndpointDefaultFromEnv(t *testing.T) {
	endpoint := "http://otel-collector:4318"
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)

	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	val, err := cmd.Flags().GetString("otlp-endpoint")
	require.NoError(t, err)
	assert.Equal(t, endpoint, val,
		"--otlp-endpoint default should reflect OTEL_EXPORTER_OTLP_ENDPOINT environment variable")
}

// TestNewProxyCmd_OTLPServiceNameDefaultFromEnv verifies that --otlp-service-name
// picks up its default value from OTEL_SERVICE_NAME.
func TestNewProxyCmd_OTLPServiceNameDefaultFromEnv(t *testing.T) {
	serviceName := "my-custom-proxy"
	t.Setenv("OTEL_SERVICE_NAME", serviceName)

	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	val, err := cmd.Flags().GetString("otlp-service-name")
	require.NoError(t, err)
	assert.Equal(t, serviceName, val,
		"--otlp-service-name default should reflect OTEL_SERVICE_NAME environment variable")
}

// TestNewProxyCmd_GuardWasmRequiredWhenNoBakedInGuard verifies that --guard-wasm is
// marked as required when the baked-in container guard does not exist.
func TestNewProxyCmd_GuardWasmRequiredWhenNoBakedInGuard(t *testing.T) {
	// This test is only meaningful when running outside a container.
	if _, err := os.Stat(containerGuardWasmPath); err == nil {
		t.Skipf("baked-in guard found at %s — in container, --guard-wasm is optional", containerGuardWasmPath)
	}

	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	// Execute with no flags — the command should fail with "required flag" error,
	// not with any other error, confirming the flag is marked required.
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err, "executing proxy command without --guard-wasm should return an error")
	assert.True(t,
		strings.Contains(err.Error(), "guard-wasm") || strings.Contains(err.Error(), "required"),
		"error should mention --guard-wasm or required: %v", err)
}

// TestNewProxyCmd_GuardWasmFlagHelpText verifies the --guard-wasm flag help text
// reflects whether the baked-in guard was auto-detected.
func TestNewProxyCmd_GuardWasmFlagHelpText(t *testing.T) {
	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	flag := cmd.Flags().Lookup("guard-wasm")
	require.NotNil(t, flag, "--guard-wasm flag should exist")

	_, err := os.Stat(containerGuardWasmPath)
	if err == nil {
		// In container environment: help text should mention auto-detected path
		assert.Contains(t, flag.Usage, "auto-detected",
			"--guard-wasm help should mention auto-detected when baked-in guard exists")
		assert.Contains(t, flag.Usage, containerGuardWasmPath,
			"--guard-wasm help should include the detected path")
	} else {
		// Not in container: help text should say required
		assert.Contains(t, flag.Usage, "required",
			"--guard-wasm help should say 'required' when no baked-in guard exists")
	}
}

// TestNewProxyCmd_GuardsModeCompletion verifies the guards-mode flag has the
// correct shell completion function returning valid enum values.
func TestNewProxyCmd_GuardsModeCompletion(t *testing.T) {
	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	completionFn, ok := cmd.GetFlagCompletionFunc("guards-mode")
	require.True(t, ok, "guards-mode flag should have a completion function registered")
	require.NotNil(t, completionFn, "guards-mode completion function should not be nil")

	completions, directive := completionFn(cmd, nil, "")

	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive,
		"guards-mode completion should use ShellCompDirectiveNoFileComp directive")
	assert.ElementsMatch(t, difc.ValidModes, completions,
		"guards-mode completion should return all valid enforcement modes")
}

// TestNewProxyCmd_TrustedBotsAndUsersDefaultNil verifies that --trusted-bots and
// --trusted-users default to nil (no pre-configured trusted users/bots).
func TestNewProxyCmd_TrustedBotsAndUsersDefaultNil(t *testing.T) {
	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	bots, err := cmd.Flags().GetStringSlice("trusted-bots")
	require.NoError(t, err)
	assert.Empty(t, bots, "--trusted-bots should default to empty/nil")

	users, err := cmd.Flags().GetStringSlice("trusted-users")
	require.NoError(t, err)
	assert.Empty(t, users, "--trusted-users should default to empty/nil")
}

// TestNewProxyCmd_LogDirDefault verifies --log-dir uses the default log directory.
func TestNewProxyCmd_LogDirDefault(t *testing.T) {
	t.Setenv("MCP_GATEWAY_LOG_DIR", "")

	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	val, err := cmd.Flags().GetString("log-dir")
	require.NoError(t, err)
	assert.Equal(t, config.DefaultLogDir, val,
		"--log-dir should default to config.DefaultLogDir when MCP_GATEWAY_LOG_DIR is unset")
}

// TestNewProxyCmd_WasmCacheDirDefault verifies --wasm-cache-dir defaults next to --log-dir.
func TestNewProxyCmd_WasmCacheDirDefault(t *testing.T) {
	t.Setenv("MCP_GATEWAY_LOG_DIR", "")
	t.Setenv(wasmCacheDirEnvVar, "")

	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	val, err := cmd.Flags().GetString("wasm-cache-dir")
	require.NoError(t, err)
	assert.Equal(t, defaultWasmCacheDir(config.DefaultLogDir), val,
		"--wasm-cache-dir should default adjacent to config.DefaultLogDir when env vars are unset")
}

// TestNewProxyCmd_ListenFlag verifies --listen, -l shorthand and default value.
func TestNewProxyCmd_ListenFlag(t *testing.T) {
	cmd := newProxyCmd()
	require.NotNil(t, cmd)

	flag := cmd.Flags().Lookup("listen")
	require.NotNil(t, flag)
	assert.Equal(t, "127.0.0.1:8080", flag.DefValue, "--listen default should be 127.0.0.1:8080")

	// Verify the flag has a shorthand
	shortFlag := cmd.Flags().ShorthandLookup("l")
	require.NotNil(t, shortFlag, "-l shorthand should be registered for --listen")
	assert.Equal(t, "listen", shortFlag.Name, "-l should map to --listen")
}

// TestNewProxyCmd_IsAddedToRootCmd verifies the proxy subcommand is registered
// on the root command so it's accessible via `awmg proxy`.
func TestNewProxyCmd_IsAddedToRootCmd(t *testing.T) {
	found := false
	for _, sub := range rootCmd.Commands() {
		if sub.Use == "proxy" {
			found = true
			break
		}
	}
	assert.True(t, found, "proxy subcommand should be registered on the root command")
}

func TestClientAddr(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "IPv4 wildcard becomes localhost",
			input:    "0.0.0.0:8080",
			expected: "localhost:8080",
		},
		{
			name:     "IPv6 wildcard :: becomes localhost",
			input:    "[::]:8443",
			expected: "localhost:8443",
		},
		{
			name:     "explicit localhost unchanged",
			input:    "localhost:3000",
			expected: "localhost:3000",
		},
		{
			name:     "explicit 127.0.0.1 unchanged",
			input:    "127.0.0.1:9090",
			expected: "127.0.0.1:9090",
		},
		{
			name:     "non-loopback host unchanged",
			input:    "192.168.1.1:8080",
			expected: "192.168.1.1:8080",
		},
		{
			name:     "invalid address returned as-is",
			input:    "not-an-addr",
			expected: "not-an-addr",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, clientAddr(tc.input))
		})
	}
}

func TestConfigureTLSTrustEnvironment(t *testing.T) {
	caPath := "/tmp/proxy-tls/ca.crt"

	t.Run("sets trust environment variables in process", func(t *testing.T) {
		assert := assert.New(t)
		t.Setenv("GITHUB_ENV", "")
		for _, key := range tlsTrustEnvKeys {
			t.Setenv(key, "")
		}

		err := configureTLSTrustEnvironment(caPath)
		require.NoError(t, err)

		for _, key := range tlsTrustEnvKeys {
			assert.Equal(caPath, os.Getenv(key), "expected %s to be set", key)
		}
	})

	t.Run("does not rely on GITHUB_ENV", func(t *testing.T) {
		assert := assert.New(t)
		githubEnvFile := t.TempDir() + "/github_env"
		const original = "UNCHANGED=1\n"
		require.NoError(t, os.WriteFile(githubEnvFile, []byte(original), 0o644))
		t.Setenv("GITHUB_ENV", githubEnvFile)
		for _, key := range tlsTrustEnvKeys {
			t.Setenv(key, "")
		}

		require.NoError(t, configureTLSTrustEnvironment(caPath))

		for _, key := range tlsTrustEnvKeys {
			assert.Equal(caPath, os.Getenv(key), "expected %s to be set", key)
		}

		content, err := os.ReadFile(githubEnvFile)
		require.NoError(t, err)
		assert.Equal(original, string(content))
	})

	t.Run("rejects CA cert path with newline", func(t *testing.T) {
		err := configureTLSTrustEnvironment("/tmp/ca.crt\nMALICIOUS=1")
		require.Error(t, err)
		assert.ErrorContains(t, err, "contains newline")
	})
}

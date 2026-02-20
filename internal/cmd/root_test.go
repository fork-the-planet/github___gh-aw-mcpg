package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

<<<<<<< claude/refactor-semantic-function-clustering
// Note: TestGetDefaultLogDir is defined in flags_logging_test.go

=======
>>>>>>> main
func TestDefaultConfigFile(t *testing.T) {
	// Verify that the default config file is empty (no default config loading)
	assert.Empty(t, defaultConfigFile, "defaultConfigFile should be empty string")
}

func TestRunRequiresConfigSource(t *testing.T) {
	// Save original values
	origConfigFile := configFile
	origConfigStdin := configStdin
	t.Cleanup(func() {
		configFile = origConfigFile
		configStdin = origConfigStdin
	})

	// Note: The validation for "one of config or config-stdin is required" is now
	// handled by Cobra's MarkFlagsOneRequired, which validates at command execution time,
	// not in preRun. Therefore, preRun should pass validation as long as at least one is set.

	t.Run("config file provided", func(t *testing.T) {
		configFile = "test.toml"
		configStdin = false
		err := preRun(nil, nil)
		// Should pass validation when --config is provided
		assert.NoError(t, err, "Should not error when --config is provided")
	})

	t.Run("config stdin provided", func(t *testing.T) {
		configFile = ""
		configStdin = true
		err := preRun(nil, nil)
		// Should pass validation when --config-stdin is provided
		assert.NoError(t, err, "Should not error when --config-stdin is provided")
	})

	t.Run("both config file and stdin provided", func(t *testing.T) {
		configFile = "test.toml"
		configStdin = true
		err := preRun(nil, nil)
		// When both are provided, should pass validation
		assert.NoError(t, err, "Should not error when both are provided")
	})
}

// TestPreRunValidation tests the preRun validation function
func TestPreRunValidation(t *testing.T) {
	// Save original values
	origConfigFile := configFile
	origConfigStdin := configStdin
	origVerbosity := verbosity
	t.Cleanup(func() {
		configFile = origConfigFile
		configStdin = origConfigStdin
		verbosity = origVerbosity
	})

	t.Run("validation passes with config file", func(t *testing.T) {
		configFile = "test.toml"
		configStdin = false
		verbosity = 0
		err := preRun(nil, nil)
		assert.NoError(t, err)
	})

	t.Run("validation passes with config stdin", func(t *testing.T) {
		configFile = ""
		configStdin = true
		verbosity = 0
		err := preRun(nil, nil)
		assert.NoError(t, err)
	})

	// Note: validation for "one of config or config-stdin is required" is now
	// handled by Cobra's MarkFlagsOneRequired, so preRun doesn't check this anymore

	t.Run("verbosity level 1 does not set DEBUG", func(t *testing.T) {
		// Save and clear DEBUG env var
		origDebug, wasSet := os.LookupEnv("DEBUG")
		t.Cleanup(func() {
			if wasSet {
				os.Setenv("DEBUG", origDebug)
			} else {
				os.Unsetenv("DEBUG")
			}
		})
		os.Unsetenv("DEBUG")

		configFile = "test.toml"
		configStdin = false
		verbosity = 1
		err := preRun(nil, nil)
		assert.NoError(t, err)
		// Level 1 doesn't set DEBUG env var
		assert.Empty(t, os.Getenv(logger.EnvDebug))
	})

	t.Run("verbosity level 2 sets DEBUG for main packages", func(t *testing.T) {
		// Save and clear DEBUG env var
		origDebug, wasSet := os.LookupEnv(logger.EnvDebug)
		t.Cleanup(func() {
			if wasSet {
				os.Setenv(logger.EnvDebug, origDebug)
			} else {
				os.Unsetenv(logger.EnvDebug)
			}
		})
		os.Unsetenv(logger.EnvDebug)

		configFile = "test.toml"
		configStdin = false
		verbosity = 2
		err := preRun(nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, "cmd:*,server:*,launcher:*", os.Getenv(logger.EnvDebug))
	})

	t.Run("verbosity level 3 sets DEBUG to all", func(t *testing.T) {
		// Save and clear DEBUG env var
		origDebug, wasSet := os.LookupEnv(logger.EnvDebug)
		t.Cleanup(func() {
			if wasSet {
				os.Setenv(logger.EnvDebug, origDebug)
			} else {
				os.Unsetenv(logger.EnvDebug)
			}
		})
		os.Unsetenv(logger.EnvDebug)

		configFile = "test.toml"
		configStdin = false
		verbosity = 3
		err := preRun(nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, "*", os.Getenv(logger.EnvDebug))
	})

	t.Run("does not override existing DEBUG env var", func(t *testing.T) {
		// Save DEBUG env var
		origDebug, wasSet := os.LookupEnv(logger.EnvDebug)
		t.Cleanup(func() {
			if wasSet {
				os.Setenv(logger.EnvDebug, origDebug)
			} else {
				os.Unsetenv(logger.EnvDebug)
			}
		})
		os.Setenv(logger.EnvDebug, "custom:*")

		configFile = "test.toml"
		configStdin = false
		verbosity = 2
		err := preRun(nil, nil)
		assert.NoError(t, err)
		// Should not override existing DEBUG
		assert.Equal(t, "custom:*", os.Getenv(logger.EnvDebug))
	})
}

func TestLoadEnvFile(t *testing.T) {
	t.Run("load valid env file", func(t *testing.T) {
		// Create temporary env file
		tmpDir := t.TempDir()
		envFile := filepath.Join(tmpDir, ".env")
		content := `# Comment line
TEST_VAR1=value1
TEST_VAR2=value2
EMPTY_LINE=

# Another comment
TEST_VAR3=value with spaces
`
		err := os.WriteFile(envFile, []byte(content), 0644)
		require.NoError(t, err)

		// Save and restore environment variables
		origTestVar1, testVar1WasSet := os.LookupEnv("TEST_VAR1")
		origTestVar2, testVar2WasSet := os.LookupEnv("TEST_VAR2")
		origTestVar3, testVar3WasSet := os.LookupEnv("TEST_VAR3")
		origEmptyLine, emptyLineWasSet := os.LookupEnv("EMPTY_LINE")
		t.Cleanup(func() {
			if testVar1WasSet {
				require.NoError(t, os.Setenv("TEST_VAR1", origTestVar1))
			} else {
				require.NoError(t, os.Unsetenv("TEST_VAR1"))
			}
			if testVar2WasSet {
				require.NoError(t, os.Setenv("TEST_VAR2", origTestVar2))
			} else {
				require.NoError(t, os.Unsetenv("TEST_VAR2"))
			}
			if testVar3WasSet {
				require.NoError(t, os.Setenv("TEST_VAR3", origTestVar3))
			} else {
				require.NoError(t, os.Unsetenv("TEST_VAR3"))
			}
			if emptyLineWasSet {
				require.NoError(t, os.Setenv("EMPTY_LINE", origEmptyLine))
			} else {
				require.NoError(t, os.Unsetenv("EMPTY_LINE"))
			}
		})

		// Load env file
		err = loadEnvFile(envFile)
		require.NoError(t, err)

		// Verify variables are set
		assert.Equal(t, "value1", os.Getenv("TEST_VAR1"))
		assert.Equal(t, "value2", os.Getenv("TEST_VAR2"))
		assert.Equal(t, "value with spaces", os.Getenv("TEST_VAR3"))
		assert.Equal(t, "", os.Getenv("EMPTY_LINE"))
	})

	t.Run("nonexistent file", func(t *testing.T) {
		err := loadEnvFile("/nonexistent/path/.env")
		require.Error(t, err, "Should error on nonexistent file")
	})

	t.Run("env file with variable expansion", func(t *testing.T) {
		// Save original values and set up cleanup before modifying environment
		origBasePath, basePathWasSet := os.LookupEnv("BASE_PATH")
		origExpandedVar, expandedVarWasSet := os.LookupEnv("EXPANDED_VAR")
		t.Cleanup(func() {
			if basePathWasSet {
				_ = os.Setenv("BASE_PATH", origBasePath)
			} else {
				_ = os.Unsetenv("BASE_PATH")
			}
			if expandedVarWasSet {
				_ = os.Setenv("EXPANDED_VAR", origExpandedVar)
			} else {
				_ = os.Unsetenv("EXPANDED_VAR")
			}
		})

		// Set up a base variable for expansion
		os.Setenv("BASE_PATH", "/home/user")
		tmpDir := t.TempDir()
		envFile := filepath.Join(tmpDir, ".env")
		content := `EXPANDED_VAR=$BASE_PATH/subdir`
		err := os.WriteFile(envFile, []byte(content), 0644)
		require.NoError(t, err)

		err = loadEnvFile(envFile)
		require.NoError(t, err)

		assert.Equal(t, "/home/user/subdir", os.Getenv("EXPANDED_VAR"))
	})

	t.Run("empty file", func(t *testing.T) {
		tmpDir := t.TempDir()
		envFile := filepath.Join(tmpDir, ".env")
		err := os.WriteFile(envFile, []byte(""), 0644)
		require.NoError(t, err)

		err = loadEnvFile(envFile)
		require.NoError(t, err, "Empty file should not cause error")
	})
}

func TestWriteGatewayConfig(t *testing.T) {
	t.Run("unified mode with API key", func(t *testing.T) {
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{
				"test-server": {
					Type: "stdio",
				},
			},
			Gateway: &config.GatewayConfig{
				APIKey: "test-api-key",
			},
		}

		var buf bytes.Buffer
		err := writeGatewayConfig(cfg, "127.0.0.1:3000", "unified", &buf)
		require.NoError(t, err)

		// Parse JSON output
		var result map[string]interface{}
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err, "Output should be valid JSON")

		// Verify mcpServers structure
		mcpServers, ok := result["mcpServers"].(map[string]interface{})
		require.True(t, ok, "Output should have mcpServers field")

		// Verify test-server exists
		serverConfig, ok := mcpServers["test-server"].(map[string]interface{})
		require.True(t, ok, "test-server should exist in mcpServers")

		// Verify server type is http
		assert.Equal(t, "http", serverConfig["type"], "Server type should be http")

		// Verify URL is correct for unified mode
		assert.Equal(t, "http://127.0.0.1:3000/mcp", serverConfig["url"], "URL should be correct for unified mode")

		// Verify Authorization header exists
		headers, ok := serverConfig["headers"].(map[string]interface{})
		require.True(t, ok, "Server should have headers field")
		assert.Equal(t, "test-api-key", headers["Authorization"], "Authorization header should match API key")
	})

	t.Run("routed mode without API key", func(t *testing.T) {
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{
				"server1": {Type: "stdio"},
				"server2": {Type: "stdio"},
			},
		}

		var buf bytes.Buffer
		err := writeGatewayConfig(cfg, "localhost:8080", "routed", &buf)
		require.NoError(t, err)

		// Parse JSON output
		var result map[string]interface{}
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err, "Output should be valid JSON")

		// Verify mcpServers structure
		mcpServers, ok := result["mcpServers"].(map[string]interface{})
		require.True(t, ok, "Output should have mcpServers field")

		// Verify both servers exist
		server1Config, ok := mcpServers["server1"].(map[string]interface{})
		require.True(t, ok, "server1 should exist in mcpServers")

		server2Config, ok := mcpServers["server2"].(map[string]interface{})
		require.True(t, ok, "server2 should exist in mcpServers")

		// Verify URLs are correct for routed mode
		assert.Equal(t, "http://localhost:8080/mcp/server1", server1Config["url"], "server1 URL should include server name")
		assert.Equal(t, "http://localhost:8080/mcp/server2", server2Config["url"], "server2 URL should include server name")

		// Verify no Authorization headers when no API key
		_, hasHeaders1 := server1Config["headers"]
		assert.False(t, hasHeaders1, "server1 should not have headers when no API key")

		_, hasHeaders2 := server2Config["headers"]
		assert.False(t, hasHeaders2, "server2 should not have headers when no API key")
	})

	t.Run("with tools field", func(t *testing.T) {
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{
				"test-server": {
					Type:  "stdio",
					Tools: []string{"tool1", "tool2"},
				},
			},
		}

		var buf bytes.Buffer
		err := writeGatewayConfig(cfg, "127.0.0.1:3000", "unified", &buf)
		require.NoError(t, err)

		// Parse JSON output
		var result map[string]interface{}
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err, "Output should be valid JSON")

		// Verify mcpServers structure
		mcpServers, ok := result["mcpServers"].(map[string]interface{})
		require.True(t, ok, "Output should have mcpServers field")

		// Verify test-server exists
		serverConfig, ok := mcpServers["test-server"].(map[string]interface{})
		require.True(t, ok, "test-server should exist in mcpServers")

		// Verify tools field exists and has correct values
		tools, ok := serverConfig["tools"].([]interface{})
		require.True(t, ok, "Server should have tools field")
		require.Len(t, tools, 2, "Should have 2 tools")

		// Convert to string slice for easier comparison
		toolsStr := make([]string, len(tools))
		for i, tool := range tools {
			toolsStr[i] = tool.(string)
		}
		assert.ElementsMatch(t, []string{"tool1", "tool2"}, toolsStr, "Tools should match")
	})

	t.Run("IPv6 address", func(t *testing.T) {
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{
				"test-server": {Type: "stdio"},
			},
		}

		var buf bytes.Buffer
		err := writeGatewayConfig(cfg, "[::1]:3000", "unified", &buf)
		require.NoError(t, err)

		// Parse JSON output
		var result map[string]interface{}
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err, "Output should be valid JSON")

		// Verify mcpServers structure
		mcpServers, ok := result["mcpServers"].(map[string]interface{})
		require.True(t, ok, "Output should have mcpServers field")

		// Verify test-server exists
		serverConfig, ok := mcpServers["test-server"].(map[string]interface{})
		require.True(t, ok, "test-server should exist in mcpServers")

		// Verify URL is correct for IPv6 address
		assert.Equal(t, "http://::1:3000/mcp", serverConfig["url"], "URL should be correct for IPv6 address")
	})

	t.Run("invalid listen address uses defaults", func(t *testing.T) {
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{
				"test-server": {Type: "stdio"},
			},
		}

		var buf bytes.Buffer
		err := writeGatewayConfig(cfg, "invalid-address", "unified", &buf)
		require.NoError(t, err)

		output := buf.String()
		// Should fall back to default host and port
		assert.Contains(t, output, DefaultListenIPv4)
		assert.Contains(t, output, DefaultListenPort)
	})
}

// TestContextCancellation tests that context cancellation works properly
func TestContextCancellation(t *testing.T) {
	t.Run("context with timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Wait for context to be done
		<-ctx.Done()

		// Verify context was cancelled due to timeout
		assert.Equal(t, context.DeadlineExceeded, ctx.Err())
	})

	t.Run("context with cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel immediately
		cancel()

		// Wait for context to be done
		<-ctx.Done()

		// Verify context was cancelled
		assert.Equal(t, context.Canceled, ctx.Err())
	})
}

// TestFlagValidationGroups tests that flag validation groups work correctly
func TestFlagValidationGroups(t *testing.T) {
	// Note: This tests that the flag validation groups are registered correctly.
	// Actual validation is performed by Cobra during command execution.
	t.Run("mutually exclusive flags registered", func(t *testing.T) {
		// Create a new root command to test
		cmd := &cobra.Command{
			Use: "test",
		}
		registerCoreFlags(cmd)

		// Verify flags are registered
		assert.NotNil(t, cmd.Flags().Lookup("routed"))
		assert.NotNil(t, cmd.Flags().Lookup("unified"))
		assert.NotNil(t, cmd.Flags().Lookup("config"))
		assert.NotNil(t, cmd.Flags().Lookup("config-stdin"))
	})
}

// TestVersionTemplate tests that custom version template is set
func TestVersionTemplate(t *testing.T) {
	t.Run("version template is set", func(t *testing.T) {
		// The version template should be set during init
		// We can verify the version command works by checking it's not empty
		assert.NotEmpty(t, rootCmd.Version, "Version should be set")
	})
}

// TestPostRunCleanup tests that postRun cleanup is called
func TestPostRunCleanup(t *testing.T) {
	t.Run("postRun is registered", func(t *testing.T) {
		// Verify that postRun hook is set
		assert.NotNil(t, rootCmd.PersistentPostRun, "PersistentPostRun should be set")
	})
}

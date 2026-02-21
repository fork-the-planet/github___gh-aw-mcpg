package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConvertStdinServerConfig_StdioServer tests stdio server conversion with full configuration.
func TestConvertStdinServerConfig_StdioServer(t *testing.T) {
	server := &StdinServerConfig{
		Type:           "stdio",
		Container:      "test/container:latest",
		Entrypoint:     "/bin/custom",
		EntrypointArgs: []string{"arg1", "arg2"},
		Args:           []string{"--network", "host"},
		Mounts:         []string{"/tmp:/tmp:rw"},
		Env: map[string]string{
			"TEST_VAR":    "value123",
			"PASSTHROUGH": "",
			"ANOTHER_VAR": "abc",
		},
		Tools: []string{"tool1", "tool2"},
	}

	result, err := convertStdinServerConfig("test-server", server, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "stdio", result.Type)
	assert.Equal(t, "docker", result.Command)
	assert.Contains(t, result.Args, "run")
	assert.Contains(t, result.Args, "--rm")
	assert.Contains(t, result.Args, "-i")

	// Check standard environment variables
	assert.Contains(t, result.Args, "NO_COLOR=1")
	assert.Contains(t, result.Args, "TERM=dumb")
	assert.Contains(t, result.Args, "PYTHONUNBUFFERED=1")

	// Check custom entrypoint
	assert.Contains(t, result.Args, "--entrypoint")
	assert.Contains(t, result.Args, "/bin/custom")

	// Check mounts
	assert.Contains(t, result.Args, "-v")
	assert.Contains(t, result.Args, "/tmp:/tmp:rw")

	// Check user environment variables
	assert.Contains(t, result.Args, "TEST_VAR=value123")
	assert.Contains(t, result.Args, "PASSTHROUGH")
	assert.Contains(t, result.Args, "ANOTHER_VAR=abc")

	// Check additional Docker args
	assert.Contains(t, result.Args, "--network")
	assert.Contains(t, result.Args, "host")

	// Check container name
	assert.Contains(t, result.Args, "test/container:latest")

	// Check entrypoint args
	assert.Contains(t, result.Args, "arg1")
	assert.Contains(t, result.Args, "arg2")

	// Check tools
	assert.Equal(t, []string{"tool1", "tool2"}, result.Tools)
}

// TestConvertStdinServerConfig_HTTPServer tests HTTP server conversion.
func TestConvertStdinServerConfig_HTTPServer(t *testing.T) {
	server := &StdinServerConfig{
		Type: "http",
		URL:  "http://example.com:8080",
		Headers: map[string]string{
			"Authorization": "Bearer token123",
			"X-Custom":      "value",
		},
		Tools: []string{"search", "fetch"},
	}

	result, err := convertStdinServerConfig("http-server", server, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "http", result.Type)
	assert.Equal(t, "http://example.com:8080", result.URL)
	assert.Equal(t, "Bearer token123", result.Headers["Authorization"])
	assert.Equal(t, "value", result.Headers["X-Custom"])
	assert.Equal(t, []string{"search", "fetch"}, result.Tools)

	// HTTP servers should not have Command or Args
	assert.Empty(t, result.Command)
	assert.Empty(t, result.Args)
}

// TestConvertStdinServerConfig_MinimalStdioServer tests minimal stdio server configuration.
func TestConvertStdinServerConfig_MinimalStdioServer(t *testing.T) {
	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "minimal/container:v1",
	}

	result, err := convertStdinServerConfig("minimal", server, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "stdio", result.Type)
	assert.Equal(t, "docker", result.Command)
	assert.Contains(t, result.Args, "minimal/container:v1")

	// Should still have standard env vars
	assert.Contains(t, result.Args, "NO_COLOR=1")
	assert.Contains(t, result.Args, "TERM=dumb")
	assert.Contains(t, result.Args, "PYTHONUNBUFFERED=1")
}

// TestConvertStdinServerConfig_TypeNormalization tests "local" type normalization to "stdio".
func TestConvertStdinServerConfig_TypeNormalization(t *testing.T) {
	testCases := []struct {
		name         string
		inputType    string
		expectedType string
	}{
		{
			name:         "local normalized to stdio",
			inputType:    "local",
			expectedType: "stdio",
		},
		{
			name:         "empty type defaults to stdio",
			inputType:    "",
			expectedType: "stdio",
		},
		{
			name:         "stdio remains stdio",
			inputType:    "stdio",
			expectedType: "stdio",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := &StdinServerConfig{
				Type:      tc.inputType,
				Container: "test/container:latest",
			}

			result, err := convertStdinServerConfig("test", server, nil)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tc.expectedType, result.Type)
		})
	}
}

// TestConvertStdinServerConfig_EnvVariableExpansion tests environment variable expansion.
func TestConvertStdinServerConfig_EnvVariableExpansion(t *testing.T) {
	// Set up test environment variable
	os.Setenv("TEST_TOKEN", "secret123")
	defer os.Unsetenv("TEST_TOKEN")

	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "test/container:latest",
		Env: map[string]string{
			"API_TOKEN": "${TEST_TOKEN}",
			"STATIC":    "static-value",
		},
	}

	result, err := convertStdinServerConfig("test", server, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check that variable was expanded in Args
	found := false
	for _, arg := range result.Args {
		if arg == "API_TOKEN=secret123" {
			found = true
			break
		}
	}
	assert.True(t, found, "Expanded env variable not found in Args")

	// Check static value
	staticFound := false
	for _, arg := range result.Args {
		if arg == "STATIC=static-value" {
			staticFound = true
			break
		}
	}
	assert.True(t, staticFound, "Static env variable not found in Args")
}

// TestConvertStdinServerConfig_EnvExpansionError tests error handling for undefined variables.
func TestConvertStdinServerConfig_EnvExpansionError(t *testing.T) {
	os.Unsetenv("UNDEFINED_VAR")

	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "test/container:latest",
		Env: map[string]string{
			"BAD_VAR": "${UNDEFINED_VAR}",
		},
	}

	result, err := convertStdinServerConfig("test", server, nil)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "UNDEFINED_VAR")
}

// TestConvertStdinServerConfig_HeadersExpansion tests HTTP headers expansion.
func TestConvertStdinServerConfig_HeadersExpansion(t *testing.T) {
	os.Setenv("AUTH_TOKEN", "bearer-token-xyz")
	defer os.Unsetenv("AUTH_TOKEN")

	server := &StdinServerConfig{
		Type: "http",
		URL:  "http://example.com",
		Headers: map[string]string{
			"Authorization": "${AUTH_TOKEN}",
			"X-Static":      "static-header",
		},
	}

	result, err := convertStdinServerConfig("test", server, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "bearer-token-xyz", result.Headers["Authorization"])
	assert.Equal(t, "static-header", result.Headers["X-Static"])
}

// TestConvertStdinServerConfig_HeadersExpansionError tests error handling for headers with undefined variables.
func TestConvertStdinServerConfig_HeadersExpansionError(t *testing.T) {
	os.Unsetenv("MISSING_TOKEN")

	server := &StdinServerConfig{
		Type: "http",
		URL:  "http://example.com",
		Headers: map[string]string{
			"Authorization": "${MISSING_TOKEN}",
		},
	}

	result, err := convertStdinServerConfig("test", server, nil)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "MISSING_TOKEN")
}

// TestConvertStdinServerConfig_ValidationError tests validation error handling.
func TestConvertStdinServerConfig_ValidationError(t *testing.T) {
	testCases := []struct {
		name          string
		server        *StdinServerConfig
		errorContains string
	}{
		{
			name: "stdio without container",
			server: &StdinServerConfig{
				Type: "stdio",
				// Missing Container field
			},
			errorContains: "container",
		},
		{
			name: "http without URL",
			server: &StdinServerConfig{
				Type: "http",
				// Missing URL field
			},
			errorContains: "url",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := convertStdinServerConfig("test", tc.server, nil)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errorContains)
		})
	}
}

// TestConvertStdinServerConfig_EmptyEnvAndHeaders tests handling of empty env and headers.
func TestConvertStdinServerConfig_EmptyEnvAndHeaders(t *testing.T) {
	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "test/container:latest",
		Env:       map[string]string{},
		Headers:   map[string]string{},
	}

	result, err := convertStdinServerConfig("test", server, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "stdio", result.Type)
	// Should not cause any errors with empty maps
}

// TestConvertStdinServerConfig_NilEnvAndHeaders tests handling of nil env and headers.
func TestConvertStdinServerConfig_NilEnvAndHeaders(t *testing.T) {
	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "test/container:latest",
		Env:       nil,
		Headers:   nil,
	}

	result, err := convertStdinServerConfig("test", server, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "stdio", result.Type)
	// Should not panic with nil maps
}

// TestConvertStdinServerConfig_MultipleEnvVariables tests multiple environment variables.
func TestConvertStdinServerConfig_MultipleEnvVariables(t *testing.T) {
	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "test/container:latest",
		Env: map[string]string{
			"VAR1": "value1",
			"VAR2": "value2",
			"VAR3": "value3",
			"VAR4": "",
		},
	}

	result, err := convertStdinServerConfig("test", server, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check all variables are present
	assert.Contains(t, result.Args, "VAR1=value1")
	assert.Contains(t, result.Args, "VAR2=value2")
	assert.Contains(t, result.Args, "VAR3=value3")
	assert.Contains(t, result.Args, "VAR4")
}

// TestConvertStdinServerConfig_CustomSchemas tests custom schemas support.
func TestConvertStdinServerConfig_CustomSchemas(t *testing.T) {
	customSchemas := map[string]interface{}{
		"customType": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"customField": map[string]interface{}{
					"type": "string",
				},
			},
		},
	}

	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "test/container:latest",
	}

	result, err := convertStdinServerConfig("test", server, customSchemas)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "stdio", result.Type)
	// Custom schemas should be passed to validation without error
}

// TestConvertStdinServerConfig_ArgsOrdering tests that Docker args are in correct order.
func TestConvertStdinServerConfig_ArgsOrdering(t *testing.T) {
	server := &StdinServerConfig{
		Type:           "stdio",
		Container:      "test/container:latest",
		Entrypoint:     "/custom/entry",
		EntrypointArgs: []string{"entry-arg1"},
		Args:           []string{"--custom-flag", "value"},
		Mounts:         []string{"/host:/container:ro"},
		Env: map[string]string{
			"MY_VAR": "my-value",
		},
	}

	result, err := convertStdinServerConfig("test", server, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Find indices of key elements to verify ordering
	runIdx := indexOf(result.Args, "run")
	entrypointIdx := indexOf(result.Args, "--entrypoint")
	mountIdx := indexOf(result.Args, "-v")
	containerIdx := indexOf(result.Args, "test/container:latest")
	entryArgIdx := indexOf(result.Args, "entry-arg1")

	assert.True(t, runIdx >= 0, "run should be present")
	assert.True(t, containerIdx >= 0, "container should be present")

	// Standard Docker args (run, --rm, -i) should come first
	assert.True(t, runIdx < entrypointIdx, "run should come before entrypoint")

	// Entrypoint should come before container
	if entrypointIdx >= 0 {
		assert.True(t, entrypointIdx < containerIdx, "entrypoint should come before container")
	}

	// Mounts should come before container
	if mountIdx >= 0 {
		assert.True(t, mountIdx < containerIdx, "mounts should come before container")
	}

	// Entrypoint args should come after container
	if entryArgIdx >= 0 && containerIdx >= 0 {
		assert.True(t, entryArgIdx > containerIdx, "entrypoint args should come after container")
	}
}

// Helper function to find index of element in slice.
func indexOf(slice []string, target string) int {
	for i, v := range slice {
		if v == target {
			return i
		}
	}
	return -1
}

// TestConvertStdinServerConfig_HTTPServerNoDockerArgs tests that HTTP servers don't get Docker args.
func TestConvertStdinServerConfig_HTTPServerNoDockerArgs(t *testing.T) {
	server := &StdinServerConfig{
		Type: "http",
		URL:  "http://example.com",
		// These fields should be ignored for HTTP servers
		Container:      "ignored/container:latest",
		Entrypoint:     "/ignored",
		EntrypointArgs: []string{"ignored"},
		Args:           []string{"--ignored"},
		Env: map[string]string{
			"IGNORED": "ignored",
		},
	}

	result, err := convertStdinServerConfig("test", server, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "http", result.Type)
	assert.Empty(t, result.Command, "HTTP server should not have Command")
	assert.Empty(t, result.Args, "HTTP server should not have Args")
	assert.NotContains(t, result.Args, "ignored/container:latest")
}

// TestConvertStdinServerConfig_MixedEnvPassthroughAndExplicit tests mixing passthrough and explicit env vars.
func TestConvertStdinServerConfig_MixedEnvPassthroughAndExplicit(t *testing.T) {
	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "test/container:latest",
		Env: map[string]string{
			"EXPLICIT":  "value",
			"PASS1":     "",
			"EXPLICIT2": "value2",
			"PASS2":     "",
		},
	}

	result, err := convertStdinServerConfig("test", server, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check explicit values are formatted correctly
	assert.Contains(t, result.Args, "EXPLICIT=value")
	assert.Contains(t, result.Args, "EXPLICIT2=value2")

	// Check passthrough values are formatted correctly (no = sign)
	hasPass1 := false
	hasPass2 := false
	for i, arg := range result.Args {
		if arg == "-e" && i+1 < len(result.Args) {
			next := result.Args[i+1]
			if next == "PASS1" {
				hasPass1 = true
			}
			if next == "PASS2" {
				hasPass2 = true
			}
		}
	}
	assert.True(t, hasPass1, "PASS1 passthrough not found")
	assert.True(t, hasPass2, "PASS2 passthrough not found")
}

package config

// Tests targeting specific uncovered branches in LoadFromStdin and normalizeLocalType
// to improve function coverage from 80.6% and 95.5% respectively.
//
// Uncovered branches identified via go test -covermode=count:
//   - LoadFromStdin lines 298-300: io.ReadAll error (broken stdin)
//   - LoadFromStdin lines 338-340: validateStringPatterns error after schema passes
//   - LoadFromStdin lines 343-345: validateCustomSchemas error after schema passes
//   - LoadFromStdin lines 348-350: validateGatewayConfig error after schema passes
//   - normalizeLocalType line 616-617: continue when server value is not a map

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stdinFromString sets os.Stdin to a pipe containing the given string,
// calls f(), then restores os.Stdin.
func stdinFromString(t *testing.T, input string, f func()) {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	writeErrCh := make(chan error, 1)
	go func() {
		defer w.Close()
		_, err := w.Write([]byte(input))
		writeErrCh <- err
	}()

	f()
	require.NoError(t, <-writeErrCh)
}

// TestLoadFromStdin_StdinReadError covers the io.ReadAll error path (lines 298-300).
// When os.Stdin is a closed file, reading returns an error immediately.
func TestLoadFromStdin_StdinReadError(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	require.NoError(t, w.Close())
	require.NoError(t, r.Close()) // close the read end before reading

	_, loadErr := LoadFromStdin()

	require.Error(t, loadErr)
	assert.ErrorContains(t, loadErr, "failed to read stdin")
}

// TestLoadFromStdin_ValidateStringPatternsError covers the validateStringPatterns error
// path (lines 338-340). A whitespace-only entrypoint passes JSON schema validation
// (schema enforces non-empty string) but is rejected by the Go-side validateStringPatterns
// check which additionally enforces that the value is not whitespace-only.
func TestLoadFromStdin_ValidateStringPatternsError(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"test": {
				"type": "stdio",
				"container": "test:latest",
				"entrypoint": "   "
			}
		},
		"gateway": {
			"port": 8080,
			"domain": "localhost",
			"apiKey": "test-key"
		}
	}`

	var loadErr error
	stdinFromString(t, jsonConfig, func() {
		_, loadErr = LoadFromStdin()
	})

	require.Error(t, loadErr)
	assert.ErrorContains(t, loadErr, "entrypoint cannot be empty or whitespace only")
}

// TestLoadFromStdin_ValidateCustomSchemasError covers the validateCustomSchemas error
// path (lines 343-345). The JSON schema allows any key matching ^[a-z][a-z0-9-]*$
// (because fixSchemaBytes replaces negative lookahead patterns), so "stdio" and "http"
// pass schema validation. However validateCustomSchemas rejects reserved type names.
func TestLoadFromStdin_ValidateCustomSchemasError_ReservedStdioKey(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"test": {
				"container": "test:latest"
			}
		},
		"gateway": {
			"port": 8080,
			"domain": "localhost",
			"apiKey": "test-key"
		},
		"customSchemas": {
			"stdio": "https://example.com/schema.json"
		}
	}`

	var loadErr error
	stdinFromString(t, jsonConfig, func() {
		_, loadErr = LoadFromStdin()
	})

	require.Error(t, loadErr)
	assert.ErrorContains(t, loadErr, "stdio")
}

// TestLoadFromStdin_ValidateCustomSchemasError_ReservedHttpKey is the same as above
// but uses "http" as the reserved key.
func TestLoadFromStdin_ValidateCustomSchemasError_ReservedHttpKey(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"test": {
				"container": "test:latest"
			}
		},
		"gateway": {
			"port": 8080,
			"domain": "localhost",
			"apiKey": "test-key"
		},
		"customSchemas": {
			"http": "https://example.com/schema.json"
		}
	}`

	var loadErr error
	stdinFromString(t, jsonConfig, func() {
		_, loadErr = LoadFromStdin()
	})

	require.Error(t, loadErr)
	assert.ErrorContains(t, loadErr, "http")
}

// TestLoadFromStdin_ValidateGatewayConfigError covers the validateGatewayConfig error
// path for invalid OpenTelemetry configuration. An all-zero W3C traceId is a valid
// 32-character lowercase hex string that passes JSON schema validation but is rejected
// by validateOpenTelemetryConfig because W3C Trace Context forbids an all-zero trace-id.
func TestLoadFromStdin_ValidateGatewayConfigError_AllZeroTraceId(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"test": {
				"container": "test:latest"
			}
		},
		"gateway": {
			"port": 8080,
			"domain": "localhost",
			"apiKey": "test-key",
			"opentelemetry": {
				"endpoint": "https://otel-collector.example.com",
				"traceId": "00000000000000000000000000000000"
			}
		}
	}`

	var loadErr error
	stdinFromString(t, jsonConfig, func() {
		_, loadErr = LoadFromStdin()
	})

	require.Error(t, loadErr)
	assert.ErrorContains(t, loadErr, "traceId")
}

// TestNormalizeLocalType_NonObjectServerValue covers the continue branch (line 616-617)
// in normalizeLocalType. When a server value inside mcpServers is not a JSON object
// (e.g., an integer), the type assertion to map[string]interface{} fails and the loop
// continues to the next server without modifying the entry.
func TestNormalizeLocalType_NonObjectServerValue(t *testing.T) {
	// "svr" maps to a JSON integer, not an object.
	input := []byte(`{"mcpServers":{"svr":123}}`)
	output, err := normalizeLocalType(input)

	require.NoError(t, err, "non-object server value should not cause an error")
	assert.Equal(t, input, output, "input should be returned unchanged when server is not a map")
}

// TestNormalizeLocalType_MixedObjectAndNonObjectServers verifies that a non-object
// server value is skipped while adjacent object servers are still processed correctly.
func TestNormalizeLocalType_MixedObjectAndNonObjectServers(t *testing.T) {
	input := []byte(`{"mcpServers":{"bad":42,"good":{"type":"local","container":"img:latest"}}}`)
	output, err := normalizeLocalType(input)

	require.NoError(t, err)
	// "local" in the object server should be normalized to "stdio"
	assert.Contains(t, string(output), `"stdio"`)
	assert.NotContains(t, string(output), `"local"`)
}

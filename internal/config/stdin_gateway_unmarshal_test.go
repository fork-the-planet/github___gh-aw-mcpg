package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for StdinGatewayConfig.UnmarshalJSON covering both error paths and
// field-tracking logic.

// TestStdinGatewayConfig_UnmarshalJSON_InvalidJSON covers the first error-return path
// (line ~62 in config_stdin.go) where json.Unmarshal fails because the input is
// not valid JSON.  Previously this path had 0% coverage; this test brings it to 100%.
func TestStdinGatewayConfig_UnmarshalJSON_InvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "plain text is not JSON",
			data: []byte(`not valid json at all`),
		},
		{
			name: "truncated object",
			data: []byte(`{"port":`),
		},
		{
			name: "array instead of object",
			data: []byte(`[1, 2, 3]`),
		},
		{
			name: "null byte",
			data: []byte{0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var g StdinGatewayConfig
			err := g.UnmarshalJSON(tt.data)
			require.Error(t, err, "expected error for invalid JSON input %q", tt.data)
		})
	}
}

// TestStdinGatewayConfig_UnmarshalJSON_FieldTracking verifies that agentIDSet and
// legacyAPIKeySet are correctly tracked after successful unmarshaling.
func TestStdinGatewayConfig_UnmarshalJSON_FieldTracking(t *testing.T) {
	tests := []struct {
		name           string
		json           string
		wantAgentIDSet bool
		wantAPIKeySet  bool
	}{
		{
			name:           "neither agentId nor apiKey present",
			json:           `{"port": 3000}`,
			wantAgentIDSet: false,
			wantAPIKeySet:  false,
		},
		{
			name:           "only agentId present",
			json:           `{"agentId": "my-agent-id"}`,
			wantAgentIDSet: true,
			wantAPIKeySet:  false,
		},
		{
			name:           "only apiKey present (legacy)",
			json:           `{"apiKey": "legacy-key"}`,
			wantAgentIDSet: false,
			wantAPIKeySet:  true,
		},
		{
			name:           "both agentId and apiKey present",
			json:           `{"agentId": "my-agent-id", "apiKey": "legacy-key"}`,
			wantAgentIDSet: true,
			wantAPIKeySet:  true,
		},
		{
			name:           "agentId set to empty string still marks as present",
			json:           `{"agentId": ""}`,
			wantAgentIDSet: true,
			wantAPIKeySet:  false,
		},
		{
			name:           "empty object has neither field",
			json:           `{}`,
			wantAgentIDSet: false,
			wantAPIKeySet:  false,
		},
		{
			name:           "other fields do not affect tracking",
			json:           `{"port": 8080, "domain": "example.com", "startupTimeout": 30}`,
			wantAgentIDSet: false,
			wantAPIKeySet:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var g StdinGatewayConfig
			err := g.UnmarshalJSON([]byte(tt.json))
			require.NoError(t, err)
			assert.Equal(t, tt.wantAgentIDSet, g.agentIDSet,
				"agentIDSet should be %v", tt.wantAgentIDSet)
			assert.Equal(t, tt.wantAPIKeySet, g.legacyAPIKeySet,
				"legacyAPIKeySet should be %v", tt.wantAPIKeySet)
		})
	}
}

// TestStdinGatewayConfig_UnmarshalJSON_FieldValues verifies that scalar fields are
// correctly decoded alongside the tracking booleans.
func TestStdinGatewayConfig_UnmarshalJSON_FieldValues(t *testing.T) {
	port := 4000
	data := []byte(fmt.Sprintf(`{
		"port": %d,
		"agentId": "test-agent",
		"domain": "example.com",
		"startupTimeout": 60,
		"toolTimeout": 120
	}`, port))

	var g StdinGatewayConfig
	require.NoError(t, g.UnmarshalJSON(data))

	require.NotNil(t, g.Port)
	assert.Equal(t, port, *g.Port)
	assert.Equal(t, "test-agent", g.AgentID)
	assert.Equal(t, "example.com", g.Domain)
	require.NotNil(t, g.StartupTimeout)
	assert.Equal(t, 60, *g.StartupTimeout)
	require.NotNil(t, g.ToolTimeout)
	assert.Equal(t, 120, *g.ToolTimeout)
	assert.True(t, g.agentIDSet)
	assert.False(t, g.legacyAPIKeySet)
}

// TestStdinGatewayConfig_UnmarshalJSON_ViaJSONUnmarshal verifies that the custom
// UnmarshalJSON is invoked when StdinGatewayConfig is embedded in a parent struct
// (i.e. via the normal json.Unmarshal path through StdinConfig).
func TestStdinGatewayConfig_UnmarshalJSON_ViaJSONUnmarshal(t *testing.T) {
	data := []byte(`{
		"mcpServers": {
			"github": {
				"type": "stdio",
				"container": "ghcr.io/github/github-mcp-server:latest"
			}
		},
		"gateway": {
			"apiKey": "some-legacy-key",
			"port": 3000
		}
	}`)

	var cfg StdinConfig
	require.NoError(t, json.Unmarshal(data, &cfg))

	require.NotNil(t, cfg.Gateway)
	assert.False(t, cfg.Gateway.agentIDSet,
		"agentId was not in the JSON so agentIDSet should be false")
	assert.True(t, cfg.Gateway.legacyAPIKeySet,
		"apiKey was in the JSON so legacyAPIKeySet should be true")
}

// Tests for validateAgainstCustomSchema covering the AddResource error path when
// the schema's $id is a different (invalid) URL.

// TestValidateAgainstCustomSchema_InvalidSchemaID covers the branch at
// validation.go ~276-282 where compiler.AddResource(schemaID, ...) fails because
// the schema's $id field contains an invalid URL (bad percent-encoding).
// This path was previously unreachable in tests because all test schemas either had
// no $id or had a valid $id URL.
func TestValidateAgainstCustomSchema_InvalidSchemaID(t *testing.T) {
	// Unique URL path to avoid collision with the global customSchemaCache entries
	// from parallel tests.
	const badSchemaID = "http://example.com/bad%zzmust-fail-percent"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		schema := map[string]interface{}{
			"$schema": "http://json-schema.org/draft-07/schema#",
			// $id is a different URL from the fetch URL, but it contains an invalid
			// percent-encoded sequence (%zz) that makes url.Parse fail.
			"$id":  badSchemaID,
			"type": "object",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(schema)
	}))
	defer mockServer.Close()

	schemaURL := mockServer.URL + "/schema-with-invalid-id"
	// Clean up any cache entry so the server is actually contacted.
	t.Cleanup(func() { customSchemaCache.Delete(schemaURL) })

	server := &StdinServerConfig{
		Type:      "mytype",
		Container: "ghcr.io/example/mytype:latest",
	}

	err := validateAgainstCustomSchema("test-server", server, schemaURL, "mcpServers.test-server")

	require.Error(t, err, "expected error when schema $id is an invalid URL")
	assert.ErrorContains(t, err, "failed to compile custom schema",
		"error message should indicate schema compilation failure")
}

// TestValidateAgainstCustomSchema_CompileFailure covers the branch at
// validation.go ~285-290 where compiler.Compile fails because the schema
// references an $id that the compiler cannot resolve.
func TestValidateAgainstCustomSchema_CompileFailure(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A schema whose $id matches the fetch URL (so both AddResource calls use
		// the same URL) but which references an undefined sub-schema via $ref.
		// The jsonschema compiler resolves $ref eagerly and returns an error when
		// the referenced URI is not registered.
		fetchURL := "http://" + r.Host + r.URL.Path
		schema := map[string]interface{}{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"$id":     fetchURL,
			"type":    "object",
			// $ref points to a missing local definition so Compile() fails deterministically
			// without attempting any external network fetch.
			"properties": map[string]interface{}{
				"value": map[string]interface{}{
					"$ref": "#/definitions/doesNotExist",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(schema)
	}))
	defer mockServer.Close()

	schemaURL := mockServer.URL + "/schema-with-bad-ref"
	t.Cleanup(func() { customSchemaCache.Delete(schemaURL) })

	server := &StdinServerConfig{
		Type:      "mytype",
		Container: "ghcr.io/example/mytype:latest",
	}

	err := validateAgainstCustomSchema("test-server", server, schemaURL, "mcpServers.test-server")

	require.Error(t, err, "expected error when schema $ref cannot be resolved")
	assert.ErrorContains(t, err, "failed to compile custom schema",
		"error message should indicate schema compilation failure")
}

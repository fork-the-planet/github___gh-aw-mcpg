package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for validateAgainstCustomSchema and validateCustomServerConfig that cover
// branches not exercised by the existing T-CFG-010 through T-CFG-014 tests.

// TestValidateAgainstCustomSchema_FetchFailure covers the fetchAndFixSchema error path
// (lines 207-215 in validation.go) when the schema server returns a non-200 status.
func TestValidateAgainstCustomSchema_FetchFailure(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	server := &StdinServerConfig{
		Type:      "mytype",
		Container: "ghcr.io/example/mytype:latest",
	}

	err := validateAgainstCustomSchema("test-server", server, mockServer.URL, "mcpServers.test-server")

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to fetch custom schema")
	assert.ErrorContains(t, err, "mytype")
}

// TestValidateAgainstCustomSchema_UnreachableURL covers the fetchAndFixSchema connection
// error path when the schema URL is completely unreachable (server is already closed).
func TestValidateAgainstCustomSchema_UnreachableURL(t *testing.T) {
	// Create and immediately close a server to get an address that refuses connections
	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := closedServer.URL
	closedServer.Close()

	server := &StdinServerConfig{
		Type:      "mytype",
		Container: "ghcr.io/example/mytype:latest",
	}

	err := validateAgainstCustomSchema("test-server", server, unreachableURL, "mcpServers.test-server")

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to fetch custom schema")
}

// TestValidateAgainstCustomSchema_SchemaWithDifferentID covers the branch at lines 248-257
// where the schema's $id differs from the fetch URL. In this case both the fetch URL and
// the $id URL must be registered with the compiler.
func TestValidateAgainstCustomSchema_SchemaWithDifferentID(t *testing.T) {
	const customSchemaID = "https://schemas.example.com/mytype-v1.json"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		schema := map[string]interface{}{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"$id":     customSchemaID,
			"type":    "object",
			"properties": map[string]interface{}{
				"type": map[string]interface{}{
					"type": "string",
				},
				"container": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"type", "container"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(schema)
	}))
	defer mockServer.Close()

	// The fetch URL differs from the schema's $id: compilation uses the $id
	server := &StdinServerConfig{
		Type:      "mytype",
		Container: "ghcr.io/example/mytype:latest",
	}

	err := validateAgainstCustomSchema("test-server", server, mockServer.URL, "mcpServers.test-server")

	// Should pass validation because required fields are present
	assert.NoError(t, err, "schema with $id different from fetch URL should validate successfully")
}

// TestValidateAgainstCustomSchema_SchemaWithDifferentID_MissingRequired verifies that
// schema validation still fails correctly when the schema has a different $id and the
// server config is missing a required field.
func TestValidateAgainstCustomSchema_SchemaWithDifferentID_MissingRequired(t *testing.T) {
	const customSchemaID = "https://schemas.example.com/mytype-v2.json"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		schema := map[string]interface{}{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"$id":     customSchemaID,
			"type":    "object",
			"properties": map[string]interface{}{
				"type": map[string]interface{}{
					"type": "string",
				},
				"container": map[string]interface{}{
					"type": "string",
				},
				"requiredField": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"type", "container", "requiredField"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(schema)
	}))
	defer mockServer.Close()

	// Missing requiredField - validation should fail
	server := &StdinServerConfig{
		Type:      "mytype",
		Container: "ghcr.io/example/mytype:latest",
		// No AdditionalProperties with "requiredField"
	}

	err := validateAgainstCustomSchema("test-server", server, mockServer.URL, "mcpServers.test-server")

	require.Error(t, err)
	assert.ErrorContains(t, err, "does not match custom schema")
}

// TestValidateAgainstCustomSchema_AdditionalPropertiesMerged verifies that fields stored
// in AdditionalProperties (custom fields from JSON unmarshaling) are merged into the
// validation map before schema validation (lines 297-299 in validation.go).
func TestValidateAgainstCustomSchema_AdditionalPropertiesMerged(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		schema := map[string]interface{}{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"type":    "object",
			"properties": map[string]interface{}{
				"type": map[string]interface{}{
					"type": "string",
				},
				"container": map[string]interface{}{
					"type": "string",
				},
				"customField": map[string]interface{}{
					"type": "string",
				},
				"anotherCustomField": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"type", "container", "customField", "anotherCustomField"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(schema)
	}))
	defer mockServer.Close()

	// AdditionalProperties are set directly (simulating JSON unmarshal of custom fields)
	server := &StdinServerConfig{
		Type:      "mytype",
		Container: "ghcr.io/example/mytype:latest",
		AdditionalProperties: map[string]interface{}{
			"customField":        "custom-value",
			"anotherCustomField": "another-value",
		},
	}

	err := validateAgainstCustomSchema("test-server", server, mockServer.URL, "mcpServers.test-server")

	assert.NoError(t, err, "AdditionalProperties should be merged into validation map")
}

// TestValidateAgainstCustomSchema_AdditionalPropertiesMissingRequired verifies that
// when AdditionalProperties are missing a required custom field, validation fails.
func TestValidateAgainstCustomSchema_AdditionalPropertiesMissingRequired(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		schema := map[string]interface{}{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"type":    "object",
			"properties": map[string]interface{}{
				"type": map[string]interface{}{
					"type": "string",
				},
				"container": map[string]interface{}{
					"type": "string",
				},
				"mandatoryCustomField": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"type", "container", "mandatoryCustomField"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(schema)
	}))
	defer mockServer.Close()

	// AdditionalProperties is populated but missing mandatoryCustomField
	server := &StdinServerConfig{
		Type:      "mytype",
		Container: "ghcr.io/example/mytype:latest",
		AdditionalProperties: map[string]interface{}{
			"someOtherField": "value",
		},
	}

	err := validateAgainstCustomSchema("test-server", server, mockServer.URL, "mcpServers.test-server")

	require.Error(t, err)
	assert.ErrorContains(t, err, "does not match custom schema")
}

// TestValidateCustomServerConfig_NonStringSchemaValue covers the type assertion branch
// (lines 183-187 in validation.go) where the custom schema map value is not a string.
// When the schema value is not a string, schemaURL is set to "" and validation is skipped.
func TestValidateCustomServerConfig_NonStringSchemaValue(t *testing.T) {
	tests := []struct {
		name        string
		schemaValue interface{}
	}{
		{
			name:        "integer_schema_value",
			schemaValue: 42,
		},
		{
			name:        "map_schema_value",
			schemaValue: map[string]interface{}{"url": "https://example.com"},
		},
		{
			name:        "bool_schema_value",
			schemaValue: true,
		},
		{
			name:        "slice_schema_value",
			schemaValue: []string{"https://example.com/schema.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customSchemas := map[string]interface{}{
				"mytype": tt.schemaValue,
			}

			server := &StdinServerConfig{
				Type:      "mytype",
				Container: "ghcr.io/example/mytype:latest",
			}

			// Non-string values cause schemaURL to be "" which skips validation
			err := validateCustomServerConfig("test-server", server, customSchemas, "mcpServers.test-server")

			assert.NoError(t, err, "non-string schema value should skip validation")
		})
	}
}

// TestValidateCustomServerConfig_NilCustomSchemas covers the nil customSchemas path
// (lines 171-174 in validation.go) via validateCustomServerConfig directly.
func TestValidateCustomServerConfig_NilCustomSchemas(t *testing.T) {
	server := &StdinServerConfig{
		Type:      "mytype",
		Container: "ghcr.io/example/mytype:latest",
	}

	err := validateCustomServerConfig("test-server", server, nil, "mcpServers.test-server")

	require.Error(t, err)
	assert.ErrorContains(t, err, "not registered in customSchemas")
}

// TestValidateCustomServerConfig_UnregisteredType covers the case where customSchemas
// is not nil but the server type is not registered (lines 176-180 in validation.go).
func TestValidateCustomServerConfig_UnregisteredType(t *testing.T) {
	customSchemas := map[string]interface{}{
		"othertype": "https://example.com/othertype-schema.json",
	}

	server := &StdinServerConfig{
		Type:      "mytype", // not in customSchemas
		Container: "ghcr.io/example/mytype:latest",
	}

	err := validateCustomServerConfig("test-server", server, customSchemas, "mcpServers.test-server")

	require.Error(t, err)
	assert.ErrorContains(t, err, "not registered in customSchemas")
	assert.ErrorContains(t, err, "mytype")
}

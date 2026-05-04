package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCustomServerTypes tests the compliance requirements for custom server types
// as specified in section 4.1.4 of the MCP Gateway specification.

// T-CFG-010: Valid custom server type with registered schema
func TestTCFG010_ValidCustomTypeWithRegisteredSchema(t *testing.T) {
	// Create a mock HTTP server that returns a valid JSON schema for the custom type
	mockSchemaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		schema := map[string]interface{}{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"type":    "object",
			"properties": map[string]interface{}{
				"type": map[string]interface{}{
					"type": "string",
					"enum": []string{"safeinputs"},
				},
				"customField": map[string]interface{}{
					"type": "string",
				},
				"container": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"type", "customField", "container"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(schema)
	}))
	defer mockSchemaServer.Close()

	// Configuration with custom server type and registered schema
	configJSON := map[string]interface{}{
		"customSchemas": map[string]string{
			"safeinputs": mockSchemaServer.URL,
		},
		"mcpServers": map[string]interface{}{
			"custom-server": map[string]interface{}{
				"type":        "safeinputs",
				"customField": "custom-value",
				"container":   "ghcr.io/example/safeinputs:latest",
			},
		},
	}

	data, err := json.Marshal(configJSON)
	require.NoError(t, err)

	// Parse the configuration
	var stdinCfg StdinConfig
	err = json.Unmarshal(data, &stdinCfg)
	require.NoError(t, err)

	// Custom schemas should be populated
	assert.NotNil(t, stdinCfg.CustomSchemas)
	assert.Equal(t, mockSchemaServer.URL, stdinCfg.CustomSchemas["safeinputs"])

	// Validate the server configuration with custom schemas
	server := stdinCfg.MCPServers["custom-server"]
	require.NotNil(t, server)

	err = validateServerConfigWithCustomSchemas("custom-server", server, stdinCfg.CustomSchemas)
	assert.NoError(t, err, "Valid custom server type with registered schema should pass validation")
}

// T-CFG-011: Reject custom type without schema registration
func TestTCFG011_RejectCustomTypeWithoutRegistration(t *testing.T) {
	// Configuration with unregistered custom server type
	configJSON := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"unregistered-server": map[string]interface{}{
				"type":      "unregistered",
				"container": "ghcr.io/example/unregistered:latest",
			},
		},
	}

	data, err := json.Marshal(configJSON)
	require.NoError(t, err)

	var stdinCfg StdinConfig
	err = json.Unmarshal(data, &stdinCfg)
	require.NoError(t, err)

	server := stdinCfg.MCPServers["unregistered-server"]
	require.NotNil(t, server)

	// Validate should fail for unregistered custom type
	err = validateServerConfigWithCustomSchemas("unregistered-server", server, stdinCfg.CustomSchemas)
	assert.Error(t, err, "Unregistered custom server type should be rejected")
	assert.ErrorContains(t, err, "unregistered")
	assert.ErrorContains(t, err, "not registered in customSchemas")
}

// T-CFG-012: Validate custom configuration against registered schema
func TestTCFG012_ValidateAgainstCustomSchema(t *testing.T) {
	t.Run("valid_custom_config", func(t *testing.T) {
		// Create a mock schema server that requires a specific field
		mockSchemaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			schema := map[string]interface{}{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type":    "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type": "string",
						"enum": []string{"mytype"},
					},
					"requiredField": map[string]interface{}{
						"type": "string",
					},
					"container": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"type", "requiredField", "container"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(schema)
		}))
		defer mockSchemaServer.Close()

		// Valid configuration that matches schema
		configJSON := map[string]interface{}{
			"customSchemas": map[string]string{
				"mytype": mockSchemaServer.URL,
			},
			"mcpServers": map[string]interface{}{
				"valid-custom": map[string]interface{}{
					"type":          "mytype",
					"requiredField": "present",
					"container":     "ghcr.io/example/mytype:latest",
				},
			},
		}

		data, err := json.Marshal(configJSON)
		require.NoError(t, err)

		var stdinCfg StdinConfig
		err = json.Unmarshal(data, &stdinCfg)
		require.NoError(t, err)

		server := stdinCfg.MCPServers["valid-custom"]
		err = validateServerConfigWithCustomSchemas("valid-custom", server, stdinCfg.CustomSchemas)
		assert.NoError(t, err, "Configuration matching custom schema should pass validation")
	})

	t.Run("invalid_custom_config", func(t *testing.T) {
		// Create a mock schema server that requires a specific field
		mockSchemaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			schema := map[string]interface{}{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type":    "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type": "string",
						"enum": []string{"mytype"},
					},
					"requiredField": map[string]interface{}{
						"type": "string",
					},
					"container": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"type", "requiredField", "container"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(schema)
		}))
		defer mockSchemaServer.Close()

		// Invalid configuration that MISSING requiredField
		configJSON := map[string]interface{}{
			"customSchemas": map[string]string{
				"mytype": mockSchemaServer.URL,
			},
			"mcpServers": map[string]interface{}{
				"invalid-custom": map[string]interface{}{
					"type":      "mytype",
					"container": "ghcr.io/example/mytype:latest",
					// Missing requiredField - should fail validation
				},
			},
		}

		data, err := json.Marshal(configJSON)
		require.NoError(t, err)

		var stdinCfg StdinConfig
		err = json.Unmarshal(data, &stdinCfg)
		require.NoError(t, err)

		server := stdinCfg.MCPServers["invalid-custom"]
		err = validateServerConfigWithCustomSchemas("invalid-custom", server, stdinCfg.CustomSchemas)
		assert.Error(t, err, "Configuration missing required fields should fail validation")
		assert.ErrorContains(t, err, "does not match custom schema")
	})

	t.Run("empty_string_skips_validation", func(t *testing.T) {
		// Empty string means skip validation
		configJSON := map[string]interface{}{
			"customSchemas": map[string]string{
				"novalidation": "",
			},
			"mcpServers": map[string]interface{}{
				"no-validation-server": map[string]interface{}{
					"type":      "novalidation",
					"container": "ghcr.io/example/novalidation:latest",
					// No other fields required
				},
			},
		}

		data, err := json.Marshal(configJSON)
		require.NoError(t, err)

		var stdinCfg StdinConfig
		err = json.Unmarshal(data, &stdinCfg)
		require.NoError(t, err)

		server := stdinCfg.MCPServers["no-validation-server"]
		err = validateServerConfigWithCustomSchemas("no-validation-server", server, stdinCfg.CustomSchemas)
		assert.NoError(t, err, "Empty schema URL should skip validation")
	})
}

// T-CFG-013: Reject custom type conflicting with reserved types (stdio/http)
func TestTCFG013_RejectReservedTypeNames(t *testing.T) {
	tests := []struct {
		name         string
		reservedType string
	}{
		{"stdio_conflict", "stdio"},
		{"http_conflict", "http"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Try to register a reserved type name in customSchemas
			configJSON := map[string]interface{}{
				"customSchemas": map[string]string{
					tt.reservedType: "https://example.com/schema.json",
				},
				"mcpServers": map[string]interface{}{
					"test-server": map[string]interface{}{
						"type":      tt.reservedType,
						"container": "ghcr.io/example/test:latest",
					},
				},
			}

			data, err := json.Marshal(configJSON)
			require.NoError(t, err)

			var stdinCfg StdinConfig
			err = json.Unmarshal(data, &stdinCfg)
			require.NoError(t, err)

			// Validation should reject reserved type names in customSchemas
			err = validateCustomSchemas(stdinCfg.CustomSchemas)
			assert.Error(t, err, "Reserved type name %q should be rejected in customSchemas", tt.reservedType)
			assert.ErrorContains(t, err, tt.reservedType)
			assert.ErrorContains(t, err, "reserved")
		})
	}
}

// T-CFG-013b: Reject non-HTTPS custom schema URLs (spec section 4.1.4)
func TestTCFG013b_RejectNonHTTPSSchemaURLs(t *testing.T) {
	tests := []struct {
		name      string
		schemaURL string
	}{
		{"http_url", "http://example.com/schema.json"},
		{"ftp_url", "ftp://example.com/schema.json"},
		{"no_scheme", "example.com/schema.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customSchemas := map[string]interface{}{
				"mytype": tt.schemaURL,
			}

			err := validateCustomSchemas(customSchemas)
			assert.Error(t, err, "Non-HTTPS schema URL %q should be rejected", tt.schemaURL)
			assert.ErrorContains(t, err, "must use HTTPS")
		})
	}
}

// T-CFG-013c: Accept valid HTTPS custom schema URLs (spec section 4.1.4)
func TestTCFG013c_AcceptHTTPSSchemaURLs(t *testing.T) {
	tests := []struct {
		name      string
		schemaURL string
	}{
		{"https_url", "https://example.com/schema.json"},
		{"empty_url", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customSchemas := map[string]interface{}{
				"mytype": tt.schemaURL,
			}

			err := validateCustomSchemas(customSchemas)
			assert.NoError(t, err, "Schema URL %q should be accepted", tt.schemaURL)
		})
	}
}

// T-CFG-014: Custom schema URL fetch and cache
func TestTCFG014_SchemaURLFetchAndCache(t *testing.T) {
	t.Run("empty_string_skips_validation", func(t *testing.T) {
		// Empty string means skip validation
		customSchemas := map[string]interface{}{
			"novalidation": "",
		}

		server := &StdinServerConfig{
			Type:      "novalidation",
			Container: "ghcr.io/example/novalidation:latest",
		}

		err := validateServerConfigWithCustomSchemas("test", server, customSchemas)
		assert.NoError(t, err, "Empty schema URL should skip validation and not fail")
	})

	t.Run("registered_custom_type", func(t *testing.T) {
		mockSchemaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			schema := map[string]interface{}{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type":    "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type": "string",
						"enum": []string{"cached"},
					},
					"container": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"type", "container"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(schema)
		}))
		defer mockSchemaServer.Close()

		customSchemas := map[string]interface{}{
			"cached": mockSchemaServer.URL,
		}

		server := &StdinServerConfig{
			Type:      "cached",
			Container: "ghcr.io/example/cached:latest",
		}

		// Multiple validations should work (caching is implementation detail)
		err1 := validateServerConfigWithCustomSchemas("test1", server, customSchemas)
		assert.NoError(t, err1)

		err2 := validateServerConfigWithCustomSchemas("test2", server, customSchemas)
		assert.NoError(t, err2)
	})
}

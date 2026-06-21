package config

import (
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compileSchemaForTest compiles a JSON schema string using the package's own
// draft-7 compiler.  Tests call this helper to produce a *jsonschema.Schema
// without any network access.
func compileSchemaForTest(t *testing.T, schemaJSON string) *jsonschema.Schema {
	t.Helper()
	const schemaURL = "https://test.example.com/schema.json"
	compiler := newDraft7Compiler()
	err := compiler.AddResource(schemaURL, strings.NewReader(schemaJSON))
	require.NoError(t, err, "AddResource should succeed for valid JSON schema")
	schema, err := compiler.Compile(schemaURL)
	require.NoError(t, err, "Compile should succeed for valid JSON schema")
	return schema
}

// TestValidateServerAgainstSchema_HappyPath verifies that a server config
// matching the schema returns no error.
func TestValidateServerAgainstSchema_HappyPath(t *testing.T) {
	schema := compileSchemaForTest(t, `{
		"type": "object",
		"properties": {
			"type":      {"type": "string"},
			"container": {"type": "string"}
		}
	}`)

	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "ghcr.io/example/server:latest",
	}

	err := validateServerAgainstSchema(
		"test-server", server, schema,
		"https://test.example.com/schema.json",
		"mcpServers.test-server",
	)
	assert.NoError(t, err, "conforming server config should pass schema validation")
}

// TestValidateServerAgainstSchema_ValidationFailure verifies that a server config
// missing a required field returns a SchemaValidationError.
func TestValidateServerAgainstSchema_ValidationFailure(t *testing.T) {
	// Schema requires "custom-required-field" but the server will not supply it.
	schema := compileSchemaForTest(t, `{
		"type": "object",
		"required": ["custom-required-field"],
		"properties": {
			"custom-required-field": {"type": "string"}
		}
	}`)

	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "ghcr.io/example/server:latest",
	}

	err := validateServerAgainstSchema(
		"test-server", server, schema,
		"https://test.example.com/schema.json",
		"mcpServers.test-server",
	)
	require.Error(t, err, "server missing required field should fail schema validation")
	assert.Contains(t, err.Error(), "does not match custom schema",
		"error message should describe the schema mismatch")
	assert.Contains(t, err.Error(), "mcpServers.test-server",
		"error message should include the JSON path")
}

// TestValidateServerAgainstSchema_AdditionalPropertiesMerged verifies that extra
// fields stored in AdditionalProperties are merged into the validation map, allowing
// a schema that requires a custom field to be satisfied.
func TestValidateServerAgainstSchema_AdditionalPropertiesMerged(t *testing.T) {
	// Schema requires a field that only lives in AdditionalProperties.
	schema := compileSchemaForTest(t, `{
		"type": "object",
		"required": ["custom-field"],
		"properties": {
			"custom-field": {"type": "string"}
		}
	}`)

	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "ghcr.io/example/server:latest",
		AdditionalProperties: map[string]interface{}{
			"custom-field": "my-value",
		},
	}

	err := validateServerAgainstSchema(
		"test-server", server, schema,
		"https://test.example.com/schema.json",
		"mcpServers.test-server",
	)
	assert.NoError(t, err,
		"AdditionalProperties fields should be merged and satisfy the schema")
}

// TestValidateServerAgainstSchema_AdditionalPropertiesCauseValidationFailure verifies
// that extra fields in AdditionalProperties can also trigger a validation failure when
// the schema forbids additional properties.
func TestValidateServerAgainstSchema_AdditionalPropertiesCauseValidationFailure(t *testing.T) {
	// Schema explicitly disallows any property beyond the ones listed.
	schema := compileSchemaForTest(t, `{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"type":      {"type": "string"},
			"container": {"type": "string"}
		}
	}`)

	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "ghcr.io/example/server:latest",
		AdditionalProperties: map[string]interface{}{
			"unexpected-field": "some-value",
		},
	}

	err := validateServerAgainstSchema(
		"test-server", server, schema,
		"https://test.example.com/schema.json",
		"mcpServers.test-server",
	)
	require.Error(t, err,
		"AdditionalProperties that violate the schema should cause a validation failure")
	assert.Contains(t, err.Error(), "does not match custom schema",
		"error should describe the schema mismatch")
}

// TestValidateServerAgainstSchema_NilAdditionalProperties verifies that a nil
// AdditionalProperties map is handled safely (the merge loop is skipped).
func TestValidateServerAgainstSchema_NilAdditionalProperties(t *testing.T) {
	schema := compileSchemaForTest(t, `{"type": "object"}`)

	server := &StdinServerConfig{
		Type:                 "stdio",
		Container:            "ghcr.io/example/server:latest",
		AdditionalProperties: nil,
	}

	err := validateServerAgainstSchema(
		"test-server", server, schema,
		"https://test.example.com/schema.json",
		"mcpServers.test-server",
	)
	assert.NoError(t, err,
		"nil AdditionalProperties should not cause a panic or failure for a permissive schema")
}

// TestValidateServerAgainstSchema_EmptyAdditionalProperties verifies that an empty
// (but non-nil) AdditionalProperties map is handled safely.
func TestValidateServerAgainstSchema_EmptyAdditionalProperties(t *testing.T) {
	schema := compileSchemaForTest(t, `{"type": "object"}`)

	server := &StdinServerConfig{
		Type:                 "stdio",
		Container:            "ghcr.io/example/server:latest",
		AdditionalProperties: map[string]interface{}{},
	}

	err := validateServerAgainstSchema(
		"test-server", server, schema,
		"https://test.example.com/schema.json",
		"mcpServers.test-server",
	)
	assert.NoError(t, err,
		"empty AdditionalProperties map should not cause a failure for a permissive schema")
}

// TestValidateServerAgainstSchema_MultipleAdditionalProperties verifies that multiple
// additional properties are all merged and can collectively satisfy a schema with
// several required custom fields.
func TestValidateServerAgainstSchema_MultipleAdditionalProperties(t *testing.T) {
	schema := compileSchemaForTest(t, `{
		"type": "object",
		"required": ["field-a", "field-b"],
		"properties": {
			"field-a": {"type": "string"},
			"field-b": {"type": "number"}
		}
	}`)

	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "ghcr.io/example/server:latest",
		AdditionalProperties: map[string]interface{}{
			"field-a": "alpha",
			"field-b": float64(42),
		},
	}

	err := validateServerAgainstSchema(
		"test-server", server, schema,
		"https://test.example.com/schema.json",
		"mcpServers.test-server",
	)
	assert.NoError(t, err,
		"multiple AdditionalProperties should all be merged and satisfy the schema")
}

// TestValidateServerAgainstSchema_TypeMismatchInAdditionalProperties verifies that
// an additional property with the wrong type triggers a schema validation error.
func TestValidateServerAgainstSchema_TypeMismatchInAdditionalProperties(t *testing.T) {
	schema := compileSchemaForTest(t, `{
		"type": "object",
		"properties": {
			"count": {"type": "number"}
		}
	}`)

	server := &StdinServerConfig{
		Type:      "stdio",
		Container: "ghcr.io/example/server:latest",
		AdditionalProperties: map[string]interface{}{
			"count": "not-a-number",
		},
	}

	err := validateServerAgainstSchema(
		"test-server", server, schema,
		"https://test.example.com/schema.json",
		"mcpServers.test-server",
	)
	require.Error(t, err,
		"wrong type for an additional property should cause a schema validation error")
	assert.Contains(t, err.Error(), "does not match custom schema")
}

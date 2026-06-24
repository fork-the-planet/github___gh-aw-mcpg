package config

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetSchemaState saves and resets the package-level schema caching variables.
// The caller gets a clean sync.Once so getOrCompileSchema runs its Do() callback
// again.  t.Cleanup restores the originals after the test.
func resetSchemaState(t *testing.T) {
	t.Helper()
	origBytes := embeddedSchemaBytes
	origCached := cachedSchema
	origErr := schemaErr
	origDone := origCached != nil || origErr != nil
	t.Cleanup(func() {
		embeddedSchemaBytes = origBytes
		cachedSchema = origCached
		schemaErr = origErr

		// Avoid copying sync.Once (copylocks); restore only whether it had already run.
		schemaOnce = sync.Once{}
		if origDone {
			schemaOnce.Do(func() {})
		}
	})
	schemaOnce = sync.Once{}
	cachedSchema = nil
	schemaErr = nil
}

// --- getOrCompileSchema error-path coverage ---

// TestGetOrCompileSchema_InvalidEmbeddedJSON covers the path where the embedded schema
// bytes are not valid JSON, causing UnmarshalJSON to fail.
func TestGetOrCompileSchema_InvalidEmbeddedJSON(t *testing.T) {
	resetSchemaState(t)
	embeddedSchemaBytes = []byte("{{{ not valid json at all")

	schema, err := getOrCompileSchema()

	assert.Nil(t, schema, "schema should be nil when parsing fails")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to process embedded schema",
		"error message should indicate schema processing failure")
}

// TestGetOrCompileSchema_SchemaWithoutID covers the branch where the schema JSON
// has no "$id" field, causing the fallback to use embeddedSchemaID.
func TestGetOrCompileSchema_SchemaWithoutID(t *testing.T) {
	resetSchemaState(t)
	// Minimal valid JSON Schema with no "$id" field.
	embeddedSchemaBytes = []byte(`{"type":"object"}`)

	schema, err := getOrCompileSchema()

	// The fallback embeddedSchemaID is used; compilation should succeed.
	require.NoError(t, err, "compilation should succeed with fallback $id")
	assert.NotNil(t, schema, "compiled schema should not be nil")
}

// TestGetOrCompileSchema_AddResourceError covers the branch where compiler.AddResource
// fails because the schema's $id is an invalid URL (percent-encoded escape is malformed).
func TestGetOrCompileSchema_AddResourceError(t *testing.T) {
	resetSchemaState(t)
	// "%zz" is an invalid percent-encoding; url.Parse returns an error for it.
	embeddedSchemaBytes = []byte(`{"$id":"http://example.com/bad%zzurl"}`)

	schema, err := getOrCompileSchema()

	assert.Nil(t, schema, "schema should be nil when AddResource fails")
	require.Error(t, err, "error expected when schema has invalid $id URL")
	assert.ErrorContains(t, err, "failed to add schema resource")
}

// TestGetOrCompileSchema_CompileError covers the branch where AddResource succeeds but
// Compile fails because the schema has duplicate canonical URIs in its definitions.
func TestGetOrCompileSchema_CompileError(t *testing.T) {
	resetSchemaState(t)
	// Two definitions share the same $id as the root schema, which causes the
	// jsonschema compiler to report a "same canonical-uri" error at compile time.
	embeddedSchemaBytes = []byte(`{
		"$id": "http://test-compile-error.example.com/schema.json",
		"definitions": {
			"alpha": {"$id": "http://test-compile-error.example.com/schema.json"},
			"beta":  {"$id": "http://test-compile-error.example.com/schema.json"}
		}
	}`)

	schema, err := getOrCompileSchema()

	assert.Nil(t, schema, "schema should be nil when Compile fails")
	require.Error(t, err, "error expected when schema has duplicate canonical URIs")
	assert.ErrorContains(t, err, "failed to compile schema")
}

// --- validateJSONSchema invalid-input coverage ---

// TestValidateJSONSchema_InvalidJSON covers the UnmarshalJSON failure inside
// validateJSONSchema when the supplied config bytes are not valid JSON.
func TestValidateJSONSchema_InvalidJSON(t *testing.T) {
	err := validateJSONSchema([]byte("{{{ this is not json at all"))

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to parse configuration JSON",
		"error message should indicate JSON parsing failure")
}

// TestValidateJSONSchema_SchemaError covers the path where getOrCompileSchema
// itself returns an error, which validateJSONSchema propagates.
func TestValidateJSONSchema_SchemaError(t *testing.T) {
	resetSchemaState(t)
	embeddedSchemaBytes = []byte("{{{ invalid}")

	err := validateJSONSchema([]byte(`{"mcpServers":{}}`))

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to process embedded schema",
		"validateJSONSchema should propagate schema compilation errors")
}

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

// --- fixSchemaBytes branch coverage ---

// TestFixSchemaBytes_TypeFieldNoPattern covers the branch where customServerConfig
// has a "type" property that is a proper object but has no "pattern" key at all.
// (validation_schema.go lines ~120-122)
func TestFixSchemaBytes_TypeFieldNoPattern(t *testing.T) {
	schema := map[string]interface{}{
		"definitions": map[string]interface{}{
			"customServerConfig": map[string]interface{}{
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type": "string", // object but no "pattern" key
					},
				},
			},
		},
	}

	result, err := fixSchemaBytes(marshalSchema(t, schema))

	require.NoError(t, err, "no error expected when type field has no pattern")
	assert.NotEmpty(t, result)
	// "pattern" was never there — structure should be unchanged
	got := unmarshalSchema(t, result)
	defs := got["definitions"].(map[string]interface{})
	csConf := defs["customServerConfig"].(map[string]interface{})
	props := csConf["properties"].(map[string]interface{})
	typeField := props["type"].(map[string]interface{})
	_, hasPattern := typeField["pattern"]
	assert.False(t, hasPattern, "pattern key should still be absent")
}

// TestFixSchemaBytes_TypeFieldPatternNotString covers the branch where the "pattern"
// key exists but its value is not a string (e.g., an integer).
// (validation_schema.go lines ~122-124)
func TestFixSchemaBytes_TypeFieldPatternNotString(t *testing.T) {
	schema := map[string]interface{}{
		"definitions": map[string]interface{}{
			"customServerConfig": map[string]interface{}{
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"pattern": 42, // not a string
					},
				},
			},
		},
	}

	result, err := fixSchemaBytes(marshalSchema(t, schema))

	require.NoError(t, err, "no error expected when type field pattern is not a string")
	assert.NotEmpty(t, result)
	// non-string pattern should be preserved without modification
	got := unmarshalSchema(t, result)
	defs := got["definitions"].(map[string]interface{})
	csConf := defs["customServerConfig"].(map[string]interface{})
	props := csConf["properties"].(map[string]interface{})
	typeField := props["type"].(map[string]interface{})
	assert.InDelta(t, float64(42), typeField["pattern"], 0, "non-string pattern value should be preserved")
}

// TestFixSchemaBytes_TypeFieldPatternNoNegativeLookahead covers the branch where
// the "pattern" is a proper string but does not contain a negative-lookahead "(?!".
// (validation_schema.go lines ~124-126)
func TestFixSchemaBytes_TypeFieldPatternNoNegativeLookahead(t *testing.T) {
	schema := map[string]interface{}{
		"definitions": map[string]interface{}{
			"customServerConfig": map[string]interface{}{
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"pattern": "^[a-z]+$", // no negative lookahead
					},
				},
			},
		},
	}

	result, err := fixSchemaBytes(marshalSchema(t, schema))

	require.NoError(t, err, "no error expected when pattern has no negative lookahead")
	got := unmarshalSchema(t, result)
	defs := got["definitions"].(map[string]interface{})
	csConf := defs["customServerConfig"].(map[string]interface{})
	props := csConf["properties"].(map[string]interface{})
	typeField := props["type"].(map[string]interface{})
	// pattern without negative lookahead must be left unchanged
	assert.Equal(t, "^[a-z]+$", typeField["pattern"],
		"pattern without negative lookahead should be preserved unchanged")
	_, hasNot := typeField["not"]
	assert.False(t, hasNot, "no not constraint should be added when pattern has no negative lookahead")
}

// TestFixSchemaBytes_TypeValueNotObject covers the else-branch reached when the
// value of customServerConfig.properties["type"] is not a map[string]interface{}.
// (validation_schema.go lines ~139-141)
func TestFixSchemaBytes_TypeValueNotObject(t *testing.T) {
	schema := map[string]interface{}{
		"definitions": map[string]interface{}{
			"customServerConfig": map[string]interface{}{
				"properties": map[string]interface{}{
					"type": "not-an-object", // string, not a map
				},
			},
		},
	}

	result, err := fixSchemaBytes(marshalSchema(t, schema))

	require.NoError(t, err, "no error expected when type value is not an object")
	assert.NotEmpty(t, result)
	// the string value should be preserved as-is
	got := unmarshalSchema(t, result)
	defs := got["definitions"].(map[string]interface{})
	csConf := defs["customServerConfig"].(map[string]interface{})
	props := csConf["properties"].(map[string]interface{})
	assert.Equal(t, "not-an-object", props["type"], "non-object type value should be preserved")
}

// --- getOrCompileSchema error-path coverage ---

// TestGetOrCompileSchema_FixSchemaError covers the path where fixSchemaBytes returns
// an error (because embeddedSchemaBytes is malformed JSON).
// (validation_schema.go lines ~369-373)
func TestGetOrCompileSchema_FixSchemaError(t *testing.T) {
	resetSchemaState(t)
	embeddedSchemaBytes = []byte("{{{ not valid json at all")

	schema, err := getOrCompileSchema()

	assert.Nil(t, schema, "schema should be nil when fix fails")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to process embedded schema",
		"error message should indicate schema processing failure")
}

// TestGetOrCompileSchema_SchemaWithoutID covers the branch where the schema JSON
// has no "$id" field, causing the fallback to use embeddedSchemaID.
// (validation_schema.go line ~383-385)
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
// (validation_schema.go lines ~391-394)
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
// (validation_schema.go lines ~397-401)
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

// TestValidateJSONSchema_InvalidJSON covers the json.Unmarshal failure inside
// validateJSONSchema when the supplied config bytes are not valid JSON.
// (validation_schema.go lines ~425-427)
func TestValidateJSONSchema_InvalidJSON(t *testing.T) {
	err := validateJSONSchema([]byte("{{{ this is not json at all"))

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to parse configuration JSON",
		"error message should indicate JSON parsing failure")
}

// TestValidateJSONSchema_SchemaError covers the path where getOrCompileSchema
// itself returns an error, which validateJSONSchema propagates.
// (validation_schema.go lines ~417-419)
func TestValidateJSONSchema_SchemaError(t *testing.T) {
	resetSchemaState(t)
	embeddedSchemaBytes = []byte("{{{ invalid}")

	err := validateJSONSchema([]byte(`{"mcpServers":{}}`))

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to process embedded schema",
		"validateJSONSchema should propagate schema compilation errors")
}

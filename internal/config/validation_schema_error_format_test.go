package config

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/version"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatErrorContext tests the formatErrorContext helper function.
// This function provides additional diagnostic context for JSON Schema validation errors
// based on the error message content.
func TestFormatErrorContext(t *testing.T) {
	tests := []struct {
		name           string
		message        string
		keywordLoc     string
		instanceLoc    string
		prefix         string
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:         "additionalProperties error",
			message:      "additionalProperties 'foo' not allowed",
			prefix:       "  ",
			wantContains: []string{"Configuration contains field(s)", "typos", "  Details:"},
		},
		{
			name:         "additional property error (alternate wording)",
			message:      "additional property 'bar' not allowed",
			prefix:       "",
			wantContains: []string{"Configuration contains field(s)", "typos"},
		},
		{
			name:         "type mismatch with expected and but got",
			message:      "expected integer but got string",
			prefix:       "  ",
			wantContains: []string{"Type mismatch", "correct type", "  Details:"},
		},
		{
			name:         "type mismatch inferred from keyword location",
			message:      "validation failed",
			keywordLoc:   "/properties/mcpServers/additionalProperties/properties/type/type",
			prefix:       "  ",
			wantContains: []string{"Type mismatch", "correct type", "  Details:"},
		},
		{
			name:         "type mismatch with expected and type",
			message:      "expected string, got 'null' type",
			prefix:       "",
			wantContains: []string{"Type mismatch", "correct type"},
		},
		{
			name:         "enum validation error with value must be one of",
			message:      "value must be one of 'a', 'b', 'c'",
			prefix:       "",
			wantContains: []string{"Invalid value", "restricted set", "allowed values"},
		},
		{
			name:         "enum validation error with must be",
			message:      "must be one of the allowed values",
			prefix:       "    ",
			wantContains: []string{"Invalid value", "restricted set"},
		},
		{
			name:         "missing required properties",
			message:      "missing properties 'container'",
			prefix:       "",
			wantContains: []string{"Required field(s) are missing", "Add the required"},
		},
		{
			name:         "required field error",
			message:      "required: apiKey not found",
			prefix:       "  ",
			wantContains: []string{"Required field(s) are missing", "  Details:"},
		},
		{
			name:         "pattern validation failure - does not match pattern",
			message:      "value does not match pattern '^[a-z]+$'",
			prefix:       "",
			wantContains: []string{"Value format is incorrect", "specific format or pattern"},
		},
		{
			name:         "pattern validation failure - pattern keyword",
			message:      "pattern validation failed",
			prefix:       "  ",
			wantContains: []string{"Value format is incorrect", "  Details:"},
		},
		{
			name:         "minimum constraint violation",
			message:      "value must be >= 1",
			prefix:       "",
			wantContains: []string{"outside the allowed range", "Adjust the value"},
		},
		{
			name:         "maximum constraint violation",
			message:      "value must be <= 65535",
			prefix:       "  ",
			wantContains: []string{"outside the allowed range", "  Details:"},
		},
		{
			name:         "minimum keyword in message",
			message:      "minimum: got 0, want 1",
			prefix:       "",
			wantContains: []string{"outside the allowed range"},
		},
		{
			name:         "maximum keyword in message",
			message:      "maximum: got 100000, want 65535",
			prefix:       "",
			wantContains: []string{"outside the allowed range"},
		},
		{
			name:         "oneOf validation error - doesn't validate with any of",
			message:      "doesn't validate with any of the oneOf schemas",
			prefix:       "",
			wantContains: []string{"doesn't match any of the expected formats", "valid configuration types"},
		},
		{
			name:         "oneOf keyword in message",
			message:      "oneOf failed: no schema matches",
			prefix:       "  ",
			wantContains: []string{"doesn't match any of the expected formats"},
		},
		{
			name:         "keyword location different from instance location adds schema location",
			message:      "some error",
			keywordLoc:   "properties/type",
			instanceLoc:  "mcpServers/github/type",
			prefix:       "",
			wantContains: []string{"Schema location: properties/type"},
		},
		{
			name:           "keyword location same as instance location - no schema location line",
			message:        "some error",
			keywordLoc:     "mcpServers/github/type",
			instanceLoc:    "mcpServers/github/type",
			prefix:         "",
			wantNotContain: []string{"Schema location:"},
		},
		{
			name:           "empty keyword location - no schema location line",
			message:        "some error",
			keywordLoc:     "",
			instanceLoc:    "mcpServers/github",
			prefix:         "",
			wantNotContain: []string{"Schema location:"},
		},
		{
			name:           "unrecognized message returns empty string",
			message:        "some unrelated error message",
			prefix:         "",
			wantNotContain: []string{"Details:", "Schema location:"},
		},
		{
			name:         "prefix is prepended to output lines",
			message:      "additionalProperties 'foo' not allowed",
			prefix:       ">>",
			wantContains: []string{">>Details:", ">>  →"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := &jsonschema.ValidationError{
				Message:          tt.message,
				KeywordLocation:  tt.keywordLoc,
				InstanceLocation: tt.instanceLoc,
			}

			result := formatErrorContext(ve, tt.prefix)

			for _, want := range tt.wantContains {
				assert.Contains(t, result, want,
					"formatErrorContext result should contain %q", want)
			}

			for _, notWant := range tt.wantNotContain {
				assert.NotContains(t, result, notWant,
					"formatErrorContext result should not contain %q", notWant)
			}
		})
	}
}

// TestFormatValidationErrorRecursive tests the formatValidationErrorRecursive function
// which formats JSON Schema validation errors with proper indentation for nested errors.
func TestFormatValidationErrorRecursive(t *testing.T) {
	t.Run("root level error with location", func(t *testing.T) {
		var sb strings.Builder
		ve := &jsonschema.ValidationError{
			InstanceLocation: "mcpServers.github",
			Message:          "missing properties 'container'",
		}

		formatValidationErrorRecursive(ve, &sb, 0)

		result := sb.String()
		assert.Contains(t, result, "Location: mcpServers.github")
		assert.Contains(t, result, "Error: missing properties 'container'")
		// Root level (depth=0) adds a trailing newline
		assert.True(t, strings.HasSuffix(result, "\n"), "Root level error should end with newline")
	})

	t.Run("empty location shows root placeholder", func(t *testing.T) {
		var sb strings.Builder
		ve := &jsonschema.ValidationError{
			InstanceLocation: "",
			Message:          "root error",
		}

		formatValidationErrorRecursive(ve, &sb, 0)

		result := sb.String()
		assert.Contains(t, result, "Location: <root>",
			"Empty location should be shown as <root>")
	})

	t.Run("depth 0 adds trailing newline", func(t *testing.T) {
		var sb strings.Builder
		ve := &jsonschema.ValidationError{
			InstanceLocation: "foo",
			Message:          "bar",
		}

		formatValidationErrorRecursive(ve, &sb, 0)
		result := sb.String()
		assert.True(t, strings.HasSuffix(result, "\n"),
			"Depth 0 errors should add trailing newline for spacing between sibling errors")
	})

	t.Run("depth > 0 does not add trailing newline", func(t *testing.T) {
		var sb strings.Builder
		ve := &jsonschema.ValidationError{
			InstanceLocation: "foo",
			Message:          "bar",
		}

		formatValidationErrorRecursive(ve, &sb, 1)
		result := sb.String()
		// At depth > 0, the last line should be Location/Error/Details but not a blank line
		lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
		for _, line := range lines {
			// Each line should have 2-space indentation (depth 1)
			if strings.TrimSpace(line) != "" {
				assert.True(t, strings.HasPrefix(line, "  "),
					"Lines at depth 1 should be indented with 2 spaces, got: %q", line)
			}
		}
	})

	t.Run("error with causes triggers recursion", func(t *testing.T) {
		var sb strings.Builder
		child := &jsonschema.ValidationError{
			InstanceLocation: "child.location",
			Message:          "child error message",
		}
		parent := &jsonschema.ValidationError{
			InstanceLocation: "parent.location",
			Message:          "parent error message",
			Causes:           []*jsonschema.ValidationError{child},
		}

		formatValidationErrorRecursive(parent, &sb, 0)

		result := sb.String()
		assert.Contains(t, result, "parent.location", "Should contain parent location")
		assert.Contains(t, result, "parent error message", "Should contain parent message")
		assert.Contains(t, result, "child.location", "Should contain child location from recursive call")
		assert.Contains(t, result, "child error message", "Should contain child message from recursive call")
	})

	t.Run("nested causes have increased indentation", func(t *testing.T) {
		var sb strings.Builder
		grandchild := &jsonschema.ValidationError{
			InstanceLocation: "gc",
			Message:          "grandchild error",
		}
		child := &jsonschema.ValidationError{
			InstanceLocation: "child",
			Message:          "child error",
			Causes:           []*jsonschema.ValidationError{grandchild},
		}
		parent := &jsonschema.ValidationError{
			InstanceLocation: "parent",
			Message:          "parent error",
			Causes:           []*jsonschema.ValidationError{child},
		}

		formatValidationErrorRecursive(parent, &sb, 0)

		result := sb.String()
		lines := strings.Split(result, "\n")

		// Find child lines (depth 1) and grandchild lines (depth 2)
		childLocationFound := false
		gcLocationFound := false
		for _, line := range lines {
			if strings.Contains(line, "child") && strings.Contains(line, "Location:") {
				if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
					childLocationFound = true
				}
			}
			if strings.Contains(line, "gc") && strings.Contains(line, "Location:") {
				if strings.HasPrefix(line, "    ") {
					gcLocationFound = true
				}
			}
		}
		assert.True(t, childLocationFound, "Child (depth 1) should have 2-space indentation")
		assert.True(t, gcLocationFound, "Grandchild (depth 2) should have 4-space indentation")
	})

	t.Run("multiple root-level causes each get trailing newline behavior from parent", func(t *testing.T) {
		var sb strings.Builder
		child1 := &jsonschema.ValidationError{
			InstanceLocation: "loc1",
			Message:          "error1",
		}
		child2 := &jsonschema.ValidationError{
			InstanceLocation: "loc2",
			Message:          "error2",
		}
		parent := &jsonschema.ValidationError{
			InstanceLocation: "root",
			Message:          "root",
			Causes:           []*jsonschema.ValidationError{child1, child2},
		}

		formatValidationErrorRecursive(parent, &sb, 0)

		result := sb.String()
		assert.Contains(t, result, "loc1")
		assert.Contains(t, result, "loc2")
		assert.Contains(t, result, "error1")
		assert.Contains(t, result, "error2")
	})

	t.Run("context details are included in output", func(t *testing.T) {
		var sb strings.Builder
		ve := &jsonschema.ValidationError{
			InstanceLocation: "gateway.port",
			Message:          "value must be <= 65535",
		}

		formatValidationErrorRecursive(ve, &sb, 0)

		result := sb.String()
		// The error context for min/max violations should be included
		assert.Contains(t, result, "outside the allowed range",
			"formatValidationErrorRecursive should include context from formatErrorContext")
	})
}

// TestFormatSchemaError tests the formatSchemaError function which formats
// JSON Schema validation errors with version information and documentation links.
func TestFormatSchemaError(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		result := formatSchemaError(nil)
		assert.Nil(t, result, "formatSchemaError(nil) should return nil")
	})

	t.Run("jsonschema.ValidationError includes version and location info", func(t *testing.T) {
		version.Set("v1.0.0-test")

		ve := &jsonschema.ValidationError{
			InstanceLocation: "mcpServers.github",
			Message:          "missing properties 'container'",
		}

		result := formatSchemaError(ve)

		require.Error(t, result, "Should return an error")
		errStr := result.Error()
		assert.Contains(t, errStr, "v1.0.0-test", "Should include version")
		assert.Contains(t, errStr, "Configuration validation error", "Should include standard prefix")
		assert.Contains(t, errStr, "Location:", "Should include location")
		assert.Contains(t, errStr, "Error:", "Should include error keyword")
		assert.Contains(t, errStr, "mcpServers.github", "Should include the instance location")
		// Should include documentation footer
		assert.Contains(t, errStr, "https://", "Should include documentation links")
	})

	t.Run("jsonschema.ValidationError includes documentation footer", func(t *testing.T) {
		version.Set("v2.0.0-test")

		ve := &jsonschema.ValidationError{
			InstanceLocation: "gateway.port",
			Message:          "some port error",
		}

		result := formatSchemaError(ve)
		require.Error(t, result)
		errStr := result.Error()
		// The documentation footer is appended via rules.AppendConfigDocsFooter
		assert.Contains(t, errStr, "mcp-gateway", "Should include config spec URL fragment")
	})

	t.Run("non-jsonschema error gets simple format", func(t *testing.T) {
		version.Set("v1.2.3-test")

		regularErr := errors.New("some generic error")
		result := formatSchemaError(regularErr)

		require.Error(t, result, "Should return an error")
		errStr := result.Error()
		assert.Contains(t, errStr, "v1.2.3-test", "Should include version for non-schema errors too")
		assert.Contains(t, errStr, "configuration validation error", "Should include prefix")
		assert.Contains(t, errStr, "some generic error", "Should include original error message")
	})

	t.Run("non-jsonschema fmt.Errorf error gets simple format", func(t *testing.T) {
		version.Set("v3.0.0-test")

		fmtErr := fmt.Errorf("wrapped error: %w", errors.New("inner"))
		result := formatSchemaError(fmtErr)

		require.Error(t, result)
		errStr := result.Error()
		assert.Contains(t, errStr, "v3.0.0-test")
		assert.Contains(t, errStr, "configuration validation error")
		assert.Contains(t, errStr, "wrapped error")
	})

	t.Run("jsonschema.ValidationError with additionalProperties shows details", func(t *testing.T) {
		version.Set("v1.0.0-test")

		ve := &jsonschema.ValidationError{
			InstanceLocation: "mcpServers.github",
			Message:          "additionalProperties 'unknownField' not allowed",
		}

		result := formatSchemaError(ve)
		require.Error(t, result)
		errStr := result.Error()
		// Should include the context details from formatErrorContext
		assert.Contains(t, errStr, "Details:",
			"additionalProperties error should include Details context")
	})

	t.Run("jsonschema.ValidationError with recursive causes includes all levels", func(t *testing.T) {
		version.Set("v1.0.0-test")

		child := &jsonschema.ValidationError{
			InstanceLocation: "mcpServers.github.container",
			Message:          "missing required field",
		}
		parent := &jsonschema.ValidationError{
			InstanceLocation: "mcpServers.github",
			Message:          "parent validation failed",
			Causes:           []*jsonschema.ValidationError{child},
		}

		result := formatSchemaError(parent)
		require.Error(t, result)
		errStr := result.Error()
		assert.Contains(t, errStr, "mcpServers.github")
		assert.Contains(t, errStr, "mcpServers.github.container",
			"Should include child error location from recursive formatting")
		assert.Contains(t, errStr, "missing required field",
			"Should include child error message")
	})
}

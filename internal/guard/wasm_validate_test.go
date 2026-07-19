package guard

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateStringArray(t *testing.T) {
	tests := []struct {
		name            string
		fieldName       string
		raw             interface{}
		requireNonEmpty bool
		wantErr         bool
		wantErrContains string
	}{
		// Branch 1: not an array, requireNonEmpty=true
		{
			name:            "non-array with requireNonEmpty=true returns error",
			fieldName:       "trusted-bots",
			raw:             "not-an-array",
			requireNonEmpty: true,
			wantErr:         true,
			wantErrContains: "expected non-empty array of strings",
		},
		{
			name:            "integer with requireNonEmpty=true returns error",
			fieldName:       "trusted-bots",
			raw:             42,
			requireNonEmpty: true,
			wantErr:         true,
			wantErrContains: "expected non-empty array of strings",
		},
		// Branch 2: not an array, requireNonEmpty=false (previously untested path)
		{
			name:            "non-array with requireNonEmpty=false returns error",
			fieldName:       "blocked-users",
			raw:             "not-an-array",
			requireNonEmpty: false,
			wantErr:         true,
			wantErrContains: "expected array of strings",
		},
		{
			name:            "integer with requireNonEmpty=false returns error",
			fieldName:       "approval-labels",
			raw:             123,
			requireNonEmpty: false,
			wantErr:         true,
			wantErrContains: "expected array of strings",
		},
		{
			name:            "bool with requireNonEmpty=false returns error",
			fieldName:       "reactions",
			raw:             true,
			requireNonEmpty: false,
			wantErr:         true,
			wantErrContains: "expected array of strings",
		},
		{
			name:            "map with requireNonEmpty=false returns error",
			fieldName:       "trusted-users",
			raw:             map[string]interface{}{"key": "value"},
			requireNonEmpty: false,
			wantErr:         true,
			wantErrContains: "expected array of strings",
		},
		// Branch 3: empty array with requireNonEmpty=true
		{
			name:            "empty array with requireNonEmpty=true returns error",
			fieldName:       "trusted-bots",
			raw:             []interface{}{},
			requireNonEmpty: true,
			wantErr:         true,
			wantErrContains: "must be a non-empty array when present",
		},
		// Empty array with requireNonEmpty=false is OK
		{
			name:            "empty array with requireNonEmpty=false succeeds",
			fieldName:       "blocked-users",
			raw:             []interface{}{},
			requireNonEmpty: false,
			wantErr:         false,
		},
		// Branch 4: array contains non-string entry
		{
			name:            "array with non-string entry returns error",
			fieldName:       "trusted-bots",
			raw:             []interface{}{"valid-bot", 42},
			requireNonEmpty: true,
			wantErr:         true,
			wantErrContains: "each entry must be a non-empty string",
		},
		{
			name:            "array with empty string entry returns error",
			fieldName:       "blocked-users",
			raw:             []interface{}{"user1", ""},
			requireNonEmpty: false,
			wantErr:         true,
			wantErrContains: "each entry must be a non-empty string",
		},
		{
			name:            "array with whitespace-only string returns error",
			fieldName:       "approval-labels",
			raw:             []interface{}{"   "},
			requireNonEmpty: false,
			wantErr:         true,
			wantErrContains: "each entry must be a non-empty string",
		},
		{
			name:            "array with nil entry returns error",
			fieldName:       "reactions",
			raw:             []interface{}{nil},
			requireNonEmpty: false,
			wantErr:         true,
			wantErrContains: "each entry must be a non-empty string",
		},
		// Happy path: valid arrays
		{
			name:            "valid single-element array with requireNonEmpty=true succeeds",
			fieldName:       "trusted-bots",
			raw:             []interface{}{"dependabot[bot]"},
			requireNonEmpty: true,
			wantErr:         false,
		},
		{
			name:            "valid multi-element array with requireNonEmpty=true succeeds",
			fieldName:       "trusted-bots",
			raw:             []interface{}{"bot1", "bot2", "bot3"},
			requireNonEmpty: true,
			wantErr:         false,
		},
		{
			name:            "valid single-element array with requireNonEmpty=false succeeds",
			fieldName:       "blocked-users",
			raw:             []interface{}{"bad-actor"},
			requireNonEmpty: false,
			wantErr:         false,
		},
		{
			name:            "valid multi-element array with requireNonEmpty=false succeeds",
			fieldName:       "approval-labels",
			raw:             []interface{}{"approved", "lgtm", "ship-it"},
			requireNonEmpty: false,
			wantErr:         false,
		},
		// Field name appears in error messages
		{
			name:            "field name is included in error for non-array requireNonEmpty=false",
			fieldName:       "my-field",
			raw:             "wrong",
			requireNonEmpty: false,
			wantErr:         true,
			wantErrContains: "my-field",
		},
		{
			name:            "field name is included in error for non-array requireNonEmpty=true",
			fieldName:       "my-field",
			raw:             "wrong",
			requireNonEmpty: true,
			wantErr:         true,
			wantErrContains: "my-field",
		},
		// nil raw value
		{
			name:            "nil raw with requireNonEmpty=false returns error",
			fieldName:       "blocked-users",
			raw:             nil,
			requireNonEmpty: false,
			wantErr:         true,
			wantErrContains: "expected array of strings",
		},
		{
			name:            "nil raw with requireNonEmpty=true returns error",
			fieldName:       "trusted-bots",
			raw:             nil,
			requireNonEmpty: true,
			wantErr:         true,
			wantErrContains: "expected non-empty array of strings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := config.ValidateStringArrayField(tt.fieldName, tt.raw, tt.requireNonEmpty)
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					assert.ErrorContains(t, err, tt.wantErrContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateIntegrityField(t *testing.T) {
	tests := []struct {
		name            string
		fieldName       string
		raw             interface{}
		wantErr         bool
		wantErrContains string
	}{
		// Not a string (untested branch in existing tests)
		{
			name:            "integer returns error",
			fieldName:       "disapproval-integrity",
			raw:             42,
			wantErr:         true,
			wantErrContains: "disapproval-integrity must be one of",
		},
		{
			name:            "bool returns error",
			fieldName:       "endorser-min-integrity",
			raw:             true,
			wantErr:         true,
			wantErrContains: "endorser-min-integrity must be one of",
		},
		{
			name:            "nil returns error",
			fieldName:       "min-integrity",
			raw:             nil,
			wantErr:         true,
			wantErrContains: "min-integrity must be one of",
		},
		{
			name:            "slice returns error",
			fieldName:       "disapproval-integrity",
			raw:             []string{"none"},
			wantErr:         true,
			wantErrContains: "disapproval-integrity must be one of",
		},
		// Invalid string value
		{
			name:            "unknown integrity level returns error",
			fieldName:       "disapproval-integrity",
			raw:             "invalid",
			wantErr:         true,
			wantErrContains: "disapproval-integrity must be one of",
		},
		{
			name:            "empty string returns error",
			fieldName:       "endorser-min-integrity",
			raw:             "",
			wantErr:         true,
			wantErrContains: "endorser-min-integrity must be one of",
		},
		{
			name:            "whitespace-only string returns error",
			fieldName:       "min-integrity",
			raw:             "   ",
			wantErr:         true,
			wantErrContains: "min-integrity must be one of",
		},
		// Valid integrity levels
		{
			name:      "none is valid",
			fieldName: "disapproval-integrity",
			raw:       "none",
			wantErr:   false,
		},
		{
			name:      "unapproved is valid",
			fieldName: "disapproval-integrity",
			raw:       "unapproved",
			wantErr:   false,
		},
		{
			name:      "approved is valid",
			fieldName: "endorser-min-integrity",
			raw:       "approved",
			wantErr:   false,
		},
		{
			name:      "merged is valid",
			fieldName: "min-integrity",
			raw:       "merged",
			wantErr:   false,
		},
		// Case normalization
		{
			name:      "uppercase NONE is valid",
			fieldName: "disapproval-integrity",
			raw:       "NONE",
			wantErr:   false,
		},
		{
			name:      "mixed-case Approved is valid",
			fieldName: "endorser-min-integrity",
			raw:       "Approved",
			wantErr:   false,
		},
		{
			name:      "value with surrounding whitespace is valid",
			fieldName: "disapproval-integrity",
			raw:       "  merged  ",
			wantErr:   false,
		},
		// Error message contains allowed values
		{
			name:            "error message lists valid options",
			fieldName:       "disapproval-integrity",
			raw:             "bad",
			wantErr:         true,
			wantErrContains: "must be one of",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := tt.raw.(string) // non-string inputs normalize to "" and are rejected by ValidateAndNormalizeIntegrityField
			_, err := config.ValidateAndNormalizeIntegrityField(tt.fieldName, s, false)
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					assert.ErrorContains(t, err, tt.wantErrContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

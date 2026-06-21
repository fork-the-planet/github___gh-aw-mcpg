package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnmarshalStringListOrExpression directly tests all branches of the private
// unmarshalStringListOrExpression function, including the two uncovered error paths:
//   - non-string/non-array JSON type (line 289)
//   - string of only delimiter characters with no labels (line 296)
func TestUnmarshalStringListOrExpression(t *testing.T) {
	tests := []struct {
		name    string
		raw     string // JSON literal passed as json.RawMessage
		want    []string
		wantErr string
	}{
		// Happy path: JSON array of strings.
		{
			name: "JSON array of strings",
			raw:  `["label1","label2"]`,
			want: []string{"label1", "label2"},
		},
		// Happy path: empty JSON array.
		{
			name: "empty JSON array",
			raw:  `[]`,
			want: []string{},
		},
		// Happy path: comma-delimited string.
		{
			name: "comma-delimited string",
			raw:  `"label1,label2,label3"`,
			want: []string{"label1", "label2", "label3"},
		},
		// Happy path: newline-delimited string.
		{
			name: "newline-delimited string",
			raw:  `"label1\nlabel2"`,
			want: []string{"label1", "label2"},
		},
		// Happy path: mixed comma and newline delimiters with extra spaces.
		{
			name: "mixed delimiters with whitespace",
			raw:  `"label1, label2\n label3 "`,
			want: []string{"label1", "label2", "label3"},
		},
		// Happy path: single label string (no delimiters).
		{
			name: "single label string",
			raw:  `"unsafe"`,
			want: []string{"unsafe"},
		},
		// Error path (line 289): JSON number — neither array nor string.
		{
			name:    "JSON number is rejected",
			raw:     `123`,
			wantErr: "expected array of strings or comma/newline-delimited expression",
		},
		// Error path (line 289): JSON boolean — neither array nor string.
		{
			name:    "JSON boolean is rejected",
			raw:     `true`,
			wantErr: "expected array of strings or comma/newline-delimited expression",
		},
		// Error path (line 289): JSON object — neither array nor string.
		{
			name:    "JSON object is rejected",
			raw:     `{"key":"value"}`,
			wantErr: "expected array of strings or comma/newline-delimited expression",
		},
		// Error path (line 289): JSON array of integers — fails []string unmarshal,
		// also fails string unmarshal.
		{
			name:    "JSON array of integers is rejected",
			raw:     `[1,2,3]`,
			wantErr: "expected array of strings or comma/newline-delimited expression",
		},
		// Error path (line 296): string consisting of only commas — FieldsFunc
		// returns empty slice because all characters are delimiters.
		{
			name:    "only-comma string has no labels",
			raw:     `","`,
			wantErr: "must include at least one label",
		},
		// Error path (line 296): string of only newlines.
		{
			name:    "only-newline string has no labels",
			raw:     `"\n"`,
			wantErr: "must include at least one label",
		},
		// Error path (line 296): string of multiple commas only.
		{
			name:    "multiple commas only",
			raw:     `",,"`,
			wantErr: "must include at least one label",
		},
		// Error path (line 308): string with spaces around comma — FieldsFunc finds
		// non-empty parts ("  " and "  ") but trimming produces all-empty result.
		{
			name:    "spaces around commas only",
			raw:     `"  ,  "`,
			wantErr: "must include at least one label",
		},
		// Edge case: whitespace-only string — FieldsFunc on non-delimiter characters
		// returns ["   "] (one part), but after TrimSpace it becomes "".
		{
			name:    "whitespace-only string has no labels",
			raw:     `"   "`,
			wantErr: "must include at least one label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(tt.raw)
			got, err := unmarshalStringListOrExpression(raw)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// TestGuardPolicyUnmarshalJSON_NonObjectJSON covers the error path in
// GuardPolicy.UnmarshalJSON when the top-level JSON is not an object.
// This triggers the return at line 100-102 of guard_policy.go.
func TestGuardPolicyUnmarshalJSON_NonObjectJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{
			name:    "JSON number rejected",
			data:    `42`,
			wantErr: "cannot unmarshal",
		},
		{
			name:    "JSON string rejected",
			data:    `"a policy string"`,
			wantErr: "cannot unmarshal",
		},
		{
			name:    "JSON array rejected",
			data:    `["allow-only","write-sink"]`,
			wantErr: "cannot unmarshal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &GuardPolicy{}
			err := json.Unmarshal([]byte(tt.data), p)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// TestGuardPolicyToMap_TypedNilPointer covers the payload == nil guard in
// GuardPolicyToMap (guard_policy.go line 175-177). A typed nil pointer passes
// the interface nil check but marshals to JSON "null", which then unmarshals
// the map variable to nil.
func TestGuardPolicyToMap_TypedNilPointer(t *testing.T) {
	var p *GuardPolicy
	result, err := GuardPolicyToMap(p)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "policy must decode to a JSON object")
}

// TestAllowOnlyPolicyUnmarshalJSON_RefusalLabelsInvalidType covers the error
// path in unmarshalStringListOrExpression (line 289) when refusal-labels has
// an invalid JSON type (number, boolean) that is neither an array nor a string.
func TestAllowOnlyPolicyUnmarshalJSON_RefusalLabelsInvalidType(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name:    "refusal-labels as JSON number",
			json:    `{"repos":"public","min-integrity":"none","refusal-labels":42}`,
			wantErr: "invalid allow-only.refusal-labels: expected array of strings or comma/newline-delimited expression",
		},
		{
			name:    "refusal-labels as JSON boolean",
			json:    `{"repos":"public","min-integrity":"none","refusal-labels":true}`,
			wantErr: "invalid allow-only.refusal-labels: expected array of strings or comma/newline-delimited expression",
		},
		{
			name:    "refusal-labels as JSON object",
			json:    `{"repos":"public","min-integrity":"none","refusal-labels":{"label":"value"}}`,
			wantErr: "invalid allow-only.refusal-labels: expected array of strings or comma/newline-delimited expression",
		},
		// Covers unmarshalStringListOrExpression line 296: string of only commas
		// has zero FieldsFunc parts.
		{
			name:    "refusal-labels delimiter-only string",
			json:    `{"repos":"public","min-integrity":"none","refusal-labels":","}`,
			wantErr: "invalid allow-only.refusal-labels: must include at least one label",
		},
		{
			name:    "refusal-labels newline-only string",
			json:    "{\"repos\":\"public\",\"min-integrity\":\"none\",\"refusal-labels\":\"\\n\"}",
			wantErr: "invalid allow-only.refusal-labels: must include at least one label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &AllowOnlyPolicy{}
			err := json.Unmarshal([]byte(tt.json), p)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

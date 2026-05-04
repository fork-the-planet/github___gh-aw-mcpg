package guard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLabelAgentResponse_Comprehensive(t *testing.T) {
	tests := []struct {
		name        string
		input       []byte
		wantErr     bool
		errContains string
		wantMode    string
	}{
		// ── Happy paths ────────────────────────────────────────────────────────────
		{
			name:     "strict difc_mode",
			input:    []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":"strict"}`),
			wantMode: "strict",
		},
		{
			name:     "filter difc_mode",
			input:    []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":"filter"}`),
			wantMode: "filter",
		},
		{
			name:     "propagate difc_mode",
			input:    []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":"propagate"}`),
			wantMode: "propagate",
		},
		{
			name:     "with populated agent labels",
			input:    []byte(`{"agent":{"secrecy":["public"],"integrity":["approved"]},"difc_mode":"strict"}`),
			wantMode: "strict",
		},
		{
			name:     "with normalized_policy",
			input:    []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":"filter","normalized_policy":{"scope_kind":"public","integrity":"none"}}`),
			wantMode: "filter",
		},
		{
			name:     "success true explicitly set - still proceeds",
			input:    []byte(`{"success":true,"agent":{"secrecy":[],"integrity":[]},"difc_mode":"strict"}`),
			wantMode: "strict",
		},
		{
			name:     "ok true explicitly set - still proceeds",
			input:    []byte(`{"ok":true,"agent":{"secrecy":[],"integrity":[]},"difc_mode":"strict"}`),
			wantMode: "strict",
		},
		{
			name:     "error field present but empty - not treated as error",
			input:    []byte(`{"error":"","agent":{"secrecy":[],"integrity":[]},"difc_mode":"strict"}`),
			wantMode: "strict",
		},
		{
			name:     "error field present but whitespace-only - not treated as error",
			input:    []byte(`{"error":"   ","agent":{"secrecy":[],"integrity":[]},"difc_mode":"strict"}`),
			wantMode: "strict",
		},
		{
			name:     "extra unknown fields ignored",
			input:    []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":"propagate","unknown_field":"value","another":123}`),
			wantMode: "propagate",
		},

		// ── Invalid JSON ────────────────────────────────────────────────────────────
		{
			name:        "invalid JSON - not an object",
			input:       []byte(`not-json`),
			wantErr:     true,
			errContains: "failed to unmarshal label_agent response",
		},
		{
			name:        "invalid JSON - empty input",
			input:       []byte(``),
			wantErr:     true,
			errContains: "failed to unmarshal label_agent response",
		},
		{
			name:        "invalid JSON - array instead of object",
			input:       []byte(`["strict"]`),
			wantErr:     true,
			errContains: "failed to unmarshal label_agent response",
		},
		{
			name:        "invalid JSON - truncated",
			input:       []byte(`{"difc_mode":"strict"`),
			wantErr:     true,
			errContains: "failed to unmarshal label_agent response",
		},

		// ── success: false cases ────────────────────────────────────────────────────
		{
			name:        "success false with error message",
			input:       []byte(`{"success":false,"error":"policy validation failed"}`),
			wantErr:     true,
			errContains: "label_agent rejected policy: policy validation failed",
		},
		{
			name:        "success false without error field",
			input:       []byte(`{"success":false}`),
			wantErr:     true,
			errContains: "label_agent returned non-success status",
		},
		{
			name:        "success false with empty error string",
			input:       []byte(`{"success":false,"error":""}`),
			wantErr:     true,
			errContains: "label_agent returned non-success status",
		},
		{
			name:        "success false with whitespace-only error",
			input:       []byte(`{"success":false,"error":"   "}`),
			wantErr:     true,
			errContains: "label_agent returned non-success status",
		},

		// ── ok: false cases ────────────────────────────────────────────────────────
		{
			name:        "ok false with error message",
			input:       []byte(`{"ok":false,"error":"missing required field"}`),
			wantErr:     true,
			errContains: "label_agent rejected policy: missing required field",
		},
		{
			name:        "ok false without error field",
			input:       []byte(`{"ok":false}`),
			wantErr:     true,
			errContains: "label_agent returned non-success status",
		},
		{
			name:        "ok false with empty error string",
			input:       []byte(`{"ok":false,"error":""}`),
			wantErr:     true,
			errContains: "label_agent returned non-success status",
		},
		{
			name:        "ok false with whitespace-only error",
			input:       []byte(`{"ok":false,"error":"  "}`),
			wantErr:     true,
			errContains: "label_agent returned non-success status",
		},

		// ── Standalone error field (no success/ok field) ────────────────────────────
		{
			name:        "error field with message and no success field",
			input:       []byte(`{"error":"unexpected guard failure"}`),
			wantErr:     true,
			errContains: "label_agent returned error: unexpected guard failure",
		},
		{
			name:        "error field preserves message content",
			input:       []byte(`{"error":"allow-only policy requires scope_kind field"}`),
			wantErr:     true,
			errContains: "allow-only policy requires scope_kind field",
		},

		// ── Missing or invalid difc_mode ───────────────────────────────────────────
		{
			name:        "missing difc_mode field",
			input:       []byte(`{"agent":{"secrecy":[],"integrity":[]}}`),
			wantErr:     true,
			errContains: "label_agent response missing difc_mode",
		},
		{
			name:        "difc_mode is empty string",
			input:       []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":""}`),
			wantErr:     true,
			errContains: "label_agent response missing difc_mode",
		},
		{
			name:        "difc_mode is whitespace only",
			input:       []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":"   "}`),
			wantErr:     true,
			errContains: "label_agent response missing difc_mode",
		},
		{
			name:        "invalid difc_mode value",
			input:       []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":"invalid-mode"}`),
			wantErr:     true,
			errContains: "invalid difc_mode from label_agent",
		},
		{
			// When difc_mode is a number, json.Unmarshal into LabelAgentResult fails
			// because the struct field expects a string type.
			name:        "difc_mode is a number not string",
			input:       []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":42}`),
			wantErr:     true,
			errContains: "failed to decode label_agent response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseLabelAgentResponse(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				if tt.errContains != "" {
					assert.ErrorContains(t, err, tt.errContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				if tt.wantMode != "" {
					assert.Equal(t, tt.wantMode, result.DIFCMode)
				}
			}
		})
	}
}

func TestParseLabelAgentResponse_AgentLabels(t *testing.T) {
	t.Run("parses secrecy and integrity labels", func(t *testing.T) {
		input := []byte(`{"agent":{"secrecy":["private","internal"],"integrity":["approved","merged"]},"difc_mode":"strict"}`)
		result, err := parseLabelAgentResponse(input)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, []string{"private", "internal"}, result.Agent.Secrecy)
		assert.Equal(t, []string{"approved", "merged"}, result.Agent.Integrity)
	})

	t.Run("parses empty agent label arrays", func(t *testing.T) {
		input := []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":"filter"}`)
		result, err := parseLabelAgentResponse(input)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Empty(t, result.Agent.Secrecy)
		assert.Empty(t, result.Agent.Integrity)
	})

	t.Run("parses normalized policy", func(t *testing.T) {
		input := []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":"filter","normalized_policy":{"scope_kind":"public","integrity":"none"}}`)
		result, err := parseLabelAgentResponse(input)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "public", result.NormalizedPolicy["scope_kind"])
		assert.Equal(t, "none", result.NormalizedPolicy["integrity"])
	})

	t.Run("nil normalized_policy when absent", func(t *testing.T) {
		input := []byte(`{"agent":{"secrecy":[],"integrity":[]},"difc_mode":"strict"}`)
		result, err := parseLabelAgentResponse(input)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Nil(t, result.NormalizedPolicy)
	})
}

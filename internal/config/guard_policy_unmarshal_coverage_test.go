package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeScopeKind tests all branches of NormalizeScopeKind.
func TestNormalizeScopeKind(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]interface{}
		want  map[string]interface{}
		isNil bool
	}{
		{
			name:  "nil input returns nil",
			input: nil,
			isNil: true,
		},
		{
			name:  "empty map returns empty map",
			input: map[string]interface{}{},
			want:  map[string]interface{}{},
		},
		{
			name:  "no scope_kind field - other fields preserved",
			input: map[string]interface{}{"min-integrity": "none", "repos": "all"},
			want:  map[string]interface{}{"min-integrity": "none", "repos": "all"},
		},
		{
			name:  "scope_kind already lowercase - unchanged",
			input: map[string]interface{}{"scope_kind": "scoped"},
			want:  map[string]interface{}{"scope_kind": "scoped"},
		},
		{
			name:  "scope_kind uppercase - normalized to lowercase",
			input: map[string]interface{}{"scope_kind": "SCOPED"},
			want:  map[string]interface{}{"scope_kind": "scoped"},
		},
		{
			name:  "scope_kind with leading and trailing spaces - trimmed",
			input: map[string]interface{}{"scope_kind": "  all  "},
			want:  map[string]interface{}{"scope_kind": "all"},
		},
		{
			name:  "scope_kind uppercase with spaces - trimmed and lowercased",
			input: map[string]interface{}{"scope_kind": "  PUBLIC  "},
			want:  map[string]interface{}{"scope_kind": "public"},
		},
		{
			name:  "scope_kind is non-string value - not modified",
			input: map[string]interface{}{"scope_kind": 42},
			want:  map[string]interface{}{"scope_kind": 42},
		},
		{
			name:  "other fields preserved alongside scope_kind",
			input: map[string]interface{}{"scope_kind": "SCOPED", "min-integrity": "approved", "repos": []string{"myorg/*"}},
			want:  map[string]interface{}{"scope_kind": "scoped", "min-integrity": "approved", "repos": []string{"myorg/*"}},
		},
		{
			name:  "does not mutate the original input map",
			input: map[string]interface{}{"scope_kind": "ALL"},
			want:  map[string]interface{}{"scope_kind": "all"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track original value for mutation test
			var originalScopeKind interface{}
			if tt.input != nil {
				originalScopeKind = tt.input["scope_kind"]
			}

			got := NormalizeScopeKind(tt.input)

			if tt.isNil {
				assert.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			assert.Equal(t, tt.want, got)

			// Verify original was not mutated (if had scope_kind)
			if tt.input != nil && originalScopeKind != nil {
				assert.Equal(t, originalScopeKind, tt.input["scope_kind"], "original map must not be mutated")
			}
		})
	}
}

// TestGuardPolicyUnmarshalJSON_InvalidInnerJSON tests error paths when
// allow-only or write-sink inner objects contain invalid JSON.
func TestGuardPolicyUnmarshalJSON_InvalidInnerJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name:    "invalid top-level JSON",
			json:    `{not json}`,
			wantErr: "invalid character",
		},
		{
			name:    "allow-only inner value fails AllowOnlyPolicy unmarshal",
			json:    `{"allow-only": "not an object"}`,
			wantErr: "cannot unmarshal string",
		},
		{
			name:    "write-sink inner value fails WriteSinkPolicy unmarshal",
			json:    `{"write-sink": "not an object"}`,
			wantErr: "cannot unmarshal string",
		},
		{
			name:    "allow-only inner value fails AllowOnlyPolicy unmarshal - missing repos",
			json:    `{"allow-only": {"min-integrity":"none"}}`,
			wantErr: "allow-only must include repos",
		},
		{
			name:    "allow-only inner value fails AllowOnlyPolicy unmarshal - missing min-integrity",
			json:    `{"allow-only": {"repos":"all"}}`,
			wantErr: "allow-only must include min-integrity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &GuardPolicy{}
			err := json.Unmarshal([]byte(tt.json), p)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// TestAllowOnlyPolicyUnmarshalJSON_FieldErrorPaths tests the error path
// for each individual field when the JSON value is invalid.
func TestAllowOnlyPolicyUnmarshalJSON_FieldErrorPaths(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name:    "min-integrity field invalid JSON type",
			json:    `{"repos": "all", "min-integrity": [1,2,3]}`,
			wantErr: "invalid allow-only.min-integrity",
		},
		{
			name:    "blocked-users field invalid JSON type",
			json:    `{"repos": "all", "min-integrity": "none", "blocked-users": "notanarray"}`,
			wantErr: "invalid allow-only.blocked-users",
		},
		{
			name:    "approval-labels field invalid JSON type",
			json:    `{"repos": "all", "min-integrity": "none", "approval-labels": 42}`,
			wantErr: "invalid allow-only.approval-labels",
		},
		{
			name:    "trusted-users field invalid JSON type",
			json:    `{"repos": "all", "min-integrity": "none", "trusted-users": true}`,
			wantErr: "invalid allow-only.trusted-users",
		},
		{
			name:    "endorsement-reactions field invalid JSON type",
			json:    `{"repos": "all", "min-integrity": "none", "endorsement-reactions": "not-an-array"}`,
			wantErr: "invalid allow-only.endorsement-reactions",
		},
		{
			name:    "disapproval-reactions field invalid JSON type",
			json:    `{"repos": "all", "min-integrity": "none", "disapproval-reactions": 123}`,
			wantErr: "invalid allow-only.disapproval-reactions",
		},
		{
			name:    "disapproval-integrity field invalid JSON type",
			json:    `{"repos": "all", "min-integrity": "none", "disapproval-integrity": []}`,
			wantErr: "invalid allow-only.disapproval-integrity",
		},
		{
			name:    "endorser-min-integrity field invalid JSON type",
			json:    `{"repos": "all", "min-integrity": "none", "endorser-min-integrity": {}}`,
			wantErr: "invalid allow-only.endorser-min-integrity",
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

// TestAllowOnlyPolicyUnmarshalJSON_EndorsementDisapprovalFields tests parsing
// of the endorsement-reactions, disapproval-reactions, disapproval-integrity,
// and endorser-min-integrity fields which were not covered by existing tests.
func TestAllowOnlyPolicyUnmarshalJSON_EndorsementDisapprovalFields(t *testing.T) {
	tests := []struct {
		name  string
		json  string
		check func(t *testing.T, p *AllowOnlyPolicy)
	}{
		{
			name: "endorsement-reactions parsed correctly",
			json: `{"repos":"all","min-integrity":"none","endorsement-reactions":["THUMBS_UP","HEART"]}`,
			check: func(t *testing.T, p *AllowOnlyPolicy) {
				assert.Equal(t, []string{"THUMBS_UP", "HEART"}, p.EndorsementReactions)
			},
		},
		{
			name: "disapproval-reactions parsed correctly",
			json: `{"repos":"all","min-integrity":"none","disapproval-reactions":["THUMBS_DOWN","CONFUSED"]}`,
			check: func(t *testing.T, p *AllowOnlyPolicy) {
				assert.Equal(t, []string{"THUMBS_DOWN", "CONFUSED"}, p.DisapprovalReactions)
			},
		},
		{
			name: "disapproval-integrity parsed correctly",
			json: `{"repos":"all","min-integrity":"none","disapproval-integrity":"approved"}`,
			check: func(t *testing.T, p *AllowOnlyPolicy) {
				assert.Equal(t, "approved", p.DisapprovalIntegrity)
			},
		},
		{
			name: "endorser-min-integrity parsed correctly",
			json: `{"repos":"all","min-integrity":"none","endorser-min-integrity":"merged"}`,
			check: func(t *testing.T, p *AllowOnlyPolicy) {
				assert.Equal(t, "merged", p.EndorserMinIntegrity)
			},
		},
		{
			name: "empty endorsement-reactions array is valid",
			json: `{"repos":"all","min-integrity":"none","endorsement-reactions":[]}`,
			check: func(t *testing.T, p *AllowOnlyPolicy) {
				assert.Empty(t, p.EndorsementReactions)
			},
		},
		{
			name: "empty disapproval-reactions array is valid",
			json: `{"repos":"all","min-integrity":"none","disapproval-reactions":[]}`,
			check: func(t *testing.T, p *AllowOnlyPolicy) {
				assert.Empty(t, p.DisapprovalReactions)
			},
		},
		{
			name: "all endorsement and disapproval fields together",
			json: `{
				"repos": "all",
				"min-integrity": "unapproved",
				"endorsement-reactions": ["THUMBS_UP"],
				"disapproval-reactions": ["THUMBS_DOWN"],
				"disapproval-integrity": "none",
				"endorser-min-integrity": "approved"
			}`,
			check: func(t *testing.T, p *AllowOnlyPolicy) {
				assert.Equal(t, []string{"THUMBS_UP"}, p.EndorsementReactions)
				assert.Equal(t, []string{"THUMBS_DOWN"}, p.DisapprovalReactions)
				assert.Equal(t, "none", p.DisapprovalIntegrity)
				assert.Equal(t, "approved", p.EndorserMinIntegrity)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &AllowOnlyPolicy{}
			err := json.Unmarshal([]byte(tt.json), p)
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, p)
			}
		})
	}
}

// TestValidateWriteSinkPolicy_NilInput tests the nil guard in ValidateWriteSinkPolicy.
func TestValidateWriteSinkPolicy_NilInput(t *testing.T) {
	err := ValidateWriteSinkPolicy(nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "write-sink policy must not be nil")
}

// TestNormalizeGuardPolicy_WriteSinkPath tests the write-sink path in NormalizeGuardPolicy
// which returns an error because write-sink policies don't produce a NormalizedGuardPolicy.
func TestNormalizeGuardPolicy_WriteSinkPath(t *testing.T) {
	policy := &GuardPolicy{
		WriteSink: &WriteSinkPolicy{Accept: []string{"*"}},
	}
	result, err := NormalizeGuardPolicy(policy)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "policy must include allow-only")
}

// TestNormalizeGuardPolicy_EndorsementReactionDedup tests that duplicate endorsement
// reactions are deduplicated using uppercase comparison.
func TestNormalizeGuardPolicy_EndorsementReactionDedup(t *testing.T) {
	t.Run("deduplicate endorsement-reactions case-insensitively", func(t *testing.T) {
		policy := &GuardPolicy{AllowOnly: &AllowOnlyPolicy{
			Repos:                "all",
			MinIntegrity:         "none",
			EndorsementReactions: []string{"thumbs_up", "THUMBS_UP", "Thumbs_Up"},
		}}
		result, err := NormalizeGuardPolicy(policy)
		require.NoError(t, err)
		assert.Len(t, result.EndorsementReactions, 1)
		assert.Equal(t, "THUMBS_UP", result.EndorsementReactions[0])
	})

	t.Run("empty endorsement-reactions entry rejected", func(t *testing.T) {
		policy := &GuardPolicy{AllowOnly: &AllowOnlyPolicy{
			Repos:                "all",
			MinIntegrity:         "none",
			EndorsementReactions: []string{"THUMBS_UP", ""},
		}}
		_, err := NormalizeGuardPolicy(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "allow-only.endorsement-reactions entries must not be empty")
	})

	t.Run("deduplicate disapproval-reactions case-insensitively", func(t *testing.T) {
		policy := &GuardPolicy{AllowOnly: &AllowOnlyPolicy{
			Repos:                "all",
			MinIntegrity:         "none",
			DisapprovalReactions: []string{"thumbs_down", "THUMBS_DOWN"},
		}}
		result, err := NormalizeGuardPolicy(policy)
		require.NoError(t, err)
		assert.Len(t, result.DisapprovalReactions, 1)
		assert.Equal(t, "THUMBS_DOWN", result.DisapprovalReactions[0])
	})

	t.Run("empty disapproval-reactions entry rejected", func(t *testing.T) {
		policy := &GuardPolicy{AllowOnly: &AllowOnlyPolicy{
			Repos:                "all",
			MinIntegrity:         "none",
			DisapprovalReactions: []string{""},
		}}
		_, err := NormalizeGuardPolicy(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "allow-only.disapproval-reactions entries must not be empty")
	})

	t.Run("invalid disapproval-integrity rejected", func(t *testing.T) {
		policy := &GuardPolicy{AllowOnly: &AllowOnlyPolicy{
			Repos:                "all",
			MinIntegrity:         "none",
			DisapprovalIntegrity: "invalid-value",
		}}
		_, err := NormalizeGuardPolicy(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "allow-only.disapproval-integrity must be one of")
	})

	t.Run("invalid endorser-min-integrity rejected", func(t *testing.T) {
		policy := &GuardPolicy{AllowOnly: &AllowOnlyPolicy{
			Repos:                "all",
			MinIntegrity:         "none",
			EndorserMinIntegrity: "invalid-level",
		}}
		_, err := NormalizeGuardPolicy(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "allow-only.endorser-min-integrity must be one of")
	})

	t.Run("valid disapproval-integrity normalized to lowercase", func(t *testing.T) {
		policy := &GuardPolicy{AllowOnly: &AllowOnlyPolicy{
			Repos:                "all",
			MinIntegrity:         "none",
			DisapprovalIntegrity: "  APPROVED  ",
		}}
		result, err := NormalizeGuardPolicy(policy)
		require.NoError(t, err)
		assert.Equal(t, "approved", result.DisapprovalIntegrity)
	})

	t.Run("valid endorser-min-integrity normalized to lowercase", func(t *testing.T) {
		policy := &GuardPolicy{AllowOnly: &AllowOnlyPolicy{
			Repos:                "all",
			MinIntegrity:         "none",
			EndorserMinIntegrity: "MERGED",
		}}
		result, err := NormalizeGuardPolicy(policy)
		require.NoError(t, err)
		assert.Equal(t, "merged", result.EndorserMinIntegrity)
	})
}

// TestNormalizeAndValidateScopeArray_NonStringElement tests the error path when
// a scope array element is not a string.
func TestNormalizeAndValidateScopeArray_NonStringElement(t *testing.T) {
	// []interface{} with a non-string element triggers "array values must be strings"
	policy := &GuardPolicy{AllowOnly: &AllowOnlyPolicy{
		Repos:        []interface{}{42},
		MinIntegrity: "none",
	}}
	_, err := NormalizeGuardPolicy(policy)
	require.Error(t, err)
	assert.ErrorContains(t, err, "allow-only.repos array values must be strings")
}

// TestAllowOnlyPolicyUnmarshalJSON_FullRoundTrip tests that a fully populated
// AllowOnlyPolicy round-trips through marshal/unmarshal correctly.
func TestAllowOnlyPolicyUnmarshalJSON_FullRoundTrip(t *testing.T) {
	original := &AllowOnlyPolicy{
		Repos:                []interface{}{"myorg/*", "myorg/repo"},
		MinIntegrity:         "approved",
		BlockedUsers:         []string{"bad-actor"},
		ApprovalLabels:       []string{"approved"},
		TrustedUsers:         []string{"contractor"},
		EndorsementReactions: []string{"THUMBS_UP"},
		DisapprovalReactions: []string{"THUMBS_DOWN"},
		DisapprovalIntegrity: "none",
		EndorserMinIntegrity: "approved",
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	parsed := &AllowOnlyPolicy{}
	err = json.Unmarshal(data, parsed)
	require.NoError(t, err)

	assert.Equal(t, original.Repos, parsed.Repos)
	assert.Equal(t, original.MinIntegrity, parsed.MinIntegrity)
	assert.Equal(t, original.BlockedUsers, parsed.BlockedUsers)
	assert.Equal(t, original.ApprovalLabels, parsed.ApprovalLabels)
	assert.Equal(t, original.TrustedUsers, parsed.TrustedUsers)
	assert.Equal(t, original.EndorsementReactions, parsed.EndorsementReactions)
	assert.Equal(t, original.DisapprovalReactions, parsed.DisapprovalReactions)
	assert.Equal(t, original.DisapprovalIntegrity, parsed.DisapprovalIntegrity)
	assert.Equal(t, original.EndorserMinIntegrity, parsed.EndorserMinIntegrity)
}

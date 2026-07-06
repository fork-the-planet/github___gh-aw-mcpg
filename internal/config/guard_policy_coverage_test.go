package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateWriteSinkPolicy_EmptyAcceptEntry covers the branch where an accept
// entry is empty (or whitespace-only) after trimming.
// (guard_policy_validation.go lines ~44-46)
func TestValidateWriteSinkPolicy_EmptyAcceptEntry(t *testing.T) {
	tests := []struct {
		name   string
		policy *WriteSinkPolicy
	}{
		{
			name:   "empty string entry",
			policy: &WriteSinkPolicy{Accept: []string{"valid-server", ""}},
		},
		{
			name:   "whitespace-only entry",
			policy: &WriteSinkPolicy{Accept: []string{"valid-server", "   "}},
		},
		{
			name:   "tab-only entry",
			policy: &WriteSinkPolicy{Accept: []string{"\t"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWriteSinkPolicy(tt.policy)
			require.Error(t, err)
			assert.ErrorContains(t, err, "is required")
		})
	}
}

// TestNormalizeGuardPolicy_StringSliceReposError covers the case where Repos is a
// []string and normalizeAndValidateScopeArray returns an error (e.g., invalid scope).
// (guard_policy_validation.go lines ~189-192)
func TestNormalizeGuardPolicy_StringSliceReposError(t *testing.T) {
	tests := []struct {
		name        string
		repos       []string
		errContains string
	}{
		{
			name:        "invalid scope no slash",
			repos:       []string{"invalid-no-slash"},
			errContains: "invalid",
		},
		{
			name:        "empty scope string",
			repos:       []string{""},
			errContains: "is required",
		},
		{
			name:        "duplicate scopes",
			repos:       []string{"owner/repo", "owner/repo"},
			errContains: "duplicates",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &GuardPolicy{
				AllowOnly: &AllowOnlyPolicy{
					Repos:        tt.repos, // []string
					MinIntegrity: "none",
				},
			}

			_, err := NormalizeGuardPolicy(policy)

			require.Error(t, err)
			assert.ErrorContains(t, err, tt.errContains)
		})
	}
}

// TestNormalizeToolCallLimits_EmptyKey covers the branch where a tool-name key
// becomes empty after TrimSpace.
// (guard_policy_validation.go lines ~353-355)
func TestNormalizeToolCallLimits_EmptyKey(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]int
	}{
		{
			name:  "empty string key",
			input: map[string]int{"": 5},
		},
		{
			name:  "whitespace-only key",
			input: map[string]int{"   ": 10},
		},
		{
			name:  "tab-only key",
			input: map[string]int{"\t": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizeToolCallLimits(tt.input)

			assert.Nil(t, result)
			require.Error(t, err)
			assert.ErrorContains(t, err, "is required")
		})
	}
}

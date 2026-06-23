package guard

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteSinkGuard_Name(t *testing.T) {
	g := NewWriteSinkGuard([]string{"private:github/gh-aw*"})
	assert.Equal(t, "write-sink", g.Name())
}

func TestWriteSinkGuard_LabelAgent(t *testing.T) {
	g := NewWriteSinkGuard([]string{"private:github/gh-aw*"})
	result, err := g.LabelAgent(context.Background(), nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Agent.Secrecy, "write-sink should not set agent secrecy")
	assert.Empty(t, result.Agent.Integrity, "write-sink should not set agent integrity")
	assert.Equal(t, difc.ModeFilter, result.DIFCMode)
}

func TestWriteSinkGuard_LabelResource_UsesAcceptPatterns(t *testing.T) {
	accept := []string{"private:github/gh-aw*"}
	g := NewWriteSinkGuard(accept)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, resource)

	// Operation must be Write
	assert.Equal(t, difc.OperationWrite, operation)

	// Secrecy must use the configured accept patterns
	secrecyTags := resource.Secrecy.Label.GetTags()
	assert.Len(t, secrecyTags, 1)
	assert.Contains(t, secrecyTags, difc.Tag("private:github/gh-aw*"))

	// Integrity must be empty (no requirements for writes)
	integrityTags := resource.Integrity.Label.GetTags()
	assert.Empty(t, integrityTags, "write-sink resource should have empty integrity")
}

func TestWriteSinkGuard_LabelResource_MultipleAcceptPatterns(t *testing.T) {
	accept := []string{"private:github/gh-aw*", "internal:github/copilot*"}
	g := NewWriteSinkGuard(accept)

	resource, operation, err := g.LabelResource(context.Background(), "noop", nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, resource)

	assert.Equal(t, difc.OperationWrite, operation)
	secrecyTags := resource.Secrecy.Label.GetTags()
	assert.Len(t, secrecyTags, 2)
	assert.Contains(t, secrecyTags, difc.Tag("private:github/gh-aw*"))
	assert.Contains(t, secrecyTags, difc.Tag("internal:github/copilot*"))
}

func TestWriteSinkGuard_LabelResource_EmptyAccept(t *testing.T) {
	g := NewWriteSinkGuard([]string{})

	resource, operation, err := g.LabelResource(context.Background(), "noop", nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, resource)

	assert.Equal(t, difc.OperationWrite, operation)
	assert.Empty(t, resource.Secrecy.Label.GetTags(), "should be empty with no accept patterns")
	assert.Empty(t, resource.Integrity.Label.GetTags())
}

func TestWriteSinkGuard_LabelResponse(t *testing.T) {
	g := NewWriteSinkGuard([]string{"private:github/gh-aw*"})
	data, err := g.LabelResponse(context.Background(), "create_issue", nil, nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, data, "write-sink should not label responses")
}

func TestWriteSinkExtractURLDomainsFromValue(t *testing.T) {
	args := map[string]any{
		"body": "See https://example.com/path and https://EXAMPLE.com/other",
		"references": []any{
			map[string]any{"url": "http://docs.github.com/en"},
		},
	}

	assert.Equal(t, []string{"docs.github.com", "example.com"}, extractURLDomainsFromValue(args))
}

func TestWriteSinkGuard_LabelResource_AuditsURLs(t *testing.T) {
	logDir := t.TempDir()
	logger.InitGatewayLoggers(logDir)
	t.Cleanup(func() {
		logger.SetURLDomainAuditEnabled(false)
		require.NoError(t, logger.CloseAllLoggers())
	})
	logger.SetURLDomainAuditEnabled(true)

	g := NewWriteSinkGuard([]string{"*"})
	resource, operation, err := g.LabelResource(context.Background(), "create_issue", map[string]any{
		"body": "Refs: https://example.com/a https://golang.org/doc",
	}, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, resource)
	assert.Equal(t, difc.OperationWrite, operation)

	content, err := os.ReadFile(filepath.Join(logDir, "observed-url-domains.json"))
	require.NoError(t, err)
	var observed map[string][]string
	require.NoError(t, json.Unmarshal(content, &observed))
	assert.Equal(t, []string{"example.com", "golang.org"}, observed["write-sink"])
}

func TestWriteSinkGuard_WriteEvaluation_Passes(t *testing.T) {
	// End-to-end: simulate the exact DIFC flow that was failing with noop guard.
	// Agent has secrecy from reading a private repo; write-sink accepts it.
	accept := []string{"private:github/gh-aw*"}
	g := NewWriteSinkGuard(accept)

	agentSecrecyTags := []difc.Tag{"private:github/gh-aw*"}
	agentIntegrityTags := []difc.Tag{
		"none:github/gh-aw*",
		"unapproved:github/gh-aw*",
		"approved:github/gh-aw*",
	}

	agentSecrecy := difc.NewSecrecyLabelWithTags(agentSecrecyTags)
	agentIntegrity := difc.NewIntegrityLabelWithTags(agentIntegrityTags)

	// Guard labels the resource using configured accept patterns
	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	// Evaluate with filter mode (same as production)
	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)

	assert.True(t, result.IsAllowed(), "write to sink should be allowed; got: %s", result.Reason)
}

func TestWriteSinkGuard_NoopWouldFail(t *testing.T) {
	// Demonstrate that noop guard would fail in the same scenario
	g := NewNoopGuard()

	agentSecrecyTags := []difc.Tag{"private:github/gh-aw*"}
	agentIntegrityTags := []difc.Tag{
		"none:github/gh-aw*",
		"unapproved:github/gh-aw*",
		"approved:github/gh-aw*",
	}

	agentSecrecy := difc.NewSecrecyLabelWithTags(agentSecrecyTags)
	agentIntegrity := difc.NewIntegrityLabelWithTags(agentIntegrityTags)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)

	assert.False(t, result.IsAllowed(), "noop guard should cause DIFC violation with tainted agent")
	assert.Contains(t, result.Reason, "integrity")
}

func TestWriteSinkGuard_SecrecyMismatchFails(t *testing.T) {
	// If the agent has secrecy tags not covered by the accept patterns, write fails
	accept := []string{"private:github/gh-aw*"}
	g := NewWriteSinkGuard(accept)

	// Agent accessed a different private repo not in accept list
	agentSecrecyTags := []difc.Tag{"private:github/gh-aw*", "private:github/secret-repo"}
	agentSecrecy := difc.NewSecrecyLabelWithTags(agentSecrecyTags)
	agentIntegrity := difc.NewIntegrityLabelWithTags(nil)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)

	assert.False(t, result.IsAllowed(), "write should fail: agent has secrecy tag not in accept list")
}

// =============================================================================
// Write-Sink Accept Rules Tests
//
// These tests verify the mapping between allow-only repos configuration and
// the required write-sink accept values. The rules are documented in
// config.WriteSinkAcceptRules.
//
// The GitHub guard's label_agent produces agent secrecy tags based on the
// repos scope. The write-sink accept must be a superset of those tags for
// writes to succeed:
//
//   repos = "all"              → agent secrecy = []                → accept = ["*"]
//   repos = "public"           → agent secrecy = []                → accept = ["*"]
//   repos = ["O/R"]            → agent secrecy = ["private:O/R"]   → accept = ["private:O/R"]
//   repos = ["O/*"]            → agent secrecy = ["private:O"]     → accept = ["private:O"]
//   repos = ["O/P*"]           → agent secrecy = ["private:O/P*"]  → accept = ["private:O/P*"]
//   repos = ["O/R1", "O/R2"]  → agent secrecy = ["private:O/R1", "private:O/R2"]
//   repos = ["O1/*", "O2/R"]  → agent secrecy = ["private:O1", "private:O2/R"]
// =============================================================================

// TestWriteSinkAcceptRules_ExactRepo tests: repos=["org/repo"] → accept=["private:org/repo"]
func TestWriteSinkAcceptRules_ExactRepo(t *testing.T) {
	accept := []string{"private:github/gh-aw"}
	g := NewWriteSinkGuard(accept)

	// Agent secrecy from label_agent with repos=["github/gh-aw"]
	agentSecrecy := difc.NewSecrecyLabelWithTags([]difc.Tag{"private:github/gh-aw"})
	agentIntegrity := difc.NewIntegrityLabelWithTags(nil)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	assert.True(t, result.IsAllowed(), "exact repo: accept matches agent secrecy; got: %s", result.Reason)
}

// TestWriteSinkAcceptRules_OwnerWildcard tests: repos=["org/*"] → accept=["private:org"]
// The owner wildcard produces a bare owner secrecy tag (no "/*" suffix).
func TestWriteSinkAcceptRules_OwnerWildcard(t *testing.T) {
	// repos=["myorg/*"] produces agent secrecy "private:myorg" (bare owner)
	accept := []string{"private:myorg"}
	g := NewWriteSinkGuard(accept)

	agentSecrecy := difc.NewSecrecyLabelWithTags([]difc.Tag{"private:myorg"})
	agentIntegrity := difc.NewIntegrityLabelWithTags(nil)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	assert.True(t, result.IsAllowed(), "owner wildcard: accept matches agent secrecy; got: %s", result.Reason)
}

// TestWriteSinkAcceptRules_OwnerWildcard_WrongAccept tests that using "private:org/*"
// does NOT match "private:org" — DIFC tags are exact string matches.
func TestWriteSinkAcceptRules_OwnerWildcard_WrongAccept(t *testing.T) {
	// WRONG: accept has "private:myorg/*" but agent secrecy is "private:myorg"
	accept := []string{"private:myorg/*"}
	g := NewWriteSinkGuard(accept)

	agentSecrecy := difc.NewSecrecyLabelWithTags([]difc.Tag{"private:myorg"})
	agentIntegrity := difc.NewIntegrityLabelWithTags(nil)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	assert.False(t, result.IsAllowed(),
		"owner wildcard with wrong accept: 'private:myorg/*' != 'private:myorg'")
}

// TestWriteSinkAcceptRules_PrefixWildcard tests: repos=["org/prefix*"] → accept=["private:org/prefix*"]
func TestWriteSinkAcceptRules_PrefixWildcard(t *testing.T) {
	accept := []string{"private:github/gh-aw*"}
	g := NewWriteSinkGuard(accept)

	// Agent secrecy from label_agent with repos=["github/gh-aw*"]
	agentSecrecy := difc.NewSecrecyLabelWithTags([]difc.Tag{"private:github/gh-aw*"})
	agentIntegrity := difc.NewIntegrityLabelWithTags(nil)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	assert.True(t, result.IsAllowed(), "prefix wildcard: accept matches agent secrecy; got: %s", result.Reason)
}

// TestWriteSinkAcceptRules_MultipleExactRepos tests: repos=["O/R1", "O/R2"] → accept=["private:O/R1", "private:O/R2"]
func TestWriteSinkAcceptRules_MultipleExactRepos(t *testing.T) {
	accept := []string{"private:github/repo1", "private:github/repo2"}
	g := NewWriteSinkGuard(accept)

	// Agent secrecy from label_agent with repos=["github/repo1", "github/repo2"]
	agentSecrecy := difc.NewSecrecyLabelWithTags([]difc.Tag{
		"private:github/repo1",
		"private:github/repo2",
	})
	agentIntegrity := difc.NewIntegrityLabelWithTags(nil)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	assert.True(t, result.IsAllowed(), "multiple exact repos: accept covers all agent secrecy; got: %s", result.Reason)
}

// TestWriteSinkAcceptRules_MixedScopes tests: repos=["O1/*", "O2/repo"] → accept=["private:O1", "private:O2/repo"]
func TestWriteSinkAcceptRules_MixedScopes(t *testing.T) {
	accept := []string{"private:myorg", "private:partner/shared-lib"}
	g := NewWriteSinkGuard(accept)

	// Agent secrecy from label_agent with repos=["myorg/*", "partner/shared-lib"]
	agentSecrecy := difc.NewSecrecyLabelWithTags([]difc.Tag{
		"private:myorg",
		"private:partner/shared-lib",
	})
	agentIntegrity := difc.NewIntegrityLabelWithTags(nil)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	assert.True(t, result.IsAllowed(), "mixed scopes: accept covers all agent secrecy; got: %s", result.Reason)
}

// TestWriteSinkAcceptRules_SupersetAcceptAllowed tests that accept can be a strict
// superset of the agent's secrecy — extra entries are harmless.
func TestWriteSinkAcceptRules_SupersetAcceptAllowed(t *testing.T) {
	// Accept covers MORE than the agent has — should still work
	accept := []string{"private:github/gh-aw*", "private:github/copilot*", "private:extra"}
	g := NewWriteSinkGuard(accept)

	// Agent only has one secrecy tag
	agentSecrecy := difc.NewSecrecyLabelWithTags([]difc.Tag{"private:github/gh-aw*"})
	agentIntegrity := difc.NewIntegrityLabelWithTags(nil)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	assert.True(t, result.IsAllowed(), "superset accept: extra entries are harmless; got: %s", result.Reason)
}

// TestWriteSinkAcceptRules_EmptyAgentSecrecy tests that an agent with no secrecy
// (repos="all" or repos="public") can write through a wildcard accept sink.
func TestWriteSinkAcceptRules_EmptyAgentSecrecy(t *testing.T) {
	// Wildcard accept: agent has no secrecy, write passes
	g := NewWriteSinkGuard([]string{"*"})

	agentSecrecy := difc.NewSecrecyLabelWithTags(nil) // repos="all" or repos="public"
	agentIntegrity := difc.NewIntegrityLabelWithTags(nil)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	assert.True(t, result.IsAllowed(), "empty agent secrecy with wildcard accept: always passes; got: %s", result.Reason)
}

// TestWriteSinkAcceptRules_PartialAcceptFails tests that if accept covers only SOME
// of the agent's secrecy tags, the write is blocked.
func TestWriteSinkAcceptRules_PartialAcceptFails(t *testing.T) {
	// Accept only covers one of two agent secrecy tags
	accept := []string{"private:github/repo1"}
	g := NewWriteSinkGuard(accept)

	// Agent has two secrecy tags from repos=["github/repo1", "github/repo2"]
	agentSecrecy := difc.NewSecrecyLabelWithTags([]difc.Tag{
		"private:github/repo1",
		"private:github/repo2",
	})
	agentIntegrity := difc.NewIntegrityLabelWithTags(nil)

	resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	assert.False(t, result.IsAllowed(),
		"partial accept: 'private:github/repo2' not covered → write blocked")
}

// TestWriteSinkAcceptRules_AllScopeTypes is a comprehensive table-driven test
// covering all possible allow-only repos → write-sink accept mappings.
func TestWriteSinkAcceptRules_AllScopeTypes(t *testing.T) {
	tests := []struct {
		name          string
		repos         string // human description of the repos config
		agentSecrecy  []difc.Tag
		accept        []string
		expectAllowed bool
	}{
		{
			name:          "repos=all → wildcard accept",
			repos:         `"all"`,
			agentSecrecy:  nil,
			accept:        []string{"*"},
			expectAllowed: true,
		},
		{
			name:          "repos=public → wildcard accept",
			repos:         `"public"`,
			agentSecrecy:  nil,
			accept:        []string{"*"},
			expectAllowed: true,
		},
		{
			name:          "repos=all → wildcard accept with tainted agent",
			repos:         `"all" (agent tainted by other guard)`,
			agentSecrecy:  []difc.Tag{"private:org/repo"},
			accept:        []string{"*"},
			expectAllowed: true,
		},
		{
			name:          "repos=[org/repo] → accept=[private:org/repo]",
			repos:         `["github/gh-aw"]`,
			agentSecrecy:  []difc.Tag{"private:github/gh-aw"},
			accept:        []string{"private:github/gh-aw"},
			expectAllowed: true,
		},
		{
			name:          "repos=[org/*] → accept=[private:org]",
			repos:         `["myorg/*"]`,
			agentSecrecy:  []difc.Tag{"private:myorg"},
			accept:        []string{"private:myorg"},
			expectAllowed: true,
		},
		{
			name:          "repos=[org/prefix*] → accept=[private:org/prefix*]",
			repos:         `["github/gh-aw*"]`,
			agentSecrecy:  []difc.Tag{"private:github/gh-aw*"},
			accept:        []string{"private:github/gh-aw*"},
			expectAllowed: true,
		},
		{
			name:          "repos=[O/R1, O/R2] → accept=[private:O/R1, private:O/R2]",
			repos:         `["github/repo1", "github/repo2"]`,
			agentSecrecy:  []difc.Tag{"private:github/repo1", "private:github/repo2"},
			accept:        []string{"private:github/repo1", "private:github/repo2"},
			expectAllowed: true,
		},
		{
			name:          "repos=[O1/*, O2/R] → accept=[private:O1, private:O2/R]",
			repos:         `["myorg/*", "partner/lib"]`,
			agentSecrecy:  []difc.Tag{"private:myorg", "private:partner/lib"},
			accept:        []string{"private:myorg", "private:partner/lib"},
			expectAllowed: true,
		},
		{
			name:          "repos=[O/P*, O/R] → accept=[private:O/P*, private:O/R]",
			repos:         `["github/gh-aw*", "github/copilot"]`,
			agentSecrecy:  []difc.Tag{"private:github/gh-aw*", "private:github/copilot"},
			accept:        []string{"private:github/gh-aw*", "private:github/copilot"},
			expectAllowed: true,
		},
		// Failure cases
		{
			name:          "FAIL: repos=[org/*] with wrong accept format org/*",
			repos:         `["myorg/*"]`,
			agentSecrecy:  []difc.Tag{"private:myorg"},
			accept:        []string{"private:myorg/*"}, // WRONG: should be "private:myorg"
			expectAllowed: false,
		},
		{
			name:          "FAIL: repos=[O/R1, O/R2] with partial accept",
			repos:         `["github/repo1", "github/repo2"]`,
			agentSecrecy:  []difc.Tag{"private:github/repo1", "private:github/repo2"},
			accept:        []string{"private:github/repo1"}, // missing repo2
			expectAllowed: false,
		},
		{
			name:          "FAIL: repos=[org/repo] with different repo in accept",
			repos:         `["github/gh-aw"]`,
			agentSecrecy:  []difc.Tag{"private:github/gh-aw"},
			accept:        []string{"private:github/other-repo"},
			expectAllowed: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWriteSinkGuard(tc.accept)

			agentSecrecy := difc.NewSecrecyLabelWithTags(tc.agentSecrecy)
			agentIntegrity := difc.NewIntegrityLabelWithTags(nil)

			resource, operation, err := g.LabelResource(context.Background(), "create_issue", nil, nil, nil)
			require.NoError(t, err)

			evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
			result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)

			if tc.expectAllowed {
				assert.True(t, result.IsAllowed(),
					"repos=%s: write should be allowed; got: %s", tc.repos, result.Reason)
			} else {
				assert.False(t, result.IsAllowed(),
					"repos=%s: write should be blocked", tc.repos)
			}
		})
	}
}

// TestWriteSinkGuard_WildcardAccept_WithIntegrity tests the key scenario:
// agent has integrity tags from GitHub guard + wildcard accept.
// This is the primary use case — write-sink with accept=["*"] prevents
// the noop guard integrity violation for repos="all"/"public".
func TestWriteSinkGuard_WildcardAccept_WithIntegrity(t *testing.T) {
	g := NewWriteSinkGuard([]string{"*"})

	// Agent has integrity from GitHub guard (repos="all" still gets integrity)
	agentSecrecy := difc.NewSecrecyLabelWithTags(nil)
	agentIntegrity := difc.NewIntegrityLabelWithTags([]difc.Tag{
		"none:*",
		"unapproved:*",
		"approved:*",
	})

	resource, operation, err := g.LabelResource(context.Background(), "safe_output", nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, difc.OperationWrite, operation)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	assert.True(t, result.IsAllowed(),
		"wildcard accept + empty agent secrecy + agent integrity: write should pass; got: %s", result.Reason)
}

// TestWriteSinkGuard_WildcardAccept_TaintedAgent tests that wildcard accept
// allows writes even from agents tainted with secrecy from another guard.
func TestWriteSinkGuard_WildcardAccept_TaintedAgent(t *testing.T) {
	g := NewWriteSinkGuard([]string{"*"})

	// Agent tainted with secrecy from some other source
	agentSecrecy := difc.NewSecrecyLabelWithTags([]difc.Tag{
		"private:github/secret-repo",
		"private:other-org/internal",
	})
	agentIntegrity := difc.NewIntegrityLabelWithTags([]difc.Tag{
		"approved:github/secret-repo",
	})

	resource, operation, err := g.LabelResource(context.Background(), "safe_output", nil, nil, nil)
	require.NoError(t, err)

	evaluator := difc.NewEvaluatorWithMode(difc.EnforcementFilter)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	assert.True(t, result.IsAllowed(),
		"wildcard accept should allow writes from any tainted agent; got: %s", result.Reason)
}

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configurableGuard is a test double for guard.Guard that lets each test
// configure the return values of LabelAgent.  It is distinct from the
// mockGuard defined in require_guard_policy_test.go.
type configurableGuard struct {
	name             string
	labelAgentResult *guard.LabelAgentResult
	labelAgentErr    error
}

func (g *configurableGuard) Name() string { return g.name }

func (g *configurableGuard) LabelAgent(_ context.Context, _ interface{}, _ guard.BackendCaller, _ *difc.Capabilities) (*guard.LabelAgentResult, error) {
	return g.labelAgentResult, g.labelAgentErr
}

func (g *configurableGuard) LabelResource(_ context.Context, _ string, _ interface{}, _ guard.BackendCaller, _ *difc.Capabilities) (*difc.LabeledResource, difc.OperationType, error) {
	return difc.NewLabeledResource("test"), difc.OperationRead, nil
}

func (g *configurableGuard) LabelResponse(_ context.Context, _ string, _ interface{}, _ guard.BackendCaller, _ *difc.Capabilities) (difc.LabeledData, error) {
	return nil, nil
}

// countGuard wraps a configurableGuard and counts how many times LabelAgent is called.
type countGuard struct {
	inner    *configurableGuard
	callsPtr *int
}

func (g *countGuard) Name() string { return g.inner.name }

func (g *countGuard) LabelAgent(ctx context.Context, policy interface{}, backend guard.BackendCaller, caps *difc.Capabilities) (*guard.LabelAgentResult, error) {
	*g.callsPtr++
	return g.inner.LabelAgent(ctx, policy, backend, caps)
}

func (g *countGuard) LabelResource(ctx context.Context, toolName string, args interface{}, backend guard.BackendCaller, caps *difc.Capabilities) (*difc.LabeledResource, difc.OperationType, error) {
	return g.inner.LabelResource(ctx, toolName, args, backend, caps)
}

func (g *countGuard) LabelResponse(ctx context.Context, toolName string, result interface{}, backend guard.BackendCaller, caps *difc.Capabilities) (difc.LabeledData, error) {
	return g.inner.LabelResponse(ctx, toolName, result, backend, caps)
}

// noopBackendCaller satisfies guard.BackendCaller without making real calls.
type noopBackendCaller struct{}

func (n *noopBackendCaller) CallTool(_ context.Context, _ string, _ interface{}) (interface{}, error) {
	return nil, nil
}

// newMinimalUnifiedServer creates a bare UnifiedServer with the fields needed to call
// ensureGuardInitialized and normalizeScopeKind directly, without starting backend
// servers or an MCP SDK server.
func newMinimalUnifiedServer(cfg *config.Config) *UnifiedServer {
	difcMode := difc.EnforcementStrict
	if cfg != nil && cfg.DIFCMode != "" {
		if m, err := difc.ParseEnforcementMode(cfg.DIFCMode); err == nil {
			difcMode = m
		}
	}
	return &UnifiedServer{
		cfg:           cfg,
		sessions:      make(map[string]*Session),
		agentRegistry: difc.NewAgentRegistryWithDefaults(nil, nil),
		capabilities:  difc.NewCapabilities(),
		evaluator:     difc.NewEvaluatorWithMode(difcMode),
	}
}

// validAllowOnlyPolicy returns a minimal GuardPolicy that passes ValidateGuardPolicy.
func validAllowOnlyPolicy() *config.GuardPolicy {
	return &config.GuardPolicy{
		AllowOnly: &config.AllowOnlyPolicy{
			Repos:        "public",
			MinIntegrity: config.IntegrityNone,
		},
	}
}

// ─── normalizeScopeKind ───────────────────────────────────────────────────────

func TestNormalizeScopeKind_Nil(t *testing.T) {
	result := normalizeScopeKind(nil)
	assert.Nil(t, result)
}

func TestNormalizeScopeKind_EmptyMap(t *testing.T) {
	result := normalizeScopeKind(map[string]interface{}{})
	require.NotNil(t, result)
	assert.Empty(t, result)
}

func TestNormalizeScopeKind_NoScopeKindField(t *testing.T) {
	input := map[string]interface{}{
		"min-integrity": "none",
		"repos":         "public",
	}
	result := normalizeScopeKind(input)
	require.NotNil(t, result)
	assert.Equal(t, "none", result["min-integrity"])
	assert.Equal(t, "public", result["repos"])
	assert.NotContains(t, result, "scope_kind")
}

func TestNormalizeScopeKind_ScopeKindAlreadyLowercase(t *testing.T) {
	input := map[string]interface{}{
		"scope_kind": "composite",
	}
	result := normalizeScopeKind(input)
	assert.Equal(t, "composite", result["scope_kind"])
}

func TestNormalizeScopeKind_ScopeKindUppercase(t *testing.T) {
	input := map[string]interface{}{
		"scope_kind": "COMPOSITE",
	}
	result := normalizeScopeKind(input)
	assert.Equal(t, "composite", result["scope_kind"])
}

func TestNormalizeScopeKind_ScopeKindMixedCaseWithWhitespace(t *testing.T) {
	input := map[string]interface{}{
		"scope_kind": "  Scoped  ",
	}
	result := normalizeScopeKind(input)
	assert.Equal(t, "scoped", result["scope_kind"])
}

func TestNormalizeScopeKind_NonStringScopeKind(t *testing.T) {
	// Non-string values must be preserved as-is (the type assertion fails silently).
	input := map[string]interface{}{
		"scope_kind": 42,
		"other":      "value",
	}
	result := normalizeScopeKind(input)
	assert.Equal(t, 42, result["scope_kind"])
	assert.Equal(t, "value", result["other"])
}

func TestNormalizeScopeKind_DoesNotMutateInput(t *testing.T) {
	input := map[string]interface{}{
		"scope_kind": "Composite",
	}
	original := input["scope_kind"]
	normalizeScopeKind(input)
	// The original map must not be modified — normalizeScopeKind returns a copy.
	assert.Equal(t, original, input["scope_kind"])
}

// ─── ensureGuardInitialized ───────────────────────────────────────────────────

// TestEnsureGuardInitialized_PolicyNil checks that when resolveGuardPolicy returns nil
// (no guard policy configured for the server) the evaluator default mode is returned.
func TestEnsureGuardInitialized_PolicyNil(t *testing.T) {
	cfg := &config.Config{
		DIFCMode: "strict",
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "stdio"},
		},
	}
	us := newMinimalUnifiedServer(cfg)
	g := &configurableGuard{name: "test-guard"}

	mode, err := us.ensureGuardInitialized(context.Background(), "session-1", "github", g, &noopBackendCaller{})

	require.NoError(t, err)
	assert.Equal(t, difc.EnforcementStrict, mode,
		"should use evaluator default mode when no policy is configured")
}

// TestEnsureGuardInitialized_PolicyResolveError checks the error path when
// resolveGuardPolicy fails because the configured GuardPolicy is invalid.
func TestEnsureGuardInitialized_PolicyResolveError(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy: &config.GuardPolicy{
			AllowOnly: &config.AllowOnlyPolicy{
				Repos:        "public",
				MinIntegrity: "INVALID_INTEGRITY_VALUE",
			},
		},
	}
	us := newMinimalUnifiedServer(cfg)
	g := &configurableGuard{name: "test-guard"}

	_, err := us.ensureGuardInitialized(context.Background(), "session-1", "server1", g, &noopBackendCaller{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve guard policy")
}

// TestEnsureGuardInitialized_CacheHit verifies that when the session already has a
// valid initialized state with the same policy hash, LabelAgent is NOT called again
// and the cached mode is returned.
func TestEnsureGuardInitialized_CacheHit(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)

	callCount := 0
	g := &countGuard{
		inner: &configurableGuard{
			name: "counting-guard",
			labelAgentResult: &guard.LabelAgentResult{
				Agent:    guard.AgentLabelsPayload{Secrecy: []string{"sec"}, Integrity: []string{"int"}},
				DIFCMode: "filter",
			},
		},
		callsPtr: &callCount,
	}

	ctx := guard.SetAgentIDInContext(context.Background(), "agent-cache")

	// First call: populates the cache.
	mode1, err := us.ensureGuardInitialized(ctx, "session-cache", "backend", g, &noopBackendCaller{})
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "LabelAgent should be called on first initialization")
	assert.Equal(t, difc.EnforcementFilter, mode1)

	// Second call with the same session/server/policy: must use the cache.
	mode2, err := us.ensureGuardInitialized(ctx, "session-cache", "backend", g, &noopBackendCaller{})
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "LabelAgent must NOT be called on a cache hit")
	assert.Equal(t, difc.EnforcementFilter, mode2)
}

// TestEnsureGuardInitialized_LabelAgentError checks that an error from LabelAgent
// propagates correctly.
func TestEnsureGuardInitialized_LabelAgentError(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)
	g := &configurableGuard{
		name:          "erroring-guard",
		labelAgentErr: errors.New("backend unreachable"),
	}

	_, err := us.ensureGuardInitialized(context.Background(), "session-err", "server1", g, &noopBackendCaller{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "label_agent failed")
	assert.Contains(t, err.Error(), "backend unreachable")
}

// TestEnsureGuardInitialized_LabelAgentNilResult checks the nil-result guard branch.
func TestEnsureGuardInitialized_LabelAgentNilResult(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)
	g := &configurableGuard{
		name:             "nil-result-guard",
		labelAgentResult: nil,
		labelAgentErr:    nil,
	}

	_, err := us.ensureGuardInitialized(context.Background(), "session-nil", "server1", g, &noopBackendCaller{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "label_agent returned nil result")
}

// TestEnsureGuardInitialized_DIFCModeEmpty verifies that when LabelAgent returns an
// empty DIFCMode the evaluator's default mode is used.
func TestEnsureGuardInitialized_DIFCModeEmpty(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "filter", // evaluator default
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)
	g := &configurableGuard{
		name: "empty-mode-guard",
		labelAgentResult: &guard.LabelAgentResult{
			DIFCMode: "", // empty → use evaluator default
		},
	}

	ctx := guard.SetAgentIDInContext(context.Background(), "agent-empty-mode")
	mode, err := us.ensureGuardInitialized(ctx, "session-empty", "server1", g, &noopBackendCaller{})

	require.NoError(t, err)
	assert.Equal(t, difc.EnforcementFilter, mode,
		"empty DIFCMode should fall back to evaluator default")
}

// TestEnsureGuardInitialized_DIFCModeValid verifies that a valid non-empty DIFCMode
// from LabelAgent overrides the evaluator default.
func TestEnsureGuardInitialized_DIFCModeValid(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict", // evaluator default
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)
	g := &configurableGuard{
		name: "filter-mode-guard",
		labelAgentResult: &guard.LabelAgentResult{
			DIFCMode: "filter",
		},
	}

	ctx := guard.SetAgentIDInContext(context.Background(), "agent-valid-mode")
	mode, err := us.ensureGuardInitialized(ctx, "session-valid", "server1", g, &noopBackendCaller{})

	require.NoError(t, err)
	assert.Equal(t, difc.EnforcementFilter, mode)
}

// TestEnsureGuardInitialized_DIFCModeInvalid verifies that an unrecognized DIFCMode
// string from LabelAgent causes an error.
func TestEnsureGuardInitialized_DIFCModeInvalid(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)
	g := &configurableGuard{
		name: "invalid-mode-guard",
		labelAgentResult: &guard.LabelAgentResult{
			DIFCMode: "invalid-mode-xyz",
		},
	}

	_, err := us.ensureGuardInitialized(context.Background(), "session-inv", "server1", g, &noopBackendCaller{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid difc_mode from label_agent")
}

// TestEnsureGuardInitialized_NewSessionCreated verifies that when no session exists yet
// a new one is created and stored with the correct state.
func TestEnsureGuardInitialized_NewSessionCreated(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)
	g := &configurableGuard{
		name: "session-creator-guard",
		labelAgentResult: &guard.LabelAgentResult{
			DIFCMode: "strict",
		},
	}

	ctx := guard.SetAgentIDInContext(context.Background(), "agent-new-session")
	mode, err := us.ensureGuardInitialized(ctx, "brand-new-session", "server1", g, &noopBackendCaller{})

	require.NoError(t, err)
	assert.Equal(t, difc.EnforcementStrict, mode)

	us.sessionMu.RLock()
	session := us.sessions["brand-new-session"]
	us.sessionMu.RUnlock()

	require.NotNil(t, session, "session should have been created")
	require.NotNil(t, session.GuardInit, "GuardInit should be initialized")
	state := session.GuardInit["server1"]
	require.NotNil(t, state)
	assert.True(t, state.Initialized)
	assert.Equal(t, difc.EnforcementStrict, state.DIFCMode)
	assert.Equal(t, "cli", state.PolicySource)
}

// TestEnsureGuardInitialized_LabelsAddedToRegistry verifies that secrecy and integrity
// tags from LabelAgent are merged into the agent registry.
func TestEnsureGuardInitialized_LabelsAddedToRegistry(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)
	g := &configurableGuard{
		name: "labeling-guard",
		labelAgentResult: &guard.LabelAgentResult{
			Agent: guard.AgentLabelsPayload{
				Secrecy:   []string{"private:org"},
				Integrity: []string{"merged"},
			},
			DIFCMode: "strict",
		},
	}

	ctx := guard.SetAgentIDInContext(context.Background(), "labeled-agent-id")
	_, err := us.ensureGuardInitialized(ctx, "session-labels", "server1", g, &noopBackendCaller{})

	require.NoError(t, err)

	agentLabels, ok := us.agentRegistry.Get("labeled-agent-id")
	require.True(t, ok, "agent labels should be registered")
	assert.Contains(t, agentLabels.GetSecrecyTags(), difc.Tag("private:org"))
	assert.Contains(t, agentLabels.GetIntegrityTags(), difc.Tag("merged"))
}

// TestEnsureGuardInitialized_PolicyHashChangeInvalidatesCache verifies that when the
// cached policy hash no longer matches the current policy the guard is re-initialized.
func TestEnsureGuardInitialized_PolicyHashChangeInvalidatesCache(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)

	callCount := 0
	g := &countGuard{
		inner: &configurableGuard{
			name: "hash-guard",
			labelAgentResult: &guard.LabelAgentResult{
				DIFCMode: "strict",
			},
		},
		callsPtr: &callCount,
	}

	// Pre-seed a stale cache entry with a hash that cannot match the current policy.
	us.sessions["session-hashtest"] = &Session{
		SessionID: "session-hashtest",
		StartTime: time.Now(),
		GuardInit: map[string]*GuardSessionState{
			"server1": {
				Initialized: true,
				PolicyHash:  "old-stale-hash-that-will-not-match",
				DIFCMode:    difc.EnforcementFilter,
			},
		},
	}

	ctx := guard.SetAgentIDInContext(context.Background(), "hash-agent")
	_, err := us.ensureGuardInitialized(ctx, "session-hashtest", "server1", g, &noopBackendCaller{})

	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "stale cache should trigger a fresh LabelAgent call")
}

// TestEnsureGuardInitialized_ExistingSessionGuardInitNil ensures that when a session
// exists but its GuardInit map is nil it is initialised before writing state.
func TestEnsureGuardInitialized_ExistingSessionGuardInitNil(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)

	// Insert a session with a nil GuardInit map.
	us.sessions["session-guardinit-nil"] = &Session{
		SessionID: "session-guardinit-nil",
		StartTime: time.Now(),
		GuardInit: nil,
	}

	g := &configurableGuard{
		name: "guardinit-nil-guard",
		labelAgentResult: &guard.LabelAgentResult{
			DIFCMode: "strict",
		},
	}

	ctx := guard.SetAgentIDInContext(context.Background(), "guardinit-nil-agent")
	mode, err := us.ensureGuardInitialized(ctx, "session-guardinit-nil", "server1", g, &noopBackendCaller{})

	require.NoError(t, err)
	assert.Equal(t, difc.EnforcementStrict, mode)

	us.sessionMu.RLock()
	session := us.sessions["session-guardinit-nil"]
	us.sessionMu.RUnlock()
	require.NotNil(t, session.GuardInit, "GuardInit should have been created")
	require.NotNil(t, session.GuardInit["server1"])
	assert.True(t, session.GuardInit["server1"].Initialized)
}

// TestEnsureGuardInitialized_NormalizedPolicyStored verifies that the normalized policy
// from LabelAgent (after scope_kind lowercasing) is persisted in the session state.
func TestEnsureGuardInitialized_NormalizedPolicyStored(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)
	g := &configurableGuard{
		name: "normalized-guard",
		labelAgentResult: &guard.LabelAgentResult{
			DIFCMode: "strict",
			NormalizedPolicy: map[string]interface{}{
				"scope_kind":    "Composite", // must be lowercased by normalizeScopeKind
				"min-integrity": "none",
			},
		},
	}

	ctx := guard.SetAgentIDInContext(context.Background(), "norm-agent")
	_, err := us.ensureGuardInitialized(ctx, "session-norm", "server1", g, &noopBackendCaller{})

	require.NoError(t, err)

	us.sessionMu.RLock()
	state := us.sessions["session-norm"].GuardInit["server1"]
	us.sessionMu.RUnlock()

	require.NotNil(t, state)
	assert.Equal(t, "composite", state.NormalizedPolicy["scope_kind"],
		"scope_kind should be lowercased via normalizeScopeKind")
	assert.Equal(t, "none", state.NormalizedPolicy["min-integrity"])
}

// TestEnsureGuardInitialized_MultipleServersIndependent confirms that guard state for
// different serverIDs is tracked independently within the same session.
func TestEnsureGuardInitialized_MultipleServersIndependent(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)

	calls := 0
	g := &countGuard{
		inner: &configurableGuard{
			name: "multi-server-guard",
			labelAgentResult: &guard.LabelAgentResult{
				DIFCMode: "strict",
			},
		},
		callsPtr: &calls,
	}

	ctx := guard.SetAgentIDInContext(context.Background(), "multi-agent")
	sessionID := "session-multi"

	_, err := us.ensureGuardInitialized(ctx, sessionID, "server-A", g, &noopBackendCaller{})
	require.NoError(t, err)

	_, err = us.ensureGuardInitialized(ctx, sessionID, "server-B", g, &noopBackendCaller{})
	require.NoError(t, err)

	assert.Equal(t, 2, calls,
		"each serverID should be initialized separately even within the same session")

	us.sessionMu.RLock()
	stateA := us.sessions[sessionID].GuardInit["server-A"]
	stateB := us.sessions[sessionID].GuardInit["server-B"]
	us.sessionMu.RUnlock()

	require.NotNil(t, stateA, "state for server-A should exist")
	require.NotNil(t, stateB, "state for server-B should exist")
}

// TestEnsureGuardInitialized_LabelsMergedAcrossGuards verifies the union-semantics
// contract: calling ensureGuardInitialized twice for the same agent (different servers)
// adds tags additively rather than overwriting.
func TestEnsureGuardInitialized_LabelsMergedAcrossGuards(t *testing.T) {
	cfg := &config.Config{
		DIFCMode:          "strict",
		GuardPolicySource: "cli",
		GuardPolicy:       validAllowOnlyPolicy(),
	}
	us := newMinimalUnifiedServer(cfg)

	agentID := "union-agent"
	ctx := guard.SetAgentIDInContext(context.Background(), agentID)

	gA := &configurableGuard{
		name: "guard-A",
		labelAgentResult: &guard.LabelAgentResult{
			Agent:    guard.AgentLabelsPayload{Secrecy: []string{"tag-A"}},
			DIFCMode: "strict",
		},
	}
	gB := &configurableGuard{
		name: "guard-B",
		labelAgentResult: &guard.LabelAgentResult{
			Agent:    guard.AgentLabelsPayload{Secrecy: []string{"tag-B"}},
			DIFCMode: "strict",
		},
	}

	_, err := us.ensureGuardInitialized(ctx, "session-union", "server-A", gA, &noopBackendCaller{})
	require.NoError(t, err)

	_, err = us.ensureGuardInitialized(ctx, "session-union", "server-B", gB, &noopBackendCaller{})
	require.NoError(t, err)

	agentLabels, ok := us.agentRegistry.Get(agentID)
	require.True(t, ok)
	tags := agentLabels.GetSecrecyTags()
	assert.Contains(t, tags, difc.Tag("tag-A"), "tag-A from guard-A should be present")
	assert.Contains(t, tags, difc.Tag("tag-B"), "tag-B from guard-B should be present")
}

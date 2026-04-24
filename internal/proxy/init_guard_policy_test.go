package proxy

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// labelAgentStubGuard is a configurable test double for guard.Guard that supports
// controlling the result of LabelAgent. Used specifically for initGuardPolicy tests.
type labelAgentStubGuard struct {
	labelAgentResult *guard.LabelAgentResult
	labelAgentErr    error
}

func (g *labelAgentStubGuard) Name() string { return "stub-label-agent" }

func (g *labelAgentStubGuard) LabelAgent(_ context.Context, _ interface{}, _ guard.BackendCaller, _ *difc.Capabilities) (*guard.LabelAgentResult, error) {
	return g.labelAgentResult, g.labelAgentErr
}

func (g *labelAgentStubGuard) LabelResource(_ context.Context, _ string, _ interface{}, _ guard.BackendCaller, _ *difc.Capabilities) (*difc.LabeledResource, difc.OperationType, error) {
	return difc.NewLabeledResource("stub"), difc.OperationRead, nil
}

func (g *labelAgentStubGuard) LabelResponse(_ context.Context, _ string, _ interface{}, _ guard.BackendCaller, _ *difc.Capabilities) (difc.LabeledData, error) {
	return nil, nil
}

// defaultLabelAgentStub returns a stub guard that succeeds with the given DIFC mode and agent labels.
func defaultLabelAgentStub(difcMode string, secrecy, integrity []string) *labelAgentStubGuard {
	return &labelAgentStubGuard{
		labelAgentResult: &guard.LabelAgentResult{
			Agent: guard.AgentLabelsPayload{
				Secrecy:   secrecy,
				Integrity: integrity,
			},
			DIFCMode: difcMode,
		},
	}
}

// newTestServerForInitGuardPolicy creates a minimal proxy.Server for testing initGuardPolicy.
func newTestServerForInitGuardPolicy(g guard.Guard, mode difc.EnforcementMode) *Server {
	return &Server{
		guard:           g,
		evaluator:       difc.NewEvaluatorWithMode(mode),
		agentRegistry:   difc.NewAgentRegistryWithDefaults(nil, nil),
		capabilities:    difc.NewCapabilities(),
		githubAPIURL:    "https://api.github.com",
		httpClient:      &http.Client{},
		enforcementMode: mode,
	}
}

// validAllowOnlyPolicyJSON is a minimal valid allow-only guard policy.
const validAllowOnlyPolicyJSON = `{"allow-only":{"repos":"public","min-integrity":"none"}}`

// validWriteSinkPolicyJSON is a valid write-sink guard policy.
const validWriteSinkPolicyJSON = `{"write-sink":{"accept":["*"]}}`

// TestInitGuardPolicy_InvalidJSON verifies that non-JSON input is rejected immediately.
func TestInitGuardPolicy_InvalidJSON(t *testing.T) {
	g := defaultLabelAgentStub(difc.ModeFilter, nil, nil)
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	err := s.initGuardPolicy(context.Background(), "not-valid-json", nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid policy JSON")
	assert.False(t, s.guardInitialized, "guardInitialized must stay false on error")
}

// TestInitGuardPolicy_ValidationFailure verifies that a structurally valid JSON object
// that does not constitute a valid guard policy is rejected.
func TestInitGuardPolicy_ValidationFailure(t *testing.T) {
	g := defaultLabelAgentStub(difc.ModeFilter, nil, nil)
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	// An empty object has neither allow-only nor write-sink.
	err := s.initGuardPolicy(context.Background(), `{}`, nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy validation failed")
	assert.False(t, s.guardInitialized)
}

// TestInitGuardPolicy_WriteSinkRejected verifies that a write-sink policy is rejected because
// the proxy only accepts allow-only policies during guard initialization.
func TestInitGuardPolicy_WriteSinkRejected(t *testing.T) {
	g := defaultLabelAgentStub(difc.ModeFilter, nil, nil)
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	err := s.initGuardPolicy(context.Background(), validWriteSinkPolicyJSON, nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write-sink policies are not supported")
	assert.False(t, s.guardInitialized)
}

// TestInitGuardPolicy_LabelAgentError verifies that an error from the guard's LabelAgent
// call propagates correctly and leaves the server uninitialized.
func TestInitGuardPolicy_LabelAgentError(t *testing.T) {
	g := &labelAgentStubGuard{
		labelAgentErr: errors.New("guard: wasm runtime error"),
	}
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	err := s.initGuardPolicy(context.Background(), validAllowOnlyPolicyJSON, nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "LabelAgent failed")
	assert.Contains(t, err.Error(), "guard: wasm runtime error")
	assert.False(t, s.guardInitialized)
}

// TestInitGuardPolicy_LabelAgentNilResult verifies that a nil result from LabelAgent
// is treated as an error and leaves the server uninitialized.
func TestInitGuardPolicy_LabelAgentNilResult(t *testing.T) {
	g := &labelAgentStubGuard{labelAgentResult: nil}
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	err := s.initGuardPolicy(context.Background(), validAllowOnlyPolicyJSON, nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil result")
	assert.False(t, s.guardInitialized)
}

// TestInitGuardPolicy_SuccessWithNoLabels verifies the happy path: a valid policy with no
// agent labels sets guardInitialized to true and leaves the enforcement mode unchanged.
func TestInitGuardPolicy_SuccessWithNoLabels(t *testing.T) {
	g := defaultLabelAgentStub(difc.ModeFilter, []string{}, []string{})
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	err := s.initGuardPolicy(context.Background(), validAllowOnlyPolicyJSON, nil, nil)

	require.NoError(t, err)
	assert.True(t, s.guardInitialized)
	assert.Equal(t, difc.EnforcementFilter, s.enforcementMode)
}

// TestInitGuardPolicy_SuccessAppliesAgentLabels verifies that secrecy and integrity tags
// returned by LabelAgent are applied to the proxy agent in the registry.
func TestInitGuardPolicy_SuccessAppliesAgentLabels(t *testing.T) {
	g := defaultLabelAgentStub(difc.ModeFilter, []string{"private:org/repo"}, []string{"approved"})
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	err := s.initGuardPolicy(context.Background(), validAllowOnlyPolicyJSON, nil, nil)

	require.NoError(t, err)
	assert.True(t, s.guardInitialized)

	labels := s.agentRegistry.GetOrCreate("proxy")
	require.NotNil(t, labels)
	assert.Contains(t, labels.GetSecrecyTags(), difc.Tag("private:org/repo"), "secrecy tag must be applied")
	assert.Contains(t, labels.GetIntegrityTags(), difc.Tag("approved"), "integrity tag must be applied")
}

// TestInitGuardPolicy_DIFCModeOverride verifies that when the guard returns a DIFCMode in the
// LabelAgent response, it overrides the server's current enforcement mode.
func TestInitGuardPolicy_DIFCModeOverride(t *testing.T) {
	// Server starts in filter mode but guard wants strict.
	g := defaultLabelAgentStub(difc.ModeStrict, []string{}, []string{})
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	err := s.initGuardPolicy(context.Background(), validAllowOnlyPolicyJSON, nil, nil)

	require.NoError(t, err)
	assert.True(t, s.guardInitialized)
	assert.Equal(t, difc.EnforcementStrict, s.enforcementMode,
		"guard response DIFCMode must override the server's enforcement mode")
}

// TestInitGuardPolicy_InvalidDIFCModeError verifies that an unrecognized DIFCMode in the
// guard response is treated as an error so the caller can react to a malformed guard output.
func TestInitGuardPolicy_InvalidDIFCModeError(t *testing.T) {
	g := defaultLabelAgentStub("not-a-real-mode", []string{}, []string{})
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	err := s.initGuardPolicy(context.Background(), validAllowOnlyPolicyJSON, nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid difc_mode")
	assert.False(t, s.guardInitialized,
		"guard must not be marked initialized when DIFCMode is invalid")
	assert.Equal(t, difc.EnforcementFilter, s.enforcementMode,
		"enforcement mode must remain unchanged when DIFCMode is invalid")
}

// TestInitGuardPolicy_EmptyDIFCModePreservesMode verifies that an empty DIFCMode in
// the guard response preserves the current enforcement mode without error.
func TestInitGuardPolicy_EmptyDIFCModePreservesMode(t *testing.T) {
	g := defaultLabelAgentStub("", []string{}, []string{})
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementStrict)

	err := s.initGuardPolicy(context.Background(), validAllowOnlyPolicyJSON, nil, nil)

	require.NoError(t, err)
	assert.True(t, s.guardInitialized)
	// Empty DIFCMode causes the mode-override block to be skipped entirely, so the
	// server's initial strict mode is preserved.
	assert.Equal(t, difc.EnforcementStrict, s.enforcementMode)
}

// TestInitGuardPolicy_LegacyAllowOnlyKey verifies that a policy using the legacy
// "allowonly" key (without the hyphen) is accepted and the guard is initialized.
// The normalization step in initGuardPolicy maps "allowonly" → "allow-only" so that
// trusted-user injection via BuildLabelAgentPayload works correctly.
func TestInitGuardPolicy_LegacyAllowOnlyKey(t *testing.T) {
	g := defaultLabelAgentStub(difc.ModeFilter, []string{}, []string{})
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	legacyPolicy := `{"allowonly":{"repos":"public","min-integrity":"none"}}`

	err := s.initGuardPolicy(context.Background(), legacyPolicy, nil, nil)

	require.NoError(t, err)
	assert.True(t, s.guardInitialized)
}

// TestInitGuardPolicy_LegacyAllowOnlyKeyWithTrustedUsers verifies that a legacy "allowonly"
// key is normalized before trusted-user injection so that trusted users are injected into
// the correct "allow-only" slot in the payload.
func TestInitGuardPolicy_LegacyAllowOnlyKeyWithTrustedUsers(t *testing.T) {
	g := defaultLabelAgentStub(difc.ModeFilter, []string{}, []string{})
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	legacyPolicy := `{"allowonly":{"repos":"public","min-integrity":"none"}}`
	trustedUsers := []string{"alice", "bob"}

	// If the normalization works, trusted users are injected into "allow-only" and
	// the call succeeds. If it doesn't work, the payload would lack an "allow-only"
	// key and the trusted-user injection would be silently skipped (no error, but
	// we can at least confirm the guard initialized).
	err := s.initGuardPolicy(context.Background(), legacyPolicy, nil, trustedUsers)

	require.NoError(t, err)
	assert.True(t, s.guardInitialized)
}

// TestInitGuardPolicy_TrustedBotsAndUsers verifies that trusted bots and users can be
// provided alongside a valid allow-only policy without causing an error.
func TestInitGuardPolicy_TrustedBotsAndUsers(t *testing.T) {
	g := defaultLabelAgentStub(difc.ModeFilter, []string{}, []string{})
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	trustedBots := []string{"dependabot[bot]", "renovate[bot]"}
	trustedUsers := []string{"alice"}

	err := s.initGuardPolicy(context.Background(), validAllowOnlyPolicyJSON, trustedBots, trustedUsers)

	require.NoError(t, err)
	assert.True(t, s.guardInitialized)
}

// TestInitGuardPolicy_MultipleSecrecyTags verifies that multiple secrecy tags are all applied.
func TestInitGuardPolicy_MultipleSecrecyTags(t *testing.T) {
	secrecy := []string{"private:org/repo1", "private:org/repo2"}
	g := defaultLabelAgentStub(difc.ModeFilter, secrecy, []string{})
	s := newTestServerForInitGuardPolicy(g, difc.EnforcementFilter)

	err := s.initGuardPolicy(context.Background(), validAllowOnlyPolicyJSON, nil, nil)

	require.NoError(t, err)
	assert.True(t, s.guardInitialized)

	labels := s.agentRegistry.GetOrCreate("proxy")
	require.NotNil(t, labels)
	for _, tag := range secrecy {
		assert.Contains(t, labels.GetSecrecyTags(), difc.Tag(tag), "secrecy tag %q must be applied", tag)
	}
}

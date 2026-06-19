package guard

import (
	"context"
	"errors"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runLabelAgentStubGuard is a minimal Guard implementation used to test RunLabelAgent.
type runLabelAgentStubGuard struct {
	name             string
	labelAgentResult *LabelAgentResult
	labelAgentErr    error
	lastPayload      interface{}
}

func (g *runLabelAgentStubGuard) Name() string { return g.name }

func (g *runLabelAgentStubGuard) LabelAgent(_ context.Context, payload interface{}, _ BackendCaller, _ *difc.Capabilities) (*LabelAgentResult, error) {
	g.lastPayload = payload
	return g.labelAgentResult, g.labelAgentErr
}

func (g *runLabelAgentStubGuard) LabelResource(_ context.Context, _ string, _ interface{}, _ BackendCaller, _ *difc.Capabilities) (*difc.LabeledResource, difc.OperationType, error) {
	return difc.NewLabeledResource("stub"), difc.OperationRead, nil
}

func (g *runLabelAgentStubGuard) LabelResponse(_ context.Context, _ string, _ interface{}, _ BackendCaller, _ *difc.Capabilities) (difc.LabeledData, error) {
	return nil, nil
}

// noopRunBackendCaller is a BackendCaller that always returns nil.
type noopRunBackendCaller struct{}

func (n *noopRunBackendCaller) CallTool(_ context.Context, _ string, _ interface{}) (interface{}, error) {
	return nil, nil
}

func TestRunLabelAgent_Success(t *testing.T) {
	g := &runLabelAgentStubGuard{
		name: "test-guard",
		labelAgentResult: &LabelAgentResult{
			Agent:    AgentLabelsPayload{Secrecy: []string{"private:org/repo"}, Integrity: []string{"approved"}},
			DIFCMode: difc.ModeFilter,
		},
	}
	caps := difc.NewCapabilities()
	agentLabels := difc.NewAgentRegistryWithDefaults(nil, nil).GetOrCreate("test-agent")
	defaultMode := difc.EnforcementStrict

	mode, result, err := RunLabelAgent(context.Background(), g, map[string]interface{}{"policy": "test"}, &noopRunBackendCaller{}, caps, agentLabels, defaultMode)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, difc.EnforcementFilter, mode, "mode should be overridden by guard response")
	assert.Equal(t, difc.ModeFilter, result.DIFCMode)
}

func TestRunLabelAgent_GuardError(t *testing.T) {
	g := &runLabelAgentStubGuard{
		name:          "error-guard",
		labelAgentErr: errors.New("wasm runtime error"),
	}
	caps := difc.NewCapabilities()
	agentLabels := difc.NewAgentRegistryWithDefaults(nil, nil).GetOrCreate("test-agent")
	defaultMode := difc.EnforcementFilter

	mode, result, err := RunLabelAgent(context.Background(), g, nil, &noopRunBackendCaller{}, caps, agentLabels, defaultMode)

	require.Error(t, err)
	assert.ErrorContains(t, err, "LabelAgent failed")
	assert.ErrorContains(t, err, "wasm runtime error")
	assert.Nil(t, result)
	assert.Equal(t, defaultMode, mode, "defaultMode should be returned on error")
}

func TestRunLabelAgent_NilResult(t *testing.T) {
	g := &runLabelAgentStubGuard{
		name:             "nil-result-guard",
		labelAgentResult: nil,
	}
	caps := difc.NewCapabilities()
	agentLabels := difc.NewAgentRegistryWithDefaults(nil, nil).GetOrCreate("test-agent")
	defaultMode := difc.EnforcementStrict

	mode, result, err := RunLabelAgent(context.Background(), g, nil, &noopRunBackendCaller{}, caps, agentLabels, defaultMode)

	require.Error(t, err)
	assert.ErrorContains(t, err, "LabelAgent returned nil result")
	assert.Nil(t, result)
	assert.Equal(t, defaultMode, mode)
}

func TestRunLabelAgent_InvalidDIFCMode(t *testing.T) {
	g := &runLabelAgentStubGuard{
		name: "bad-mode-guard",
		labelAgentResult: &LabelAgentResult{
			Agent:    AgentLabelsPayload{Secrecy: []string{}, Integrity: []string{}},
			DIFCMode: "not-a-real-mode",
		},
	}
	caps := difc.NewCapabilities()
	agentLabels := difc.NewAgentRegistryWithDefaults(nil, nil).GetOrCreate("test-agent")
	defaultMode := difc.EnforcementFilter

	mode, result, err := RunLabelAgent(context.Background(), g, nil, &noopRunBackendCaller{}, caps, agentLabels, defaultMode)

	require.Error(t, err)
	assert.ErrorContains(t, err, "LabelAgent result invalid")
	assert.Nil(t, result)
	assert.Equal(t, defaultMode, mode)
}

func TestRunLabelAgent_EmptyDIFCModePreservesDefault(t *testing.T) {
	g := &runLabelAgentStubGuard{
		name: "empty-mode-guard",
		labelAgentResult: &LabelAgentResult{
			Agent:    AgentLabelsPayload{Secrecy: []string{}, Integrity: []string{}},
			DIFCMode: "", // empty → preserve defaultMode
		},
	}
	caps := difc.NewCapabilities()
	agentLabels := difc.NewAgentRegistryWithDefaults(nil, nil).GetOrCreate("test-agent")
	defaultMode := difc.EnforcementStrict

	mode, result, err := RunLabelAgent(context.Background(), g, nil, &noopRunBackendCaller{}, caps, agentLabels, defaultMode)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, defaultMode, mode, "empty DIFCMode should preserve defaultMode")
}

func TestRunLabelAgentForAgent_Success(t *testing.T) {
	g := &runLabelAgentStubGuard{
		name: "test-guard",
		labelAgentResult: &LabelAgentResult{
			Agent:    AgentLabelsPayload{Secrecy: []string{"private:org/repo"}, Integrity: []string{"approved"}},
			DIFCMode: difc.ModeFilter,
		},
	}
	caps := difc.NewCapabilities()
	registry := difc.NewAgentRegistry()
	defaultMode := difc.EnforcementStrict

	mode, result, err := RunLabelAgentForAgent(context.Background(), g, map[string]interface{}{"policy": "test"}, &noopRunBackendCaller{}, caps, registry, "agent-1", defaultMode)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, difc.EnforcementFilter, mode)
	// agent should have been created in the registry and have the returned labels applied
	labels, ok := registry.Get("agent-1")
	require.True(t, ok, "agent should be registered")
	require.NotNil(t, labels)
	assert.Contains(t, labels.GetSecrecyTags(), difc.Tag("private:org/repo"))
	assert.Contains(t, labels.GetIntegrityTags(), difc.Tag("approved"))
}

func TestRunLabelAgentForAgent_PropagatesError(t *testing.T) {
	g := &runLabelAgentStubGuard{
		name:          "error-guard",
		labelAgentErr: errors.New("wasm error"),
	}
	caps := difc.NewCapabilities()
	registry := difc.NewAgentRegistry()
	defaultMode := difc.EnforcementStrict

	mode, result, err := RunLabelAgentForAgent(context.Background(), g, nil, &noopRunBackendCaller{}, caps, registry, "agent-2", defaultMode)

	require.Error(t, err)
	assert.ErrorContains(t, err, "LabelAgent failed")
	assert.Nil(t, result)
	assert.Equal(t, defaultMode, mode)
}

func TestRunLabelAgentInit_BuildsPayloadAndRuns(t *testing.T) {
	g := &runLabelAgentStubGuard{
		name: "test-guard",
		labelAgentResult: &LabelAgentResult{
			Agent:    AgentLabelsPayload{Secrecy: []string{}, Integrity: []string{}},
			DIFCMode: difc.ModeFilter,
		},
	}
	caps := difc.NewCapabilities()
	registry := difc.NewAgentRegistry()
	defaultMode := difc.EnforcementStrict
	policy := map[string]interface{}{
		"allow-only": map[string]interface{}{
			"repos":         "public",
			"min-integrity": "none",
		},
	}

	mode, result, err := RunLabelAgentInit(
		context.Background(),
		g,
		policy,
		[]string{"dependabot[bot]"},
		[]string{"alice"},
		&noopRunBackendCaller{},
		caps,
		registry,
		"agent-3",
		defaultMode,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, difc.EnforcementFilter, mode)

	payload, ok := g.lastPayload.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{"dependabot[bot]"}, payload["trusted-bots"])

	allowOnly, ok := payload["allow-only"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "public", allowOnly["repos"])
	assert.Equal(t, "none", allowOnly["min-integrity"])
	assert.Equal(t, []interface{}{"alice"}, allowOnly["trusted-users"])
}

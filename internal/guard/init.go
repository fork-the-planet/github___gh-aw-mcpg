package guard

import (
	"context"
	"fmt"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logGuardInit = logger.New("guard:init")

// RunLabelAgent executes the standard LabelAgent initialization pipeline:
//  1. Calls the guard's LabelAgent method with the provided pre-built payload.
//  2. Validates the result is non-nil.
//  3. Applies the returned agent labels to agentLabels.
//  4. Parses and returns the effective enforcement mode.
//
// It returns the effective enforcement mode, the raw LabelAgentResult (so
// callers can inspect NormalizedPolicy, DIFCMode, etc.), and any error.
// On error, defaultMode is returned unchanged so the caller's mode is unaffected.
func RunLabelAgent(
	ctx context.Context,
	g Guard,
	payload interface{},
	backend BackendCaller,
	caps *difc.Capabilities,
	agentLabels *difc.AgentLabels,
	defaultMode difc.EnforcementMode,
) (difc.EnforcementMode, *LabelAgentResult, error) {
	logGuardInit.Printf("Calling LabelAgent: guard=%s", g.Name())

	result, err := g.LabelAgent(ctx, payload, backend, caps)
	if err != nil {
		logGuardInit.Printf("LabelAgent failed: guard=%s, error=%v", g.Name(), err)
		return defaultMode, nil, fmt.Errorf("LabelAgent failed: %w", err)
	}
	if result == nil {
		logGuardInit.Printf("LabelAgent returned nil result: guard=%s", g.Name())
		return defaultMode, nil, fmt.Errorf("LabelAgent returned nil result")
	}

	mode, err := ApplyLabelAgentResult(result, agentLabels, defaultMode)
	if err != nil {
		logGuardInit.Printf("LabelAgent result invalid: guard=%s, error=%v", g.Name(), err)
		return defaultMode, nil, fmt.Errorf("LabelAgent result invalid: %w", err)
	}

	logGuardInit.Printf("LabelAgent completed: guard=%s, mode=%s", g.Name(), mode)
	return mode, result, nil
}

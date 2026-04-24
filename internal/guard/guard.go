package guard

import (
	"context"
	"fmt"

	"github.com/github/gh-aw-mcpg/internal/difc"
)

// BackendCaller provides a way for guards to make read-only calls to the backend
// to gather information needed for labeling (e.g., fetching issue author)
type BackendCaller interface {
	// CallTool makes a read-only call to the backend MCP server
	// This is used by guards to gather metadata for labeling
	CallTool(ctx context.Context, toolName string, args interface{}) (interface{}, error)
}

// Guard handles DIFC labeling for a specific MCP server
// Guards ONLY label resources - they do NOT make access control decisions
// The Reference Monitor (in the server) uses guard-provided labels to enforce DIFC policies
type Guard interface {
	// Name returns the identifier for this guard (e.g., "github", "noop")
	Name() string

	// LabelAgent initializes guard policy and returns effective agent/session state
	// for the current session.
	// Returns:
	//   - result: effective labels, mode, and normalized policy
	//   - error: any validation/initialization error
	LabelAgent(ctx context.Context, policy interface{}, backend BackendCaller, caps *difc.Capabilities) (*LabelAgentResult, error)

	// LabelResource determines the resource being accessed and its labels
	// This may call the backend (via BackendCaller) to gather metadata needed for labeling
	// Returns:
	//   - resource: The labeled resource (simple or nested structure for fine-grained filtering)
	//   - operation: The type of operation (Read, Write, or ReadWrite)
	//   - error: Any error that occurred during labeling
	LabelResource(ctx context.Context, toolName string, args interface{}, backend BackendCaller, caps *difc.Capabilities) (*difc.LabeledResource, difc.OperationType, error)

	// LabelResponse labels the response data after a successful backend call
	// This is used for fine-grained filtering of collections
	// Returns:
	//   - labeledData: The response data with per-item labels (if applicable)
	//   - error: Any error that occurred during labeling
	// If the guard returns nil for labeledData, the reference monitor will use the
	// resource labels from LabelResource for the entire response
	LabelResponse(ctx context.Context, toolName string, result interface{}, backend BackendCaller, caps *difc.Capabilities) (difc.LabeledData, error)
}

// LabelAgentResult describes the effective policy/session state returned by a guard.
type LabelAgentResult struct {
	Agent            AgentLabelsPayload     `json:"agent"`
	DIFCMode         string                 `json:"difc_mode"`
	NormalizedPolicy map[string]interface{} `json:"normalized_policy,omitempty"`
}

// AgentLabelsPayload holds effective secrecy/integrity labels for the session.
type AgentLabelsPayload struct {
	Secrecy   []string `json:"secrecy"`
	Integrity []string `json:"integrity"`
}

// RequestState represents any state that the guard needs to pass from request to response
// This is useful when the guard needs to carry information from LabelResource to LabelResponse
type RequestState interface{}

// emptyAgentLabelsResult returns a LabelAgentResult with empty agent labels for the given DIFC mode.
// Used by guards that do not contribute agent labels (e.g. NoopGuard, WriteSinkGuard).
func emptyAgentLabelsResult(mode string) *LabelAgentResult {
	return &LabelAgentResult{
		Agent: AgentLabelsPayload{
			Secrecy:   []string{},
			Integrity: []string{},
		},
		DIFCMode: mode,
	}
}

// ApplyLabelAgentResult applies the agent labels from a LabelAgentResult to the given
// AgentLabels using batch helpers (minimizing mutex acquisitions), and returns the
// effective enforcement mode. If result.DIFCMode is empty, defaultMode is returned
// unchanged. If result.DIFCMode is non-empty but cannot be parsed, an error is returned.
func ApplyLabelAgentResult(result *LabelAgentResult, agentLabels *difc.AgentLabels, defaultMode difc.EnforcementMode) (difc.EnforcementMode, error) {
	// Validate/parse mode first so that tag mutation is skipped when mode is invalid.
	// This keeps the operation atomic: either both the mode and the tags are applied,
	// or neither is.
	mode := defaultMode
	if result.DIFCMode != "" {
		parsedMode, err := difc.ParseEnforcementMode(result.DIFCMode)
		if err != nil {
			return defaultMode, fmt.Errorf("invalid difc_mode from label_agent: %w", err)
		}
		mode = parsedMode
	}

	agentLabels.AddSecrecyTags(difc.StringsToTags(result.Agent.Secrecy))
	agentLabels.AddIntegrityTags(difc.StringsToTags(result.Agent.Integrity))

	return mode, nil
}

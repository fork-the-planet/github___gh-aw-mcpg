package guard

import (
	"context"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logWriteSink = logger.New("guard:write-sink")

// WriteSinkGuard is a guard for write-only output channels (e.g., safe-outputs).
//
// When DIFC is enabled, an agent that reads from a guarded server (like GitHub)
// acquires secrecy and integrity tags. Writing to an unguarded output server
// would fail because the DIFC evaluator sees a label mismatch: the agent has
// tags that the resource (with empty labels from a noop guard) does not.
//
// The write-sink guard fixes this by:
//   - Returning empty labels from LabelAgent (does not contribute agent labels)
//   - Setting resource secrecy to the configured accept patterns
//   - Classifying all operations as OperationWrite
//
// This ensures writes succeed because:
//   - Integrity: resource requires no tags (empty), agent has all zero required → OK
//   - Secrecy: resource secrecy includes the agent's secrecy patterns → agentSecrecy ⊆ resourceSecrecy → OK
//
// Write-sink is required for ALL output servers when DIFC guards are enabled,
// including when repos="all" or repos="public". Without it, the noop guard
// assigns OperationRead + empty labels, causing integrity violations when the
// agent has integrity tags from other guards.
//
// Configuration examples:
//
//	// For repos="all" or repos="public" (agent has no secrecy):
//	"guard-policies": {
//	  "write-sink": {
//	    "accept": ["*"]
//	  }
//	}
//
//	// For scoped repos (agent has secrecy tags):
//	"guard-policies": {
//	  "write-sink": {
//	    "accept": ["private:github/gh-aw*"]
//	  }
//	}
type WriteSinkGuard struct {
	acceptTags []difc.Tag
}

// NewWriteSinkGuard creates a new write-sink guard with the specified accept patterns.
// Each pattern becomes a secrecy tag on the resource, allowing agents with
// matching secrecy to write to this sink.
func NewWriteSinkGuard(accept []string) *WriteSinkGuard {
	tags := make([]difc.Tag, len(accept))
	for i, a := range accept {
		tags[i] = difc.Tag(a)
	}
	logWriteSink.Printf("Creating write-sink guard with %d accept patterns", len(tags))
	return &WriteSinkGuard{acceptTags: tags}
}

// Name returns the identifier for this guard
func (g *WriteSinkGuard) Name() string {
	return "write-sink"
}

// LabelAgent returns empty labels. The write-sink does not contribute agent
// labels — those are set by the primary guard (e.g., the GitHub WASM guard).
func (g *WriteSinkGuard) LabelAgent(_ context.Context, _ interface{}, _ BackendCaller, _ *difc.Capabilities) (*LabelAgentResult, error) {
	logWriteSink.Print("LabelAgent: returning empty labels (write-sink does not label agents)")
	return &LabelAgentResult{
		Agent: AgentLabelsPayload{
			Secrecy:   []string{},
			Integrity: []string{},
		},
		DIFCMode: difc.ModeFilter,
	}, nil
}

// LabelResource sets the resource's secrecy to the configured accept patterns
// and classifies the operation as a write.
//
// For writes the DIFC evaluator checks:
//   - agentSecrecy ⊆ resource.Secrecy     (no secret information leak)
//   - resource.Integrity ⊆ agentIntegrity  (agent is trusted enough)
//
// By setting the resource secrecy to the accept patterns from config, agents
// whose secrecy tags are a subset of the accept set can write successfully.
// By leaving the resource integrity empty, the second check also passes
// because the agent has all zero of the (empty) required integrity tags.
func (g *WriteSinkGuard) LabelResource(_ context.Context, toolName string, _ interface{}, _ BackendCaller, _ *difc.Capabilities) (*difc.LabeledResource, difc.OperationType, error) {
	logWriteSink.Printf("LabelResource: tool=%s, operation=write, accept_tags=%d", toolName, len(g.acceptTags))

	resource := &difc.LabeledResource{
		Description: "write-sink (" + toolName + ")",
		Secrecy:     *difc.NewSecrecyLabelWithTags(g.acceptTags),
		Integrity:   *difc.NewIntegrityLabel(), // empty: no integrity requirements
	}

	return resource, difc.OperationWrite, nil
}

// LabelResponse returns nil; the write-sink does not perform fine-grained
// response labeling since all operations are writes (responses are confirmations).
func (g *WriteSinkGuard) LabelResponse(_ context.Context, _ string, _ interface{}, _ BackendCaller, _ *difc.Capabilities) (difc.LabeledData, error) {
	return nil, nil
}

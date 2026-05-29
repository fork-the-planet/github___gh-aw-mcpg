package difc

import "github.com/github/gh-aw-mcpg/internal/logger"

var logPipeline = logger.New("difc:pipeline_decisions")

// CoarseCheckOutcome is the typed result of a Phase 2 coarse-grained access check.
type CoarseCheckOutcome int

const (
	// CoarseAllowed means the access is permitted by the coarse-grained check.
	CoarseAllowed CoarseCheckOutcome = iota
	// CoarseBypassForRead means the coarse check would deny, but the operation is
	// a read so the pipeline should continue to Phase 5 fine-grained filtering.
	CoarseBypassForRead
	// CoarseDenied means access is blocked.
	CoarseDenied
)

// EvaluateCoarseAccess runs Phase 2 of the DIFC pipeline: it evaluates the
// coarse-grained access check and classifies the outcome as CoarseAllowed,
// CoarseBypassForRead, or CoarseDenied. The underlying EvaluationResult is
// returned so callers can use the Reason field and other details when
// formulating their denial response (MCP error vs HTTP 403).
func EvaluateCoarseAccess(
	evaluator *Evaluator,
	agentSecrecy *SecrecyLabel,
	agentIntegrity *IntegrityLabel,
	resource *LabeledResource,
	operation OperationType,
) (CoarseCheckOutcome, *EvaluationResult) {
	logPipeline.Printf("EvaluateCoarseAccess: operation=%s, resource=%s", operation, resource.Description)
	result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, operation)
	if result.IsAllowed() {
		logPipeline.Printf("EvaluateCoarseAccess: outcome=allowed, operation=%s", operation)
		return CoarseAllowed, result
	}
	if ShouldBypassCoarseDeny(operation) {
		logPipeline.Printf("EvaluateCoarseAccess: outcome=bypass-for-read, operation=%s", operation)
		return CoarseBypassForRead, result
	}
	logPipeline.Printf("EvaluateCoarseAccess: outcome=denied, operation=%s, reason=%s", operation, result.Reason)
	return CoarseDenied, result
}

// ShouldBypassCoarseDeny returns true when a coarse-grained deny should still
// proceed to backend execution so Phase 5 can enforce per-item policy.
func ShouldBypassCoarseDeny(operation OperationType) bool {
	result := operation == OperationRead
	logPipeline.Printf("ShouldBypassCoarseDeny: operation=%s, bypass=%t", operation, result)
	return result
}

// ShouldCallLabelResponse returns true when guards should label response data
// for possible fine-grained filtering.
func ShouldCallLabelResponse(operation OperationType, enforcementMode EnforcementMode) bool {
	isPureWrite := operation == OperationWrite
	result := !isPureWrite && (operation != OperationReadWrite || enforcementMode != EnforcementStrict)
	logPipeline.Printf("ShouldCallLabelResponse: operation=%s, mode=%s, result=%t", operation, enforcementMode, result)
	return result
}

// ShouldBlockFilteredResponse returns true when filtered items should block the
// whole response instead of returning a partially filtered result.
func ShouldBlockFilteredResponse(enforcementMode EnforcementMode, filteredCount int) bool {
	result := enforcementMode == EnforcementStrict && filteredCount > 0
	logPipeline.Printf("ShouldBlockFilteredResponse: mode=%s, filteredCount=%d, block=%t", enforcementMode, filteredCount, result)
	return result
}

// ShouldAccumulateReadLabels returns true when read labels should be
// accumulated back into the agent label set.
func ShouldAccumulateReadLabels(operation OperationType, enforcementMode EnforcementMode) bool {
	result := operation != OperationWrite && enforcementMode == EnforcementPropagate
	logPipeline.Printf("ShouldAccumulateReadLabels: operation=%s, mode=%s, accumulate=%t", operation, enforcementMode, result)
	return result
}

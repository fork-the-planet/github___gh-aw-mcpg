package difc

import "github.com/github/gh-aw-mcpg/internal/logger"

var logPipeline = logger.New("difc:pipeline_decisions")

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

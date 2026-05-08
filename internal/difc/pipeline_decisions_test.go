package difc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldBypassCoarseDeny(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation OperationType
		want      bool
	}{
		{name: "read bypasses coarse deny", operation: OperationRead, want: true},
		{name: "write does not bypass", operation: OperationWrite, want: false},
		{name: "read-write does not bypass", operation: OperationReadWrite, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ShouldBypassCoarseDeny(tt.operation))
		})
	}
}

func TestShouldCallLabelResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		operation       OperationType
		enforcementMode EnforcementMode
		want            bool
	}{
		// Pure write operations never require labeling regardless of enforcement mode.
		{name: "write/strict: no labeling", operation: OperationWrite, enforcementMode: EnforcementStrict, want: false},
		{name: "write/filter: no labeling", operation: OperationWrite, enforcementMode: EnforcementFilter, want: false},
		{name: "write/propagate: no labeling", operation: OperationWrite, enforcementMode: EnforcementPropagate, want: false},

		// Read-write in strict mode does not label (strict coarse deny handles it).
		{name: "read-write/strict: no labeling", operation: OperationReadWrite, enforcementMode: EnforcementStrict, want: false},

		// Read-write in non-strict modes requires labeling for fine-grained filtering.
		{name: "read-write/filter: labeling required", operation: OperationReadWrite, enforcementMode: EnforcementFilter, want: true},
		{name: "read-write/propagate: labeling required", operation: OperationReadWrite, enforcementMode: EnforcementPropagate, want: true},

		// Pure read operations always require labeling.
		{name: "read/strict: labeling required", operation: OperationRead, enforcementMode: EnforcementStrict, want: true},
		{name: "read/filter: labeling required", operation: OperationRead, enforcementMode: EnforcementFilter, want: true},
		{name: "read/propagate: labeling required", operation: OperationRead, enforcementMode: EnforcementPropagate, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ShouldCallLabelResponse(tt.operation, tt.enforcementMode))
		})
	}
}

func TestShouldBlockFilteredResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		enforcementMode EnforcementMode
		filteredCount   int
		want            bool
	}{
		// Strict mode blocks when items were filtered.
		{name: "strict/filtered=1: block", enforcementMode: EnforcementStrict, filteredCount: 1, want: true},
		{name: "strict/filtered=5: block", enforcementMode: EnforcementStrict, filteredCount: 5, want: true},

		// Strict mode does not block when nothing was filtered.
		{name: "strict/filtered=0: no block", enforcementMode: EnforcementStrict, filteredCount: 0, want: false},

		// Non-strict modes never block regardless of filtered count.
		{name: "filter/filtered=3: no block", enforcementMode: EnforcementFilter, filteredCount: 3, want: false},
		{name: "filter/filtered=0: no block", enforcementMode: EnforcementFilter, filteredCount: 0, want: false},
		{name: "propagate/filtered=2: no block", enforcementMode: EnforcementPropagate, filteredCount: 2, want: false},
		{name: "propagate/filtered=0: no block", enforcementMode: EnforcementPropagate, filteredCount: 0, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ShouldBlockFilteredResponse(tt.enforcementMode, tt.filteredCount))
		})
	}
}

func TestShouldAccumulateReadLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		operation       OperationType
		enforcementMode EnforcementMode
		want            bool
	}{
		// Only propagate mode accumulates read labels, and only for non-write operations.
		{name: "read/propagate: accumulate", operation: OperationRead, enforcementMode: EnforcementPropagate, want: true},
		{name: "read-write/propagate: accumulate", operation: OperationReadWrite, enforcementMode: EnforcementPropagate, want: true},

		// Write operations never accumulate labels.
		{name: "write/propagate: no accumulation", operation: OperationWrite, enforcementMode: EnforcementPropagate, want: false},
		{name: "write/strict: no accumulation", operation: OperationWrite, enforcementMode: EnforcementStrict, want: false},
		{name: "write/filter: no accumulation", operation: OperationWrite, enforcementMode: EnforcementFilter, want: false},

		// Non-propagate modes never accumulate labels.
		{name: "read/strict: no accumulation", operation: OperationRead, enforcementMode: EnforcementStrict, want: false},
		{name: "read/filter: no accumulation", operation: OperationRead, enforcementMode: EnforcementFilter, want: false},
		{name: "read-write/strict: no accumulation", operation: OperationReadWrite, enforcementMode: EnforcementStrict, want: false},
		{name: "read-write/filter: no accumulation", operation: OperationReadWrite, enforcementMode: EnforcementFilter, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ShouldAccumulateReadLabels(tt.operation, tt.enforcementMode))
		})
	}
}

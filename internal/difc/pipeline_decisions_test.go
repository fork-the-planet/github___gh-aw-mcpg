package difc

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errorLabeledData struct{}

func (e *errorLabeledData) Overall() *LabeledResource { return NewLabeledResource("error") }
func (e *errorLabeledData) ToResult() (interface{}, error) {
	return nil, errors.New("to result failed")
}

func TestEvaluateCoarseAccess(t *testing.T) {
	t.Parallel()

	evaluator := NewEvaluator() // strict mode

	tests := []struct {
		name           string
		agentSecrecy   *SecrecyLabel
		agentIntegrity *IntegrityLabel
		resource       *LabeledResource
		operation      OperationType
		wantOutcome    CoarseCheckOutcome
	}{
		{
			name: "allowed: agent has clearance for read",
			agentSecrecy: func() *SecrecyLabel {
				l := NewSecrecyLabel()
				l.Label.Add("secret")
				return l
			}(),
			agentIntegrity: NewIntegrityLabel(),
			resource: func() *LabeledResource {
				r := NewLabeledResource("test-resource")
				r.Secrecy.Label.Add("secret")
				return r
			}(),
			operation:   OperationRead,
			wantOutcome: CoarseAllowed,
		},
		{
			name:           "bypass for read: agent lacks clearance but operation is read",
			agentSecrecy:   NewSecrecyLabel(),
			agentIntegrity: NewIntegrityLabel(),
			resource: func() *LabeledResource {
				r := NewLabeledResource("test-resource")
				r.Secrecy.Label.Add("secret")
				return r
			}(),
			operation:   OperationRead,
			wantOutcome: CoarseBypassForRead,
		},
		{
			name: "denied: agent carries secret data that cannot flow to public resource (write)",
			agentSecrecy: func() *SecrecyLabel {
				l := NewSecrecyLabel()
				l.Label.Add("secret")
				return l
			}(),
			agentIntegrity: NewIntegrityLabel(),
			resource:       NewLabeledResource("public-resource"),
			operation:      OperationWrite,
			wantOutcome:    CoarseDenied,
		},
		{
			name:           "denied: agent lacks clearance and operation is read-write",
			agentSecrecy:   NewSecrecyLabel(),
			agentIntegrity: NewIntegrityLabel(),
			resource: func() *LabeledResource {
				r := NewLabeledResource("test-resource")
				r.Secrecy.Label.Add("secret")
				return r
			}(),
			operation:   OperationReadWrite,
			wantOutcome: CoarseDenied,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			outcome, result := EvaluateCoarseAccess(evaluator, tt.agentSecrecy, tt.agentIntegrity, tt.resource, tt.operation)
			assert.Equal(t, tt.wantOutcome, outcome)
			require.NotNil(t, result)
			if outcome == CoarseAllowed {
				assert.True(t, result.IsAllowed())
			} else {
				assert.False(t, result.IsAllowed())
			}
		})
	}
}

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
		tt := tt
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
		tt := tt
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ShouldBlockFilteredResponse(tt.enforcementMode, tt.filteredCount))
		})
	}
}

func TestFilterAndConvertLabeledData(t *testing.T) {
	t.Parallel()

	makePrivateItem := func(id int) LabeledItem {
		labels := NewLabeledResource("private")
		labels.Secrecy.Label.Add("private:restricted/repo")
		return LabeledItem{Data: map[string]interface{}{"id": id}, Labels: labels}
	}
	makePublicItem := func(id int) LabeledItem {
		return LabeledItem{Data: map[string]interface{}{"id": id}, Labels: NewLabeledResource("public")}
	}

	evaluator := NewEvaluator()
	agentSecrecy := NewSecrecyLabel()
	agentIntegrity := NewIntegrityLabel()

	t.Run("nil labeled data returns empty result", func(t *testing.T) {
		t.Parallel()
		result, err := FilterAndConvertLabeledData(evaluator, agentSecrecy, agentIntegrity, OperationRead, nil, EnforcementFilter)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Nil(t, result.FinalResult)
		assert.Nil(t, result.Filtered)
		assert.False(t, result.Blocked)
	})

	t.Run("simple labeled data converts directly", func(t *testing.T) {
		t.Parallel()
		labeled := &SimpleLabeledData{Data: map[string]interface{}{"ok": true}, Labels: NewLabeledResource("simple")}
		result, err := FilterAndConvertLabeledData(evaluator, agentSecrecy, agentIntegrity, OperationRead, labeled, EnforcementFilter)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, map[string]interface{}{"ok": true}, result.FinalResult)
		assert.Nil(t, result.Filtered)
		assert.False(t, result.Blocked)
	})

	t.Run("strict mode blocks filtered collection", func(t *testing.T) {
		t.Parallel()
		collection := &CollectionLabeledData{
			Items: []LabeledItem{
				makePublicItem(1),
				makePrivateItem(2),
			},
		}
		result, err := FilterAndConvertLabeledData(evaluator, agentSecrecy, agentIntegrity, OperationRead, collection, EnforcementStrict)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Blocked)
		require.NotNil(t, result.Filtered)
		assert.Equal(t, 1, result.Filtered.GetFilteredCount())
		assert.Nil(t, result.FinalResult)
	})

	t.Run("filter mode returns partial collection result", func(t *testing.T) {
		t.Parallel()
		collection := &CollectionLabeledData{
			Items: []LabeledItem{
				makePublicItem(1),
				makePrivateItem(2),
			},
		}
		result, err := FilterAndConvertLabeledData(evaluator, agentSecrecy, agentIntegrity, OperationRead, collection, EnforcementFilter)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.Blocked)
		require.NotNil(t, result.Filtered)
		assert.Equal(t, 1, result.Filtered.GetAccessibleCount())
		assert.Equal(t, 1, result.Filtered.GetFilteredCount())
		require.IsType(t, []interface{}{}, result.FinalResult)
		finalItems := result.FinalResult.([]interface{})
		require.Len(t, finalItems, 1)
		assert.Equal(t, 1, finalItems[0].(map[string]interface{})["id"])
	})

	t.Run("propagate mode allows collection without blocking or filtering", func(t *testing.T) {
		t.Parallel()
		collection := &CollectionLabeledData{
			Items: []LabeledItem{
				makePublicItem(1),
				makePrivateItem(2),
			},
		}
		eval := NewEvaluatorWithMode(EnforcementPropagate)
		result, err := FilterAndConvertLabeledData(eval, agentSecrecy, agentIntegrity, OperationRead, collection, EnforcementPropagate)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.Blocked)
		require.NotNil(t, result.Filtered)
		assert.Equal(t, 0, result.Filtered.GetFilteredCount())
		require.IsType(t, []interface{}{}, result.FinalResult)
		finalItems := result.FinalResult.([]interface{})
		require.Len(t, finalItems, 2)
	})

	t.Run("to result conversion errors are returned", func(t *testing.T) {
		t.Parallel()
		result, err := FilterAndConvertLabeledData(evaluator, agentSecrecy, agentIntegrity, OperationRead, &errorLabeledData{}, EnforcementFilter)
		require.Error(t, err)
		assert.Nil(t, result)
	})
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ShouldAccumulateReadLabels(tt.operation, tt.enforcementMode))
		})
	}
}

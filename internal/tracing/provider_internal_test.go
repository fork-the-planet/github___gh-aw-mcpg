package tracing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergeOTLPHeaders(t *testing.T) {
	tests := []struct {
		name     string
		shared   map[string]string
		specific map[string]string
		want     map[string]string
	}{
		{
			name: "both nil returns nil",
			want: nil,
		},
		{
			name:   "only shared headers",
			shared: map[string]string{"Authorization": "shared"},
			want:   map[string]string{"Authorization": "shared"},
		},
		{
			name:     "only specific headers",
			specific: map[string]string{"X-Trace-Id": "trace-123"},
			want:     map[string]string{"X-Trace-Id": "trace-123"},
		},
		{
			name:     "non-overlapping maps are combined",
			shared:   map[string]string{"Authorization": "shared"},
			specific: map[string]string{"X-Trace-Id": "trace-123"},
			want: map[string]string{
				"Authorization": "shared",
				"X-Trace-Id":    "trace-123",
			},
		},
		{
			name:     "specific overrides shared",
			shared:   map[string]string{"Authorization": "shared"},
			specific: map[string]string{"Authorization": "specific"},
			want:     map[string]string{"Authorization": "specific"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mergeOTLPHeaders(tt.shared, tt.specific))
		})
	}
}

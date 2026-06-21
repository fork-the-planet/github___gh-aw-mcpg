package mcpresult

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeContentItems(t *testing.T) {
	t.Parallel()

	textItem := map[string]interface{}{"type": "text", "text": "hello"}
	audioItem := map[string]interface{}{"type": "audio", "text": "skip"}

	tests := []struct {
		name    string
		content interface{}
		want    []map[string]interface{}
		wantOK  bool
	}{
		{
			name: "supports []interface{}",
			content: []interface{}{
				textItem,
				audioItem,
			},
			want:   []map[string]interface{}{textItem, audioItem},
			wantOK: true,
		},
		{
			name: "supports []map[string]interface{}",
			content: []map[string]interface{}{
				textItem,
				audioItem,
			},
			want:   []map[string]interface{}{textItem, audioItem},
			wantOK: true,
		},
		{
			name: "skips non-map items in []interface{}",
			content: []interface{}{
				"not a map",
				nil,
				textItem,
				42,
			},
			want:   []map[string]interface{}{textItem},
			wantOK: true,
		},
		{
			name:    "rejects unsupported content types",
			content: map[string]interface{}{"type": "text"},
			want:    nil,
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := NormalizeContentItems(tt.content)

			require.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

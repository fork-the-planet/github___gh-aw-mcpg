package tracing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseOTLPHeaders covers the parseOTLPHeaders helper with a range of inputs.
func TestParseOTLPHeaders(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:  "single well-formed pair",
			input: "Authorization=Bearer test-token",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
			},
		},
		{
			name:  "multiple well-formed pairs",
			input: "Authorization=Bearer test-token,X-Request-ID=req-123",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
				"X-Request-ID":  "req-123",
			},
		},
		{
			name:  "whitespace is trimmed around keys and values",
			input: " Authorization = Bearer test-token , X-Request-ID = req-123 ",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
				"X-Request-ID":  "req-123",
			},
		},
		{
			name:  "value containing '=' is preserved",
			input: "Authorization=Basic dXNlcjpwYXNz==",
			expected: map[string]string{
				"Authorization": "Basic dXNlcjpwYXNz==",
			},
		},
		{
			name:  "malformed pair without '=' is skipped",
			input: "Authorization=Bearer test-token,malformed,X-Trace-ID=trace-123",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
				"X-Trace-ID":    "trace-123",
			},
		},
		{
			name:  "pair with empty key is skipped",
			input: "Authorization=Bearer test-token,=empty-key,X-Trace-ID=trace-123",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
				"X-Trace-ID":    "trace-123",
			},
		},
		{
			name:  "pair with whitespace-only key is skipped",
			input: "Authorization=Bearer test-token,  =whitespace-key",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
			},
		},
		{
			name:  "empty trailing comma is skipped",
			input: "Authorization=Bearer test-token,",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseOTLPHeaders(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

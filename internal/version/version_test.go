package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSet(t *testing.T) {
	tests := []struct {
		name           string
		initial        string
		inputVersion   string
		expectedResult string
	}{
		{
			name:           "set valid version",
			initial:        "0.0.0-dev",
			inputVersion:   "v1.2.3",
			expectedResult: "v1.2.3",
		},
		{
			name:           "set version with build metadata",
			initial:        "0.0.0-dev",
			inputVersion:   "v1.2.3, commit: abc1234, built: 2024-01-01",
			expectedResult: "v1.2.3, commit: abc1234, built: 2024-01-01",
		},
		{
			name:           "empty string preserves default version",
			initial:        "0.0.0-dev",
			inputVersion:   "",
			expectedResult: "0.0.0-dev", // should remain default
		},
		{
			name:           "empty string preserves non-default version",
			initial:        "v1.0.0",
			inputVersion:   "",
			expectedResult: "v1.0.0",
		},
		{
			name:           "replaces existing non-default version",
			initial:        "v1.0.0",
			inputVersion:   "v2.0.0",
			expectedResult: "v2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := gatewayVersion
			t.Cleanup(func() { gatewayVersion = original })

			gatewayVersion = tt.initial
			Set(tt.inputVersion)
			assert.Equal(t, tt.expectedResult, Get())
		})
	}
}

func TestGet(t *testing.T) {
	t.Run("returns default version", func(t *testing.T) {
		original := gatewayVersion
		t.Cleanup(func() { gatewayVersion = original })

		gatewayVersion = "0.0.0-dev"
		assert.Equal(t, "0.0.0-dev", Get())
	})

	t.Run("returns updated version after Set", func(t *testing.T) {
		original := gatewayVersion
		t.Cleanup(func() { gatewayVersion = original })

		gatewayVersion = "0.0.0-dev"
		Set("v2.0.0")
		assert.Equal(t, "v2.0.0", Get(), "Version should be updated to 'v2.0.0'")
	})

	t.Run("is idempotent", func(t *testing.T) {
		original := gatewayVersion
		t.Cleanup(func() { gatewayVersion = original })

		gatewayVersion = "v3.1.4"
		assert.Equal(t, Get(), Get(), "consecutive Get() calls should return the same value")
	})
}

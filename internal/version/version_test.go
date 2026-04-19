package version

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestBuildVersionString(t *testing.T) {
	t.Run("all fields provided", func(t *testing.T) {
		got := BuildVersionString("v1.2.3", "abc1234", "2024-01-01")
		assert.Equal(t, "v1.2.3, commit: abc1234, built: 2024-01-01", got)
	})

	t.Run("empty mainVersion uses dev prefix", func(t *testing.T) {
		got := BuildVersionString("", "abc1234", "2024-01-01")
		assert.Equal(t, "dev, commit: abc1234, built: 2024-01-01", got)
	})

	t.Run("mainVersion and gitCommit without buildDate", func(t *testing.T) {
		got := BuildVersionString("v1.2.3", "abc1234", "")
		// buildDate is empty so build info fallback may or may not add a date
		assert.True(t, strings.HasPrefix(got, "v1.2.3, commit: abc1234"),
			"result should start with version and commit: got %q", got)
	})

	t.Run("mainVersion and buildDate without gitCommit", func(t *testing.T) {
		got := BuildVersionString("v1.2.3", "", "2024-01-01")
		// gitCommit is empty so build info fallback may or may not add a commit
		assert.True(t, strings.HasPrefix(got, "v1.2.3"),
			"result should start with version: got %q", got)
		assert.True(t, strings.HasSuffix(got, "built: 2024-01-01"),
			"result should end with built date: got %q", got)
	})

	t.Run("only mainVersion", func(t *testing.T) {
		got := BuildVersionString("v2.0.0", "", "")
		// No explicit commit or date; build info fallback may add them
		assert.True(t, strings.HasPrefix(got, "v2.0.0"),
			"result should start with mainVersion: got %q", got)
	})

	t.Run("all empty uses dev", func(t *testing.T) {
		got := BuildVersionString("", "", "")
		assert.True(t, strings.HasPrefix(got, "dev"),
			"result should start with 'dev' when mainVersion is empty: got %q", got)
	})

	t.Run("result is comma-separated", func(t *testing.T) {
		got := BuildVersionString("v1.0.0", "deadbeef", "2025-06-01")
		parts := strings.Split(got, ", ")
		require.GreaterOrEqual(t, len(parts), 3, "should have at least 3 comma-separated parts")
		assert.Equal(t, "v1.0.0", parts[0])
		assert.Equal(t, "commit: deadbeef", parts[1])
		assert.Equal(t, "built: 2025-06-01", parts[2])
	})

	t.Run("explicit gitCommit is not truncated", func(t *testing.T) {
		longHash := "abcdef1234567890"
		got := BuildVersionString("v1.0.0", longHash, "2024-01-01")
		// When gitCommit is explicitly provided it is used as-is (no truncation)
		assert.Contains(t, got, "commit: "+longHash)
	})
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

package version

import (
	"runtime/debug"
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

func makeTestBuildInfo(settings map[string]string) *debug.BuildInfo {
	info := &debug.BuildInfo{}
	for k, v := range settings {
		info.Settings = append(info.Settings, debug.BuildSetting{Key: k, Value: v})
	}
	return info
}

func TestVCSCommitFromBuildInfo(t *testing.T) {
	tests := []struct {
		name      string
		buildInfo *debug.BuildInfo
		settings  map[string]string
		want      string
	}{
		{
			name:      "nil build info returns empty string",
			buildInfo: nil,
			want:      "",
		},
		{
			name:     "vcs.revision present with short hash (≤7 chars)",
			settings: map[string]string{"vcs.revision": "abc1234"},
			want:     "abc1234",
		},
		{
			name:     "vcs.revision present with full 40-char hash is truncated to 7",
			settings: map[string]string{"vcs.revision": "abcdef1234567890abcdef1234567890abcdef12"},
			want:     "abcdef1",
		},
		{
			name:     "vcs.revision present with exactly 7 chars is not truncated",
			settings: map[string]string{"vcs.revision": "1234567"},
			want:     "1234567",
		},
		{
			name:     "vcs.revision present with 8 chars is truncated to 7",
			settings: map[string]string{"vcs.revision": "12345678"},
			want:     "1234567",
		},
		{
			name:     "vcs.revision absent returns empty string",
			settings: map[string]string{"vcs.time": "2024-01-01"},
			want:     "",
		},
		{
			name:     "empty settings returns empty string",
			settings: map[string]string{},
			want:     "",
		},
		{
			name:     "vcs.revision with empty value returns empty string",
			settings: map[string]string{"vcs.revision": ""},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildInfo := tt.buildInfo
			if buildInfo == nil && tt.settings != nil {
				buildInfo = makeTestBuildInfo(tt.settings)
			}

			got := vcsCommitFromBuildInfo(buildInfo)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVCSTimeFromBuildInfo(t *testing.T) {
	tests := []struct {
		name      string
		buildInfo *debug.BuildInfo
		settings  map[string]string
		want      string
	}{
		{
			name:      "nil build info returns empty string",
			buildInfo: nil,
			want:      "",
		},
		{
			name:     "vcs.time present returns value",
			settings: map[string]string{"vcs.time": "2024-01-15T10:30:00Z"},
			want:     "2024-01-15T10:30:00Z",
		},
		{
			name:     "vcs.time absent returns empty string",
			settings: map[string]string{"vcs.revision": "abc1234"},
			want:     "",
		},
		{
			name:     "empty settings returns empty string",
			settings: map[string]string{},
			want:     "",
		},
		{
			name:     "vcs.time with empty value returns empty string",
			settings: map[string]string{"vcs.time": ""},
			want:     "",
		},
		{
			name:     "both vcs settings present returns time",
			settings: map[string]string{"vcs.revision": "abc1234", "vcs.time": "2024-06-01T00:00:00Z"},
			want:     "2024-06-01T00:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildInfo := tt.buildInfo
			if buildInfo == nil && tt.settings != nil {
				buildInfo = makeTestBuildInfo(tt.settings)
			}

			got := vcsTimeFromBuildInfo(buildInfo)
			assert.Equal(t, tt.want, got)
		})
	}
}

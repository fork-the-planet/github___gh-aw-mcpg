package main

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/version"
	"github.com/stretchr/testify/assert"
)

func TestBuildVersionString(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		gitCommit      string
		buildDate      string
		expectedParts  []string
		unexpectedPart string
	}{
		// Normal scenarios
		{
			name:          "all metadata present",
			version:       "v1.0.0",
			gitCommit:     "abc123",
			buildDate:     "2026-01-21T10:00:00Z",
			expectedParts: []string{"v1.0.0", "commit: abc123", "built: 2026-01-21T10:00:00Z"},
		},
		{
			name:          "only version",
			version:       "v1.0.0",
			gitCommit:     "",
			buildDate:     "",
			expectedParts: []string{"v1.0.0"},
		},
		{
			name:          "version with commit",
			version:       "v1.0.0",
			gitCommit:     "abc123",
			buildDate:     "",
			expectedParts: []string{"v1.0.0", "commit: abc123"},
		},
		{
			name:          "version with build date",
			version:       "v1.0.0",
			gitCommit:     "",
			buildDate:     "2026-01-21T10:00:00Z",
			expectedParts: []string{"v1.0.0", "built: 2026-01-21T10:00:00Z"},
		},
		// Edge case: Empty version
		{
			name:           "no version defaults to dev",
			version:        "",
			gitCommit:      "",
			buildDate:      "",
			expectedParts:  []string{"dev"},
			unexpectedPart: "commit:",
		},
		// Edge case: Long commit hash truncation
		{
			name:          "long commit hash not truncated by buildVersionString",
			version:       "v2.0.0",
			gitCommit:     "abcdef1234567890",
			buildDate:     "",
			expectedParts: []string{"v2.0.0", "commit: abcdef1234567890"},
		},
		// Edge case: Special characters
		{
			name:          "version with special characters",
			version:       "v1.0.0-beta+build.123",
			gitCommit:     "abc-123",
			buildDate:     "2026-01-21T10:00:00Z",
			expectedParts: []string{"v1.0.0-beta+build.123", "commit: abc-123", "built: 2026-01-21T10:00:00Z"},
		},
		// Edge case: Whitespace in inputs
		{
			name:          "whitespace in version metadata",
			version:       " v1.0.0 ",
			gitCommit:     " abc123 ",
			buildDate:     " 2026-01-21T10:00:00Z ",
			expectedParts: []string{" v1.0.0 ", "commit:  abc123 ", "built:  2026-01-21T10:00:00Z "},
		},
		// Edge case: Only commit
		{
			name:          "only commit without version",
			version:       "",
			gitCommit:     "xyz789",
			buildDate:     "",
			expectedParts: []string{"dev", "commit: xyz789"},
		},
		// Edge case: Only build date
		{
			name:          "only build date without version",
			version:       "",
			gitCommit:     "",
			buildDate:     "2026-02-09T14:00:00Z",
			expectedParts: []string{"dev", "built: 2026-02-09T14:00:00Z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)

			// Set test values
			origVersion := Version
			origGitCommit := GitCommit
			origBuildDate := BuildDate
			t.Cleanup(func() {
				Version = origVersion
				GitCommit = origGitCommit
				BuildDate = origBuildDate
			})

			Version = tt.version
			GitCommit = tt.gitCommit
			BuildDate = tt.buildDate

			result := version.BuildVersionString(Version, GitCommit, BuildDate)

			// Check expected parts
			for _, part := range tt.expectedParts {
				assert.Contains(result, part, "Version string should contain: %s", part)
			}

			// Check unexpected parts
			if tt.unexpectedPart != "" {
				assert.NotContains(result, tt.unexpectedPart, "Version string should not contain: %s", tt.unexpectedPart)
			}

			// Ensure parts are comma-separated when multiple
			if len(tt.expectedParts) > 1 {
				assert.Contains(result, ", ", "Multi-part version should be comma-separated")
			}
		})
	}
}

func TestBuildVersionString_UsesVCSInfo(t *testing.T) {
	// When ldflags are not set, buildVersionString should fall back to VCS info from runtime/debug
	// This test verifies the code structure, actual VCS extraction depends on build settings
	assert := assert.New(t)

	// Set empty values to trigger VCS fallback
	origVersion := Version
	origGitCommit := GitCommit
	origBuildDate := BuildDate
	t.Cleanup(func() {
		Version = origVersion
		GitCommit = origGitCommit
		BuildDate = origBuildDate
	})

	Version = ""
	GitCommit = ""
	BuildDate = ""

	result := version.BuildVersionString(Version, GitCommit, BuildDate)

	// Should at least have "dev"
	assert.Contains(result, "dev", "Should contain 'dev' when Version is empty")

	// May or may not have commit/built info depending on VCS availability
	// Just verify it doesn't panic or return empty string
	assert.NotEmpty(result, "Should return non-empty string")
}

func TestBuildVersionString_CommitHashShortening(t *testing.T) {
	// Verify that commit hash shortening logic is properly triggered
	// Note: The shortening happens in the VCS fallback path, not when GitCommit is set directly
	assert := assert.New(t)

	origVersion := Version
	origGitCommit := GitCommit
	origBuildDate := BuildDate
	t.Cleanup(func() {
		Version = origVersion
		GitCommit = origGitCommit
		BuildDate = origBuildDate
	})

	// When GitCommit is set via ldflags, it's used as-is (no shortening)
	Version = "v1.0.0"
	GitCommit = "1234567890abcdefghijklmnop" // Very long hash
	BuildDate = ""

	result := version.BuildVersionString(Version, GitCommit, BuildDate)

	// The full commit hash should be present (no shortening in direct path)
	assert.Contains(result, "commit: 1234567890abcdefghijklmnop", "Should include full commit hash when set via GitCommit variable")
}

func TestBuildVersionString_OutputFormat(t *testing.T) {
	// Verify the exact output format and structure
	tests := []struct {
		name           string
		version        string
		gitCommit      string
		buildDate      string
		expectedOutput string
	}{
		{
			name:           "single field",
			version:        "v1.0.0",
			gitCommit:      "",
			buildDate:      "",
			expectedOutput: "v1.0.0",
		},
		{
			name:           "two fields",
			version:        "v1.0.0",
			gitCommit:      "abc123",
			buildDate:      "",
			expectedOutput: "v1.0.0, commit: abc123",
		},
		{
			name:           "all three fields",
			version:        "v1.0.0",
			gitCommit:      "abc123",
			buildDate:      "2026-01-21T10:00:00Z",
			expectedOutput: "v1.0.0, commit: abc123, built: 2026-01-21T10:00:00Z",
		},
		{
			name:           "only dev",
			version:        "",
			gitCommit:      "",
			buildDate:      "",
			expectedOutput: "dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)

			origVersion := Version
			origGitCommit := GitCommit
			origBuildDate := BuildDate
			t.Cleanup(func() {
				Version = origVersion
				GitCommit = origGitCommit
				BuildDate = origBuildDate
			})

			Version = tt.version
			GitCommit = tt.gitCommit
			BuildDate = tt.buildDate

			result := version.BuildVersionString(Version, GitCommit, BuildDate)

			// For VCS fallback case, we can't guarantee exact output
			if tt.version == "" && tt.gitCommit == "" && tt.buildDate == "" {
				assert.Contains(result, "dev", "Should contain dev for empty inputs")
				return
			}

			assert.Equal(tt.expectedOutput, result, "Output format should match expected")
		})
	}
}

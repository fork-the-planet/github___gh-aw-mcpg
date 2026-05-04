package sys

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDockerBinary creates a fake docker binary in a temp directory that outputs
// a fixed response and returns a cleanup function. The caller must prepend the
// returned directory to PATH via t.Setenv before calling any docker-dependent code.
//
// output is what the fake docker script prints to stdout.
// exitCode controls whether the fake docker succeeds (0) or fails (non-zero).
func mockDockerBinary(t *testing.T, output string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")

	content := fmt.Sprintf("#!/bin/sh\nprintf '%%s' '%s'\nexit %d\n", output, exitCode)
	require.NoError(t, os.WriteFile(script, []byte(content), 0o755))
	return dir
}

// prependPath prepends dir to the current PATH for the duration of the test.
func prependPath(t *testing.T, dir string) {
	t.Helper()
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+origPath)
}

// TestCheckPortMapping_SuccessPath verifies the happy-path branches of CheckPortMapping
// where docker inspect returns well-formed port-mapping JSON.
func TestCheckPortMapping_SuccessPath(t *testing.T) {
	tests := []struct {
		name        string
		dockerOut   string
		containerID string
		port        string
		wantMapped  bool
		wantErr     bool
	}{
		{
			name:        "port is mapped",
			dockerOut:   `{"8080/tcp":[{"HostIp":"0.0.0.0","HostPort":"8080"}]}`,
			containerID: "abc123def4567890",
			port:        "8080",
			wantMapped:  true,
			wantErr:     false,
		},
		{
			name:        "port not mapped - portKey absent",
			dockerOut:   `{"9090/tcp":[{"HostIp":"0.0.0.0","HostPort":"9090"}]}`,
			containerID: "abc123def4567890",
			port:        "8080",
			wantMapped:  false,
			wantErr:     false,
		},
		{
			name:        "port key present but no HostPort binding",
			dockerOut:   `{"8080/tcp":null}`,
			containerID: "abc123def4567890",
			port:        "8080",
			wantMapped:  false,
			wantErr:     false,
		},
		{
			name:        "empty output - no ports",
			dockerOut:   `{}`,
			containerID: "abc123def4567890",
			port:        "8080",
			wantMapped:  false,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := mockDockerBinary(t, tt.dockerOut, 0)
			prependPath(t, dir)

			mapped, err := CheckPortMapping(tt.containerID, tt.port)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantMapped, mapped)
			}
		})
	}
}

// TestCheckStdinInteractive_SuccessPath verifies the happy-path branches of
// CheckStdinInteractive where docker inspect returns "true" or "false".
func TestCheckStdinInteractive_SuccessPath(t *testing.T) {
	tests := []struct {
		name        string
		dockerOut   string
		containerID string
		want        bool
	}{
		{
			name:        "container has stdin interactive (true)",
			dockerOut:   "true",
			containerID: "abc123def4567890",
			want:        true,
		},
		{
			name:        "container does not have stdin interactive (false)",
			dockerOut:   "false",
			containerID: "abc123def4567890",
			want:        false,
		},
		{
			name:        "unexpected output treated as non-interactive",
			dockerOut:   "unexpected",
			containerID: "abc123def4567890",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := mockDockerBinary(t, tt.dockerOut, 0)
			prependPath(t, dir)

			result := CheckStdinInteractive(tt.containerID)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestCheckLogDirMounted_SuccessPath verifies the happy-path branches of
// CheckLogDirMounted where docker inspect returns mount JSON.
func TestCheckLogDirMounted_SuccessPath(t *testing.T) {
	tests := []struct {
		name        string
		dockerOut   string
		containerID string
		logDir      string
		want        bool
	}{
		{
			name:        "log dir is mounted",
			dockerOut:   `[{"Type":"bind","Source":"/tmp/gh-aw/mcp-logs","Destination":"/tmp/gh-aw/mcp-logs"}]`,
			containerID: "abc123def4567890",
			logDir:      "/tmp/gh-aw/mcp-logs",
			want:        true,
		},
		{
			name:        "log dir not in mounts",
			dockerOut:   `[{"Type":"bind","Source":"/other/path","Destination":"/other/path"}]`,
			containerID: "abc123def4567890",
			logDir:      "/tmp/gh-aw/mcp-logs",
			want:        false,
		},
		{
			name:        "no mounts at all",
			dockerOut:   `[]`,
			containerID: "abc123def4567890",
			logDir:      "/tmp/gh-aw/mcp-logs",
			want:        false,
		},
		{
			name:        "partial path match is not a match",
			dockerOut:   `[{"Type":"bind","Source":"/tmp/gh-aw","Destination":"/tmp/gh-aw"}]`,
			containerID: "abc123def4567890",
			logDir:      "/tmp/gh-aw/mcp-logs",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := mockDockerBinary(t, tt.dockerOut, 0)
			prependPath(t, dir)

			result := CheckLogDirMounted(tt.containerID, tt.logDir)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestRunDockerInspect_SuccessPath tests the success path of runDockerInspect
// where docker inspect runs successfully and returns output.
func TestRunDockerInspect_SuccessPath(t *testing.T) {
	t.Run("returns trimmed output on success", func(t *testing.T) {
		dir := mockDockerBinary(t, "  some output  ", 0)
		prependPath(t, dir)

		output, err := runDockerInspect("abc123def4567890", "{{.Config}}")
		require.NoError(t, err)
		assert.Equal(t, "some output", output)
	})

	t.Run("returns empty string for empty docker output", func(t *testing.T) {
		dir := mockDockerBinary(t, "", 0)
		prependPath(t, dir)

		output, err := runDockerInspect("abc123def4567890", "{{.Config}}")
		require.NoError(t, err)
		assert.Equal(t, "", output)
	})
}

package sys

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateContainerID_SecurityCritical verifies the security-critical container ID validation.
// Container IDs must be 12–64 lowercase hex characters (a-f, 0-9).
func TestValidateContainerID_SecurityCritical(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty string",
			id:      "",
			wantErr: true,
			errMsg:  "container ID is empty",
		},
		{
			name:    "valid 12-char short form",
			id:      "abc123def456",
			wantErr: false,
		},
		{
			name:    "valid 64-char full form",
			id:      "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			wantErr: false,
		},
		{
			name:    "valid 32-char intermediate",
			id:      "deadbeefcafe1234deadbeefcafe1234",
			wantErr: false,
		},
		{
			name:    "uppercase letters rejected",
			id:      "ABC123DEF456",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "mixed case rejected",
			id:      "abc123DEF456",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "non-hex characters rejected",
			id:      "xyz123abc456",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "hyphens rejected",
			id:      "abc123-def456",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "spaces rejected",
			id:      "abc123 def456",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "too short (11 chars) - rejected by pattern",
			id:      "abc123def45",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "too long (65 chars) - rejected by pattern",
			id:      "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "slash injection attempt",
			id:      "abc123; rm -rf /",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "newline injection attempt",
			id:      "abc123def456\n",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "all zeros (valid)",
			id:      "000000000000",
			wantErr: false,
		},
		{
			name:    "all 'f' chars (valid)",
			id:      "ffffffffffff",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateContainerID(tt.id)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRunDockerInspect(t *testing.T) {
	tests := []struct {
		name           string
		containerID    string
		formatTemplate string
		shouldError    bool
	}{
		{
			name:           "empty container ID",
			containerID:    "",
			formatTemplate: "{{.Config.OpenStdin}}",
			shouldError:    true,
		},
		{
			name:           "invalid container ID - too short",
			containerID:    "abc123",
			formatTemplate: "{{.Config.OpenStdin}}",
			shouldError:    true,
		},
		{
			name:           "invalid container ID - special chars",
			containerID:    "abc;def123456",
			formatTemplate: "{{.Config.OpenStdin}}",
			shouldError:    true,
		},
		{
			name:           "valid container ID format - command will fail without docker",
			containerID:    "abcdef123456",
			formatTemplate: "{{.Config.OpenStdin}}",
			shouldError:    true, // Will fail because container doesn't exist
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runDockerInspect(tt.containerID, tt.formatTemplate)

			if tt.shouldError {
				assert.Error(t, err, "Expected error but got none")
				assert.Empty(t, output, "Expected empty output on error")
			} else {
				assert.NoError(t, err, "Unexpected error")
			}
		})
	}
}

func TestCheckDockerAccessible(t *testing.T) {
	t.Run("check docker accessibility", func(t *testing.T) {
		// This test verifies the function runs without panicking
		// In CI environments, Docker may or may not be available
		result := CheckDockerAccessible()
		t.Logf("Docker accessible: %v", result)
		// We don't assert the result since Docker availability varies by environment
	})

	t.Run("with custom DOCKER_HOST", func(t *testing.T) {
		// Test with a custom DOCKER_HOST that doesn't exist
		originalHost := os.Getenv("DOCKER_HOST")
		defer func() {
			if originalHost != "" {
				os.Setenv("DOCKER_HOST", originalHost)
			} else {
				os.Unsetenv("DOCKER_HOST")
			}
		}()

		os.Setenv("DOCKER_HOST", "unix:///nonexistent/docker.sock")
		result := CheckDockerAccessible()
		assert.False(t, result, "Should return false for nonexistent socket")
	})

	t.Run("with unix:// prefix in DOCKER_HOST", func(t *testing.T) {
		originalHost := os.Getenv("DOCKER_HOST")
		defer func() {
			if originalHost != "" {
				os.Setenv("DOCKER_HOST", originalHost)
			} else {
				os.Unsetenv("DOCKER_HOST")
			}
		}()

		// Set DOCKER_HOST with unix:// prefix
		os.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
		// Function should strip the unix:// prefix and check the path
		CheckDockerAccessible()
		// If it doesn't panic, the prefix stripping works
	})
}

func TestCheckPortMapping(t *testing.T) {
	tests := []struct {
		name        string
		containerID string
		port        string
		shouldError bool
	}{
		{
			name:        "empty container ID",
			containerID: "",
			port:        "8080",
			shouldError: true,
		},
		{
			name:        "invalid container ID",
			containerID: "invalid;id",
			port:        "8080",
			shouldError: true,
		},
		{
			name:        "valid container ID format - nonexistent container",
			containerID: "abcdef123456",
			port:        "8080",
			shouldError: true, // Will fail because container doesn't exist
		},
		{
			name:        "empty port",
			containerID: "abcdef123456",
			port:        "",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapped, err := CheckPortMapping(tt.containerID, tt.port)

			if tt.shouldError {
				assert.Error(t, err, "Expected error for %s", tt.name)
				assert.False(t, mapped, "Port should not be mapped on error")
			} else {
				assert.NoError(t, err, "Unexpected error")
			}
		})
	}
}

func TestCheckStdinInteractive(t *testing.T) {
	tests := []struct {
		name        string
		containerID string
		expected    bool
	}{
		{
			name:        "empty container ID",
			containerID: "",
			expected:    false,
		},
		{
			name:        "invalid container ID",
			containerID: "invalid;id",
			expected:    false,
		},
		{
			name:        "valid container ID format - nonexistent container",
			containerID: "abcdef123456",
			expected:    false, // Will fail because container doesn't exist
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckStdinInteractive(tt.containerID)
			assert.Equal(t, tt.expected, result, "Unexpected result for %s", tt.name)
		})
	}
}

func TestCheckLogDirMounted(t *testing.T) {
	tests := []struct {
		name        string
		containerID string
		logDir      string
		expected    bool
	}{
		{
			name:        "empty container ID",
			containerID: "",
			logDir:      "/tmp/gh-aw/mcp-logs",
			expected:    false,
		},
		{
			name:        "invalid container ID",
			containerID: "invalid;id",
			logDir:      "/tmp/gh-aw/mcp-logs",
			expected:    false,
		},
		{
			name:        "valid container ID format - nonexistent container",
			containerID: "abcdef123456",
			logDir:      "/tmp/gh-aw/mcp-logs",
			expected:    false, // Will fail because container doesn't exist
		},
		{
			name:        "empty log directory",
			containerID: "abcdef123456",
			logDir:      "",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckLogDirMounted(tt.containerID, tt.logDir)
			assert.Equal(t, tt.expected, result, "Unexpected result for %s", tt.name)
		})
	}
}

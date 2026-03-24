package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateContainerID verifies the security-critical container ID validation.
// Container IDs must be 12–64 lowercase hex characters (a-f, 0-9).
func TestValidateContainerID(t *testing.T) {
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
			name:    "too short (11 chars)",
			id:      "abc123def45",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "too long (65 chars)",
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
			err := validateContainerID(tt.id)
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

// TestGetGatewayPortFromEnv tests the env-based gateway port parsing.
func TestGetGatewayPortFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		envSet   bool
		wantPort int
		wantErr  bool
		errMsg   string
	}{
		{
			name:    "env var not set",
			envSet:  false,
			wantErr: true,
			errMsg:  "MCP_GATEWAY_PORT environment variable not set",
		},
		{
			name:     "env var set to empty string",
			envValue: "",
			envSet:   true,
			wantErr:  true,
			errMsg:   "MCP_GATEWAY_PORT environment variable not set",
		},
		{
			name:     "invalid integer value",
			envValue: "not-a-number",
			envSet:   true,
			wantErr:  true,
			errMsg:   "invalid MCP_GATEWAY_PORT value",
		},
		{
			name:     "port zero (out of range)",
			envValue: "0",
			envSet:   true,
			wantErr:  true,
		},
		{
			name:     "port 65536 (out of range)",
			envValue: "65536",
			envSet:   true,
			wantErr:  true,
		},
		{
			name:     "negative port",
			envValue: "-1",
			envSet:   true,
			wantErr:  true,
		},
		{
			name:     "valid port 3000",
			envValue: "3000",
			envSet:   true,
			wantPort: 3000,
			wantErr:  false,
		},
		{
			name:     "valid port 1",
			envValue: "1",
			envSet:   true,
			wantPort: 1,
			wantErr:  false,
		},
		{
			name:     "valid port 65535 (max)",
			envValue: "65535",
			envSet:   true,
			wantPort: 65535,
			wantErr:  false,
		},
		{
			name:     "valid port 8080",
			envValue: "8080",
			envSet:   true,
			wantPort: 8080,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envSet {
				t.Setenv("MCP_GATEWAY_PORT", tt.envValue)
			} else {
				os.Unsetenv("MCP_GATEWAY_PORT")
			}

			port, err := GetGatewayPortFromEnv()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				assert.Equal(t, 0, port)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPort, port)
			}
		})
	}
}

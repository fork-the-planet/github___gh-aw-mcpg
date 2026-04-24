package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetGatewayPortFromEnv tests the env-based gateway port parsing.
func TestGetGatewayPortFromEnv_Comprehensive(t *testing.T) {
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

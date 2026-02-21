package config

import (
	"fmt"
	"strconv"

	"github.com/github/gh-aw-mcpg/internal/config/rules"
	"github.com/github/gh-aw-mcpg/internal/envutil"
)

// GetGatewayPortFromEnv returns the MCP_GATEWAY_PORT value, parsed as int
func GetGatewayPortFromEnv() (int, error) {
	portStr := envutil.GetEnvString("MCP_GATEWAY_PORT", "")
	if portStr == "" {
		return 0, fmt.Errorf("MCP_GATEWAY_PORT environment variable not set")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("invalid MCP_GATEWAY_PORT value: %s", portStr)
	}

	if validationErr := rules.PortRange(port, "MCP_GATEWAY_PORT"); validationErr != nil {
		return 0, fmt.Errorf("%s", validationErr.Message)
	}

	return port, nil
}

// GetGatewayDomainFromEnv returns the MCP_GATEWAY_DOMAIN value
func GetGatewayDomainFromEnv() string {
	return envutil.GetEnvString("MCP_GATEWAY_DOMAIN", "")
}

// GetGatewayAPIKeyFromEnv returns the MCP_GATEWAY_API_KEY value
func GetGatewayAPIKeyFromEnv() string {
	return envutil.GetEnvString("MCP_GATEWAY_API_KEY", "")
}

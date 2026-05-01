package config

import (
	"fmt"
	"strconv"

	"github.com/github/gh-aw-mcpg/internal/config/rules"
	"github.com/github/gh-aw-mcpg/internal/envutil"
)

// ToolTimeoutMin is the minimum allowed value for toolTimeout (seconds).
const ToolTimeoutMin = 10

// ToolTimeoutMax is the maximum allowed value for toolTimeout (seconds).
const ToolTimeoutMax = 600

// GetGatewayPortFromEnv returns the MCP_GATEWAY_PORT value, parsed as int
func GetGatewayPortFromEnv() (int, error) {
	portStr := envutil.GetEnvString("MCP_GATEWAY_PORT", "")
	if portStr == "" {
		logConfig.Print("MCP_GATEWAY_PORT not set in environment")
		return 0, fmt.Errorf("MCP_GATEWAY_PORT environment variable not set")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		logConfig.Printf("MCP_GATEWAY_PORT=%q is not a valid integer: %v", portStr, err)
		return 0, fmt.Errorf("invalid MCP_GATEWAY_PORT value: %s", portStr)
	}

	if validationErr := rules.PortRange(port, "MCP_GATEWAY_PORT"); validationErr != nil {
		logConfig.Printf("MCP_GATEWAY_PORT=%d is outside valid port range: %s", port, validationErr.Message)
		return 0, fmt.Errorf("%s", validationErr.Message)
	}

	logConfig.Printf("MCP_GATEWAY_PORT resolved to %d", port)
	return port, nil
}

// GetGatewayDomainFromEnv returns the MCP_GATEWAY_DOMAIN value
func GetGatewayDomainFromEnv() string {
	domain := envutil.GetEnvString("MCP_GATEWAY_DOMAIN", "")
	if domain != "" {
		logConfig.Printf("MCP_GATEWAY_DOMAIN=%q", domain)
	}
	return domain
}

// GetGatewayAPIKeyFromEnv returns the MCP_GATEWAY_API_KEY value
func GetGatewayAPIKeyFromEnv() string {
	key := envutil.GetEnvString("MCP_GATEWAY_API_KEY", "")
	if key != "" {
		logConfig.Print("MCP_GATEWAY_API_KEY found in environment")
	} else {
		logConfig.Print("MCP_GATEWAY_API_KEY not set in environment")
	}
	return key
}

// GetGatewayToolTimeoutFromEnv returns the MCP_GATEWAY_TOOL_TIMEOUT value, parsed as int.
// Returns (0, false) when the environment variable is not set or empty.
// Returns an error when the variable is set but invalid (non-integer or out of bounds [10, 600]).
func GetGatewayToolTimeoutFromEnv() (int, bool, error) {
	timeoutStr := envutil.GetEnvString("MCP_GATEWAY_TOOL_TIMEOUT", "")
	if timeoutStr == "" {
		logConfig.Print("MCP_GATEWAY_TOOL_TIMEOUT not set in environment")
		return 0, false, nil
	}

	timeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		logConfig.Printf("MCP_GATEWAY_TOOL_TIMEOUT=%q is not a valid integer: %v", timeoutStr, err)
		return 0, false, fmt.Errorf("invalid MCP_GATEWAY_TOOL_TIMEOUT value: %s", timeoutStr)
	}

	if validationErr := rules.TimeoutRange(timeout, ToolTimeoutMin, ToolTimeoutMax, "MCP_GATEWAY_TOOL_TIMEOUT", "MCP_GATEWAY_TOOL_TIMEOUT"); validationErr != nil {
		logConfig.Printf("MCP_GATEWAY_TOOL_TIMEOUT=%d is outside valid range [%d, %d]: %s", timeout, ToolTimeoutMin, ToolTimeoutMax, validationErr.Message)
		return 0, false, fmt.Errorf("%s", validationErr.Message)
	}

	logConfig.Printf("MCP_GATEWAY_TOOL_TIMEOUT resolved to %d", timeout)
	return timeout, true, nil
}

// toolTimeoutEnvOrDefault returns the value of MCP_GATEWAY_TOOL_TIMEOUT when set and valid,
// otherwise DefaultToolTimeout. Invalid env var values are logged and ignored (fallback to default).
func toolTimeoutEnvOrDefault() int {
	timeout, ok, err := GetGatewayToolTimeoutFromEnv()
	if err != nil {
		logConfig.Printf("MCP_GATEWAY_TOOL_TIMEOUT is invalid, falling back to default %d: %v", DefaultToolTimeout, err)
		return DefaultToolTimeout
	}
	if !ok {
		return DefaultToolTimeout
	}
	return timeout
}

package cmd

// Logging-related flags

import (
	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/spf13/cobra"
)

// Logging flag defaults
const (
	defaultPayloadPathPrefix = "" // Empty by default - use actual filesystem path
)

// Logging flag variables
var (
	logDir               string
	payloadDir           string
	payloadPathPrefix    string
	payloadSizeThreshold int
)

func init() {
	RegisterFlag(func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&logDir, "log-dir", getDefaultLogDir(), "Directory for log files (falls back to stdout if directory cannot be created)")
		cmd.Flags().StringVar(&payloadDir, "payload-dir", getDefaultPayloadDir(), "Directory for storing large payload files (segmented by session ID)")
		cmd.Flags().StringVar(&payloadPathPrefix, "payload-path-prefix", getDefaultPayloadPathPrefix(), "Path prefix to use when returning payloadPath to clients (allows remapping host paths to client/agent container paths)")
		cmd.Flags().IntVar(&payloadSizeThreshold, "payload-size-threshold", getDefaultPayloadSizeThreshold(), "Size threshold (in bytes) for storing payloads to disk. Payloads larger than this are stored, smaller ones returned inline")
	})
}

// getDefaultLogDir returns the default log directory, checking MCP_GATEWAY_LOG_DIR
// environment variable first, then falling back to the hardcoded default
func getDefaultLogDir() string {
	return envutil.GetEnvString("MCP_GATEWAY_LOG_DIR", config.DefaultLogDir)
}

// getDefaultPayloadDir returns the default payload directory, checking MCP_GATEWAY_PAYLOAD_DIR
// environment variable first, then falling back to the hardcoded default
func getDefaultPayloadDir() string {
	return envutil.GetEnvString("MCP_GATEWAY_PAYLOAD_DIR", config.DefaultPayloadDir)
}

// getDefaultPayloadPathPrefix returns the default payload path prefix, checking MCP_GATEWAY_PAYLOAD_PATH_PREFIX
// environment variable first, then falling back to the hardcoded default
func getDefaultPayloadPathPrefix() string {
	return envutil.GetEnvString("MCP_GATEWAY_PAYLOAD_PATH_PREFIX", defaultPayloadPathPrefix)
}

// getDefaultPayloadSizeThreshold returns the default payload size threshold, checking
// MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD environment variable first, then falling back to the hardcoded default
func getDefaultPayloadSizeThreshold() int {
	return envutil.GetEnvInt("MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD", config.DefaultPayloadSizeThreshold)
}

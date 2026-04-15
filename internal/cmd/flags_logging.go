package cmd

// Logging-related flags

import (
	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/spf13/cobra"
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
		cmd.Flags().StringVar(&logDir, "log-dir", envutil.GetEnvString("MCP_GATEWAY_LOG_DIR", config.DefaultLogDir), "Directory for log files (falls back to stdout if directory cannot be created)")
		cmd.Flags().StringVar(&payloadDir, "payload-dir", envutil.GetEnvString("MCP_GATEWAY_PAYLOAD_DIR", config.DefaultPayloadDir), "Directory for storing large payload files (segmented by session ID)")
		cmd.Flags().StringVar(&payloadPathPrefix, "payload-path-prefix", envutil.GetEnvString("MCP_GATEWAY_PAYLOAD_PATH_PREFIX", ""), "Path prefix to use when returning payloadPath to clients (allows remapping host paths to client/agent container paths)")
		cmd.Flags().IntVar(&payloadSizeThreshold, "payload-size-threshold", envutil.GetEnvInt("MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD", config.DefaultPayloadSizeThreshold), "Size threshold (in bytes) for storing payloads to disk. Payloads larger than this are stored, smaller ones returned inline")
	})
}

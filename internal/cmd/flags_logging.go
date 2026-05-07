package cmd

// Logging-related flags

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/spf13/cobra"
)

const wasmCacheDirEnvVar = "MCP_GATEWAY_WASM_CACHE_DIR"

// Logging flag variables
var (
	logDir               string
	payloadDir           string
	payloadPathPrefix    string
	payloadSizeThreshold int
	wasmCacheDir         string
)

func trimmedValue(value string) string {
	return strings.TrimSpace(value)
}

func defaultWasmCacheDir(logDir string) string {
	return filepath.Join(logDir, config.DefaultWasmCacheDirName)
}

func resolveWasmCacheDir(flagChanged bool, flagValue, effectiveLogDir string) string {
	if trimmed := trimmedValue(flagValue); flagChanged && trimmed != "" {
		return trimmed
	}

	if envValue, exists := os.LookupEnv(wasmCacheDirEnvVar); exists {
		if trimmed := trimmedValue(envValue); trimmed != "" {
			return trimmed
		}
	}

	return defaultWasmCacheDir(effectiveLogDir)
}

func init() {
	RegisterFlag(func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&logDir, "log-dir", envutil.GetEnvString("MCP_GATEWAY_LOG_DIR", config.DefaultLogDir), "Directory for log files (falls back to stdout if directory cannot be created)")
		cmd.Flags().StringVar(&payloadDir, "payload-dir", envutil.GetEnvString("MCP_GATEWAY_PAYLOAD_DIR", config.DefaultPayloadDir), "Directory for storing large payload files (segmented by session ID)")
		cmd.Flags().StringVar(&wasmCacheDir, "wasm-cache-dir", resolveWasmCacheDir(false, "", envutil.GetEnvString("MCP_GATEWAY_LOG_DIR", config.DefaultLogDir)), "Directory for disk-backed wazero compilation cache (default: <log-dir>/wazero-cache)")
		cmd.Flags().StringVar(&payloadPathPrefix, "payload-path-prefix", envutil.GetEnvString("MCP_GATEWAY_PAYLOAD_PATH_PREFIX", ""), "Path prefix to use when returning payloadPath to clients (allows remapping host paths to client/agent container paths)")
		cmd.Flags().IntVar(&payloadSizeThreshold, "payload-size-threshold", envutil.GetEnvInt("MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD", config.DefaultPayloadSizeThreshold), "Size threshold (in bytes) for storing payloads to disk. Payloads larger than this are stored, smaller ones returned inline")
	})
}

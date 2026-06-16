package cmd

// HTTP server lifecycle flags

import (
	"time"

	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/spf13/cobra"
)

const (
	defaultShutdownTimeout = 5 * time.Second
	shutdownTimeoutEnvVar  = "MCP_GATEWAY_SHUTDOWN_TIMEOUT"
)

// shutdownTimeout is the maximum time the HTTP server waits for in-flight
// requests to complete before forcefully closing connections on shutdown.
// Its initial value is set by the DurationVar registration in init() below,
// which reads MCP_GATEWAY_SHUTDOWN_TIMEOUT (if set) before any command runs.
var shutdownTimeout time.Duration

func init() {
	RegisterFlag(func(cmd *cobra.Command) {
		cmd.Flags().DurationVar(
			&shutdownTimeout,
			"shutdown-timeout",
			envutil.GetEnvDuration(shutdownTimeoutEnvVar, defaultShutdownTimeout),
			"Maximum time to wait for in-flight requests to complete during graceful shutdown (e.g. 30s, 2m)",
		)
	})
}

package cmd

// TLS and HMAC security flags (ASI-07: Secure Agent↔Gateway Communication)

import (
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/spf13/cobra"
)

// TLS/HMAC flag variables
var (
	tlsCertPath string
	tlsKeyPath  string
	tlsCAPath   string
	hmacSecret  string
)

func init() {
	RegisterFlag(func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&tlsCertPath, "tls-cert", envutil.GetEnvString("MCP_GATEWAY_TLS_CERT", ""), "Path to TLS server certificate PEM file (enables HTTPS)")
		cmd.Flags().StringVar(&tlsKeyPath, "tls-key", envutil.GetEnvString("MCP_GATEWAY_TLS_KEY", ""), "Path to TLS server private key PEM file (enables HTTPS)")
		cmd.Flags().StringVar(&tlsCAPath, "tls-ca", envutil.GetEnvString("MCP_GATEWAY_CA_CERT", ""), "Path to CA certificate PEM file for client certificate verification (enables mTLS)")
		cmd.Flags().StringVar(&hmacSecret, "hmac-secret", envutil.GetEnvString("MCP_GATEWAY_HMAC_SECRET", ""), "Shared HMAC-SHA256 secret for request signing and replay protection")
		// Cert and key must always be provided together when set via CLI flags.
		// Note: env-var defaults (MCP_GATEWAY_TLS_CERT/MCP_GATEWAY_TLS_KEY) are
		// validated at runtime in run() since MarkFlagsRequiredTogether only fires
		// when flags are explicitly changed on the command line.
		cmd.MarkFlagsRequiredTogether("tls-cert", "tls-key")
	})
}

package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/proxy"
	"github.com/spf13/cobra"
)

var logProxyCmd = logger.New("cmd:proxy")

var tlsTrustEnvKeys = []string{
	"NODE_EXTRA_CA_CERTS",
	"SSL_CERT_FILE",
	"GIT_SSL_CAINFO",
	"CURL_CA_BUNDLE",
	"REQUESTS_CA_BUNDLE",
}

// Proxy subcommand flag variables
var (
	proxyGuardWasm      string
	proxyPolicy         string
	proxyToken          string
	proxyListen         string
	proxyLogDir         string
	proxyWasmCacheDir   string
	proxyDIFCMode       string
	proxyAPIURL         string
	proxyTLS            bool
	proxyTLSDir         string
	proxyTrustedBots    []string
	proxyTrustedUsers   []string
	proxyOTLPEndpoint   string
	proxyOTLPService    string
	proxyOTLPSampleRate float64
)

func init() {
	rootCmd.AddCommand(newProxyCmd())
}

func newProxyCmd() *cobra.Command {
	defaultGuard := detectGuardWasm()
	defaultProxyLogDir := envutil.GetEnvString("MCP_GATEWAY_LOG_DIR", config.DefaultLogDir)

	cmd := &cobra.Command{
		Use:     "proxy",
		GroupID: "modes",
		Short:   "Run as a GitHub API filtering proxy",
		Long: `Run the gateway in proxy mode — an HTTP(S) forward proxy that intercepts
gh CLI requests and applies DIFC filtering using the same guard WASM module.

Container usage (uses baked-in guard automatically):

  docker run --rm -p 8443:8443 \
    -e GITHUB_TOKEN \
    -v /tmp/proxy-logs:/tmp/gh-aw/mcp-logs \
    ghcr.io/github/gh-aw-mcpg:latest proxy \
    --policy '{"allow-only":{"repos":["org/repo"],"min-integrity":"approved"}}' \
    --listen 0.0.0.0:8443 \
    --tls

  # Trust the CA cert from the mounted volume
  export GH_HOST=localhost:8443
  export NODE_EXTRA_CA_CERTS=/tmp/proxy-logs/proxy-tls/ca.crt
  gh issue list -R org/repo

Local usage:

  awmg proxy \
    --guard-wasm guards/github-guard/github_guard.wasm \
    --policy '{"allow-only":{"repos":["org/repo"],"min-integrity":"approved"}}' \
    --listen localhost:8443 --tls`,
		Example: `  # Run with auto-detected baked-in guard (container image)
  awmg proxy --policy '{"allow-only":{"repos":["org/repo"],"min-integrity":"approved"}}'

  # Run locally with explicit guard WASM and TLS
  awmg proxy \
    --guard-wasm guards/github-guard/github_guard.wasm \
    --policy '{"allow-only":{"repos":["org/repo"]}}' \
    --listen localhost:8443 --tls`,
		SilenceUsage: true,
		RunE:         runProxy,
	}

	guardHelp := "Path to the guard WASM module"
	if defaultGuard != "" {
		guardHelp += " (auto-detected: " + defaultGuard + ")"
	} else {
		guardHelp += " (required)"
	}
	// Note: --listen and --log-dir are re-declared here (not inherited from rootCmd as
	// persistent flags) because the proxy subcommand has different defaults and a distinct
	// purpose: it runs as a standalone HTTPS forward proxy, not an MCP gateway. Keeping
	// them independent avoids confusion and allows each command to evolve separately.
	cmd.Flags().StringVar(&proxyGuardWasm, "guard-wasm", defaultGuard, guardHelp)
	cmd.Flags().StringVar(&proxyPolicy, "policy", os.Getenv("MCP_GATEWAY_GUARD_POLICY_JSON"), "Guard policy JSON")
	cmd.Flags().StringVar(&proxyToken, "github-token", "", "Fallback GitHub API token (default: forwards client Authorization header)")
	cmd.Flags().StringVarP(&proxyListen, "listen", "l", "127.0.0.1:8080", "Proxy listen address")
	cmd.Flags().StringVar(&proxyLogDir, "log-dir", defaultProxyLogDir, "Log file directory")
	cmd.Flags().StringVar(&proxyWasmCacheDir, "wasm-cache-dir", resolveWasmCacheDir(false, "", defaultProxyLogDir), "Directory for disk-backed wazero compilation cache (default: sibling of <log-dir>, named wazero-cache)")
	cmd.Flags().StringVar(&proxyDIFCMode, "guards-mode", difc.DefaultEnforcementMode(), "DIFC enforcement mode: strict, filter, propagate")
	cmd.Flags().StringVar(&proxyAPIURL, "github-api-url", "", "Upstream GitHub API URL (default: auto-derived from GITHUB_API_URL or GITHUB_SERVER_URL, falls back to https://api.github.com)")
	cmd.Flags().BoolVar(&proxyTLS, "tls", false, "Enable HTTPS with auto-generated self-signed certificates")
	cmd.Flags().StringVar(&proxyTLSDir, "tls-dir", "", "Directory for TLS certificates (default: <log-dir>/proxy-tls)")
	cmd.Flags().StringSliceVar(&proxyTrustedBots, "trusted-bots", nil, "Additional trusted bot usernames (comma-separated, extends built-in list)")
	cmd.Flags().StringSliceVar(&proxyTrustedUsers, "trusted-users", nil, "User logins that receive approved integrity (comma-separated)")
	registerTracingFlags(cmd, &proxyOTLPEndpoint, &proxyOTLPService, &proxyOTLPSampleRate,
		"OTLP HTTP endpoint for trace export (e.g. http://localhost:4318). Tracing is disabled when empty.",
		"Service name reported in traces.",
		"Fraction of traces to sample and export (0.0–1.0).")

	// Only require --guard-wasm when no baked-in guard is available
	if defaultGuard == "" {
		cmd.MarkFlagRequired("guard-wasm")
	}

	// Enum completions for the proxy command's own DIFC flag.
	// Note: rootCmd registers guards-mode completion separately for the root
	// command's distinct flag definition; keep both registrations in place.
	cmd.RegisterFlagCompletionFunc("guards-mode", cobra.FixedCompletions(
		difc.ValidModes, cobra.ShellCompDirectiveNoFileComp))

	// Use MarkFlagDirname for directory flags (cobra best practice)
	for _, dirFlag := range []string{"log-dir", "wasm-cache-dir", "tls-dir"} {
		if err := cmd.MarkFlagDirname(dirFlag); err != nil {
			logProxyCmd.Printf("Failed to register --%s dirname completion: %v", dirFlag, err)
		}
	}

	return cmd
}

func runProxy(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logProxyCmd.Printf("Starting proxy: listen=%s, guard=%s, mode=%s, tls=%v", proxyListen, proxyGuardWasm, proxyDIFCMode, proxyTLS)

	if _, err := difc.ParseEnforcementMode(proxyDIFCMode); err != nil {
		return fmt.Errorf("invalid --guards-mode flag: %w", err)
	}

	// Initialize loggers
	logger.InitProxyLoggers(proxyLogDir)

	logger.LogInfo("startup", "MCPG Proxy starting: listen=%s, guard=%s, mode=%s, tls=%v", proxyListen, proxyGuardWasm, proxyDIFCMode, proxyTLS)

	resolvedWasmCacheDir, err := configureWasmCompilationCache(ctx, cmd.Flags().Changed("wasm-cache-dir"), proxyWasmCacheDir, proxyLogDir, logger.StartupWarn)
	if err != nil {
		return err
	}
	cleanupCtx := context.WithoutCancel(ctx)
	defer func() {
		if err := guard.CloseGlobalCompilationCache(cleanupCtx); err != nil {
			logger.LogError("shutdown", "Failed to close WASM compilation cache: %v", err)
		}
	}()
	logger.LogInfo("startup", "WASM compilation cache directory: %s", resolvedWasmCacheDir)

	// Initialize OpenTelemetry tracer provider for the proxy server.
	// When no endpoint is configured, a noop provider is used (zero overhead).
	var tracingCfg *config.TracingConfig
	if proxyOTLPEndpoint != "" {
		tracingCfg = &config.TracingConfig{
			Endpoint:    proxyOTLPEndpoint,
			ServiceName: proxyOTLPService,
			SampleRate:  &proxyOTLPSampleRate,
		}
	}
	// Provider enablement logging remains tied to explicit proxy flag configuration.
	_, cleanupTracing := setupCommandTracing(
		ctx,
		tracingCfg,
		"failed to initialize tracing provider: %v",
		logTracingWarnf,
		logTracingWarnf,
	)
	defer cleanupTracing()
	if tracingCfg != nil {
		logger.StartupInfo("OpenTelemetry tracing enabled for proxy: endpoint=%s, service=%s", proxyOTLPEndpoint, proxyOTLPService)
	} else {
		logger.StartupInfo("OpenTelemetry tracing disabled for proxy (no --otlp-endpoint configured)")
	}

	// Resolve GitHub token (optional — proxy forwards client auth by default)
	token := proxyToken
	if token == "" {
		token = envutil.LookupGitHubToken()
	}
	if token != "" {
		logger.LogInfo("startup", "Fallback GitHub token configured from flag/env")
	} else {
		logger.LogInfo("startup", "No fallback token — proxy will forward client Authorization headers")
	}

	// Resolve GitHub API URL: flag → env vars → default
	apiURL := proxyAPIURL
	if apiURL == "" {
		apiURL = envutil.DeriveGitHubAPIURL("")
	}
	if apiURL == "" {
		apiURL = proxy.DefaultGitHubAPIBase
	}
	logger.LogInfo("startup", "Upstream GitHub API URL: %s", apiURL)
	logProxyCmd.Printf("Resolved GitHub API URL: %s, explicit flag=%v", apiURL, proxyAPIURL != "")

	// Create the proxy server
	logProxyCmd.Printf("Creating proxy server: guard=%s, hasPolicy=%v, mode=%s, trustedBots=%d, trustedUsers=%d",
		proxyGuardWasm, proxyPolicy != "", proxyDIFCMode, len(proxyTrustedBots), len(proxyTrustedUsers))
	proxySrv, err := proxy.New(ctx, proxy.Config{
		WasmPath:     proxyGuardWasm,
		Policy:       proxyPolicy,
		GitHubToken:  token,
		GitHubAPIURL: apiURL,
		DIFCMode:     proxyDIFCMode,
		TrustedBots:  proxyTrustedBots,
		TrustedUsers: proxyTrustedUsers,
	})
	if err != nil {
		return fmt.Errorf("failed to create proxy server: %w", err)
	}
	logProxyCmd.Printf("Proxy server created successfully")

	// Generate TLS certificates if requested
	var tlsCfg *proxy.TLSConfig
	if proxyTLS {
		tlsDir := proxyTLSDir
		if tlsDir == "" {
			tlsDir = filepath.Join(proxyLogDir, "proxy-tls")
		}
		logProxyCmd.Printf("Generating TLS certificates in: %s", tlsDir)
		tlsCfg, err = proxy.GenerateSelfSignedTLS(tlsDir)
		if err != nil {
			return fmt.Errorf("failed to generate TLS certificates: %w", err)
		}
		if err := configureTLSTrustEnvironment(tlsCfg.CACertPath); err != nil {
			return err
		}
		logger.LogInfo("startup", "TLS certificates generated: ca=%s", tlsCfg.CACertPath)
	}

	// Create the HTTP server
	logProxyCmd.Printf("Creating HTTP server: addr=%s, tls=%v", proxyListen, tlsCfg != nil)
	httpServer := &http.Server{
		Addr:    proxyListen,
		Handler: proxySrv.Handler(),
	}
	if tlsCfg != nil {
		logProxyCmd.Printf("Applying TLS configuration to HTTP server")
		httpServer.TLSConfig = tlsCfg.Config
	}

	err = serveAndWait(
		ctx,
		cancel,
		httpServer,
		shutdownTimeout,
		func() {
			logger.LogInfoToMarkdown("shutdown", "Shutting down proxy...")
		},
		func() error {
			listener, err := net.Listen("tcp", proxyListen)
			if err != nil {
				return fmt.Errorf("failed to listen on %s: %w", proxyListen, err)
			}

			if tlsCfg != nil {
				listener = tls.NewListener(listener, tlsCfg.Config)
			}

			actualAddr := listener.Addr().String()
			scheme := "http"
			if tlsCfg != nil {
				scheme = "https"
			}

			logger.StartupInfo("Proxy listening on %s://%s", scheme, actualAddr)

			// Print connection info
			stderr := cmd.ErrOrStderr()
			fmt.Fprintf(stderr, "\nMCPG GitHub API Proxy\n")
			fmt.Fprintf(stderr, "  Listening: %s://%s\n", scheme, actualAddr)
			fmt.Fprintf(stderr, "  Upstream:  %s\n", apiURL)
			fmt.Fprintf(stderr, "  Mode:      %s\n", proxyDIFCMode)
			fmt.Fprintf(stderr, "  Guard:     %s\n", proxyGuardWasm)
			if tlsCfg != nil {
				fmt.Fprintf(stderr, "  CA cert:   %s\n", tlsCfg.CACertPath)
				fmt.Fprintf(stderr, "\nConnect with:\n")
				fmt.Fprintf(stderr, "  export GH_HOST=%s\n", clientAddr(actualAddr))
				fmt.Fprintf(stderr, "  export NODE_EXTRA_CA_CERTS=%s\n", tlsCfg.CACertPath)
				fmt.Fprintf(stderr, "  export SSL_CERT_FILE=%s\n", tlsCfg.CACertPath)
				fmt.Fprintf(stderr, "  export GIT_SSL_CAINFO=%s\n", tlsCfg.CACertPath)
				fmt.Fprintf(stderr, "  gh issue list -R org/repo\n\n")
			} else {
				fmt.Fprintf(stderr, "\nConnect with:\n")
				fmt.Fprintf(stderr, "  curl http://%s/repos/org/repo/issues\n\n", actualAddr)
			}

			return httpServer.Serve(listener)
		},
	)

	if err != nil {
		logger.LogError("shutdown", "Proxy server exited with error: %v", err)
		return err
	}

	return nil
}

func configureTLSTrustEnvironment(caCertPath string) error {
	if strings.ContainsAny(caCertPath, "\r\n") {
		return fmt.Errorf("invalid TLS CA cert path contains newline")
	}

	logProxyCmd.Printf("Configuring TLS trust environment: caCertPath=%s, envVars=%v", caCertPath, tlsTrustEnvKeys)
	for _, key := range tlsTrustEnvKeys {
		if err := os.Setenv(key, caCertPath); err != nil {
			return fmt.Errorf("failed to set %s: %w", key, err)
		}
	}
	logProxyCmd.Printf("TLS trust environment configured successfully: %d env vars set", len(tlsTrustEnvKeys))
	return nil
}

// clientAddr returns a client-friendly address from a listener address.
// When the host is a wildcard (0.0.0.0, ::, or empty), it substitutes
// "localhost" so the printed GH_HOST value is usable from a client.
//
// Note: output.go uses "127.0.0.1" for the same wildcard substitution in
// the gateway config output, while this function uses "localhost" because
// GH_HOST must be a resolvable hostname for the gh CLI.
func clientAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return net.JoinHostPort("localhost", port)
	}
	return addr
}

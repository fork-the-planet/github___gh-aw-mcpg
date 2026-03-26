package cmd

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/proxy"
	"github.com/spf13/cobra"
)

// Proxy subcommand flag variables
var (
	proxyGuardWasm    string
	proxyPolicy       string
	proxyToken        string
	proxyListen       string
	proxyLogDir       string
	proxyDIFCMode     string
	proxyAPIURL       string
	proxyTLS          bool
	proxyTLSDir       string
	proxyTrustedBots  []string
	proxyTrustedUsers []string
)

func init() {
	rootCmd.AddCommand(newProxyCmd())
}

// containerGuardWasmPath is the baked-in guard path in the container image.
const containerGuardWasmPath = "/guards/github/00-github-guard.wasm"

// detectGuardWasm returns the baked-in container guard path if it exists,
// or empty string if not found (requiring the user to specify --guard-wasm).
func detectGuardWasm() string {
	if _, err := os.Stat(containerGuardWasmPath); err == nil {
		return containerGuardWasmPath
	}
	return ""
}

func newProxyCmd() *cobra.Command {
	defaultGuard := detectGuardWasm()

	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Run as a GitHub API filtering proxy",
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
		SilenceUsage: true,
		RunE:         runProxy,
	}

	guardHelp := "Path to the guard WASM module"
	if defaultGuard != "" {
		guardHelp += " (auto-detected: " + defaultGuard + ")"
	} else {
		guardHelp += " (required)"
	}
	cmd.Flags().StringVar(&proxyGuardWasm, "guard-wasm", defaultGuard, guardHelp)
	cmd.Flags().StringVar(&proxyPolicy, "policy", os.Getenv("MCP_GATEWAY_GUARD_POLICY_JSON"), "Guard policy JSON")
	cmd.Flags().StringVar(&proxyToken, "github-token", "", "Fallback GitHub API token (default: forwards client Authorization header)")
	cmd.Flags().StringVarP(&proxyListen, "listen", "l", "127.0.0.1:8080", "Proxy listen address")
	cmd.Flags().StringVar(&proxyLogDir, "log-dir", getDefaultLogDir(), "Log file directory")
	cmd.Flags().StringVar(&proxyDIFCMode, "guards-mode", "filter", "DIFC enforcement mode: strict, filter, propagate")
	cmd.Flags().StringVar(&proxyAPIURL, "github-api-url", "", "Upstream GitHub API URL (default: auto-derived from GITHUB_API_URL or GITHUB_SERVER_URL, falls back to https://api.github.com)")
	cmd.Flags().BoolVar(&proxyTLS, "tls", false, "Enable HTTPS with auto-generated self-signed certificates")
	cmd.Flags().StringVar(&proxyTLSDir, "tls-dir", "", "Directory for TLS certificates (default: <log-dir>/proxy-tls)")
	cmd.Flags().StringSliceVar(&proxyTrustedBots, "trusted-bots", nil, "Additional trusted bot usernames (comma-separated, extends built-in list)")
	cmd.Flags().StringSliceVar(&proxyTrustedUsers, "trusted-users", nil, "User logins that receive approved integrity (comma-separated)")

	// Only require --guard-wasm when no baked-in guard is available
	if defaultGuard == "" {
		cmd.MarkFlagRequired("guard-wasm")
	}

	// Enum completions for proxy DIFC flag
	cmd.RegisterFlagCompletionFunc("guards-mode", cobra.FixedCompletions(
		[]string{"strict", "filter", "propagate"}, cobra.ShellCompDirectiveNoFileComp))

	return cmd
}

func runProxy(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := ValidateDIFCMode(proxyDIFCMode); err != nil {
		return fmt.Errorf("invalid --guards-mode flag: %w", err)
	}

	// Initialize loggers
	if err := logger.InitFileLogger(proxyLogDir, "proxy.log"); err != nil {
		log.Printf("Warning: Failed to initialize file logger: %v", err)
	}
	if err := logger.InitJSONLLogger(proxyLogDir, "rpc-messages.jsonl"); err != nil {
		log.Printf("Warning: Failed to initialize JSONL logger: %v", err)
	}

	logger.LogInfo("startup", "MCPG Proxy starting: listen=%s, guard=%s, mode=%s, tls=%v", proxyListen, proxyGuardWasm, proxyDIFCMode, proxyTLS)

	// Resolve GitHub token (optional — proxy forwards client auth by default)
	token := proxyToken
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")
	}
	if token != "" {
		logger.LogInfo("startup", "Fallback GitHub token configured from flag/env")
	} else {
		logger.LogInfo("startup", "No fallback token — proxy will forward client Authorization headers")
	}

	// Resolve GitHub API URL: flag → env vars → default
	apiURL := proxyAPIURL
	if apiURL == "" {
		apiURL = proxy.DeriveGitHubAPIURL()
	}
	if apiURL == "" {
		apiURL = proxy.DefaultGitHubAPIBase
	}
	logger.LogInfo("startup", "Upstream GitHub API URL: %s", apiURL)

	// Create the proxy server
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

	// Generate TLS certificates if requested
	var tlsCfg *proxy.TLSConfig
	if proxyTLS {
		tlsDir := proxyTLSDir
		if tlsDir == "" {
			tlsDir = filepath.Join(proxyLogDir, "proxy-tls")
		}
		tlsCfg, err = proxy.GenerateSelfSignedTLS(tlsDir)
		if err != nil {
			return fmt.Errorf("failed to generate TLS certificates: %w", err)
		}
		logger.LogInfo("startup", "TLS certificates generated: ca=%s", tlsCfg.CACertPath)
	}

	// Create the HTTP server
	httpServer := &http.Server{
		Addr:    proxyListen,
		Handler: proxySrv.Handler(),
	}
	if tlsCfg != nil {
		httpServer.TLSConfig = tlsCfg.Config
	}

	// Start server in background
	go func() {
		listener, err := net.Listen("tcp", proxyListen)
		if err != nil {
			log.Printf("Failed to listen on %s: %v", proxyListen, err)
			cancel()
			return
		}

		if tlsCfg != nil {
			listener = tls.NewListener(listener, tlsCfg.Config)
		}

		actualAddr := listener.Addr().String()
		scheme := "http"
		if tlsCfg != nil {
			scheme = "https"
		}

		log.Printf("MCPG Proxy listening on %s://%s", scheme, actualAddr)
		logger.LogInfo("startup", "Proxy listening on %s://%s", scheme, actualAddr)

		// Print connection info
		fmt.Fprintf(os.Stderr, "\nMCPG GitHub API Proxy\n")
		fmt.Fprintf(os.Stderr, "  Listening: %s://%s\n", scheme, actualAddr)
		fmt.Fprintf(os.Stderr, "  Mode:      %s\n", proxyDIFCMode)
		fmt.Fprintf(os.Stderr, "  Guard:     %s\n", proxyGuardWasm)
		if tlsCfg != nil {
			fmt.Fprintf(os.Stderr, "  CA cert:   %s\n", tlsCfg.CACertPath)
			fmt.Fprintf(os.Stderr, "\nConnect with:\n")
			fmt.Fprintf(os.Stderr, "  export GH_HOST=%s\n", clientAddr(actualAddr))
			fmt.Fprintf(os.Stderr, "  export NODE_EXTRA_CA_CERTS=%s\n", tlsCfg.CACertPath)
			fmt.Fprintf(os.Stderr, "  gh issue list -R org/repo\n\n")
		} else {
			fmt.Fprintf(os.Stderr, "\nConnect with:\n")
			fmt.Fprintf(os.Stderr, "  curl http://%s/repos/org/repo/issues\n\n", actualAddr)
		}

		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
			cancel()
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	log.Println("Shutting down proxy...")
	logger.LogInfo("shutdown", "Proxy shutting down")

	return httpServer.Close()
}

// clientAddr returns a client-friendly address from a listener address.
// When the host is a wildcard (0.0.0.0, ::, or empty), it substitutes
// "localhost" so the printed GH_HOST value is usable from a client.
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

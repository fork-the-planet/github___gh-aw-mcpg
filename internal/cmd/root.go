package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/github/gh-aw-mcpg/internal/auth"
	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/server"
	"github.com/github/gh-aw-mcpg/internal/tracing"
	"github.com/github/gh-aw-mcpg/internal/version"
	"github.com/spf13/cobra"
)

// Exported constants for use by other packages
const (
	// DefaultListenIPv4 is the default interface used by the HTTP server.
	DefaultListenIPv4 = "127.0.0.1"
	// DefaultListenPort is the default port used by the HTTP server.
	DefaultListenPort = "3000"
)

// Package-level variables that don't belong to a specific feature
var (
	debugLog = logger.New("cmd:root")
	// cliVersion stores the version string for Cobra's CLI version display.
	// This is kept separate from version.Get() because rootCmd.Version must be
	// set at initialization time (before SetVersion is called). We sync both
	// values in SetVersion() to maintain a single source of truth.
	cliVersion = "dev" // Default version, overridden by SetVersion
)

var rootCmd = &cobra.Command{
	Use:   "awmg",
	Short: "MCPG MCP proxy server",
	Long: `MCPG is a proxy server for Model Context Protocol (MCP) servers.
It provides routing, aggregation, and management of multiple MCP backend servers.`,
	Example: `  # Start in routed mode with a config file
  awmg --config config.toml --routed

  # Start in unified mode reading config from stdin
  cat config.json | awmg --config-stdin --unified --listen 0.0.0.0:3000

  # Run with debug logging
  DEBUG=* awmg --config config.toml`,
	Version:           cliVersion,
	Args:              cobra.NoArgs,
	SilenceUsage:      true, // Don't show help on runtime errors
	SilenceErrors:     true, // Prevent cobra from printing errors — Execute() caller handles display
	PersistentPreRunE: preRun,
	RunE:              run,
	PersistentPostRun: postRun,
}

func init() {
	// Set custom error prefix for better branding
	rootCmd.SetErrPrefix("MCPG Error:")

	// Set custom version template with enhanced formatting
	rootCmd.SetVersionTemplate(`MCPG Gateway {{.Version}}
`)

	// Register all flags from feature modules (flags_*.go files)
	registerAllFlags(rootCmd)

	// Register custom flag completions
	registerFlagCompletions(rootCmd)

	// Group subcommands for organized help output
	rootCmd.AddGroup(
		&cobra.Group{ID: "modes", Title: "Operation Modes:"},
		&cobra.Group{ID: "utils", Title: "Utilities:"},
	)

	// Add completion command
	rootCmd.AddCommand(newCompletionCmd())
}

const (
	// Debug log patterns for different verbosity levels
	debugMainPackages = "cmd:*,server:*,launcher:*"
	debugAllPackages  = "*"
)

// preRun performs validation before command execution
func preRun(cmd *cobra.Command, args []string) error {
	// Apply verbosity level to logging (if DEBUG is not already set)
	// -v (1): info level, -vv (2): debug level, -vvv (3): trace level
	debugEnv := os.Getenv(logger.EnvDebug)
	if verbosity > 0 && debugEnv == "" {
		// Set DEBUG env var based on verbosity level
		// Level 1: basic info (no special DEBUG setting needed, handled by logger)
		// Level 2: enable debug logs for cmd and server packages
		// Level 3: enable all debug logs
		switch verbosity {
		case 1:
			// Info level - no special DEBUG setting (standard log output)
			log.Printf("Logging level: info (-v)")
		case 2:
			// Debug level - enable debug logs for main packages
			os.Setenv("DEBUG", debugMainPackages)
			log.Printf("Logging level: debug (-vv), DEBUG=%s", debugMainPackages)
		default:
			// Trace level (3+) - enable all debug logs
			os.Setenv("DEBUG", debugAllPackages)
			log.Printf("Logging level: trace (-vvv), DEBUG=%s", debugAllPackages)
		}
	} else if debugEnv != "" {
		log.Printf("Logging level: DEBUG=%s (from environment)", debugEnv)
	}

	return nil
}

// postRun performs cleanup after command execution
func postRun(cmd *cobra.Command, args []string) {
	if err := logger.CloseAllLoggers(); err != nil {
		log.Printf("Warning: error closing loggers: %v", err)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Use signal.NotifyContext for proper cancellation on SIGINT/SIGTERM
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Load .env file if specified before resolving env-backed startup settings.
	if envFile != "" {
		debugLog.Printf("Loading environment from file: %s", envFile)
		if err := envutil.LoadEnvFile(envFile); err != nil {
			return fmt.Errorf("failed to load .env file: %w", err)
		}
	}

	logger.InitGatewayLoggers(logDir)

	logger.LogInfoMd("startup", "MCPG Gateway version: %s", cliVersion)

	// Log config source based on what was provided
	configSource := configFile
	if configStdin {
		configSource = "stdin"
	}
	logger.LogInfoMd("startup", "Starting MCPG with config: %s, listen: %s, log-dir: %s", configSource, listenAddr, logDir)
	debugLog.Printf("Starting MCPG with config: %s, listen: %s", configSource, listenAddr)

	resolvedWasmCacheDir, cacheErr := configureWasmCompilationCache(ctx, cmd.Flags().Changed("wasm-cache-dir"), wasmCacheDir, logDir, logger.StartupWarn)
	if cacheErr != nil {
		return cacheErr
	}
	logger.StartupInfo("WASM compilation cache directory: %s", resolvedWasmCacheDir)

	// Validate execution environment if requested
	if validateEnv {
		debugLog.Printf("Validating execution environment...")
		result := config.ValidateExecutionEnvironment()
		if !result.IsValid() {
			logger.LogErrorMd("startup", "Environment validation failed: %s", result.Error())
			return fmt.Errorf("environment validation failed: %s", result.Error())
		}
		logger.StartupInfo("Environment validation passed")
	}

	// Load configuration
	var cfg *config.Config
	var err error

	if configStdin {
		log.Println("Reading configuration from stdin...")
		cfg, err = config.LoadFromStdin()
	} else {
		log.Printf("Reading configuration from %s...", configFile)
		cfg, err = config.LoadFromFile(configFile)
	}

	if err != nil {
		// Log configuration validation errors to markdown logger
		logger.LogErrorMd("startup", "Configuration validation failed:\n%s", err.Error())
		return fmt.Errorf("failed to load config: %w", err)
	}

	debugLog.Printf("Configuration loaded with %d servers", len(cfg.Servers))
	log.Printf("Loaded %d MCP server(s)", len(cfg.Servers))

	// Log server names to markdown
	serverNames := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		serverNames = append(serverNames, name)
	}
	if len(serverNames) > 0 {
		logger.LogInfoMd("startup", "Loaded %d MCP server(s): %v", len(cfg.Servers), serverNames)
	} else {
		logger.LogInfoMd("startup", "Loaded %d MCP server(s)", len(cfg.Servers))
	}

	// Validate guards mode before applying
	if err := validateDIFCModeFlag(difcMode); err != nil {
		return err
	}

	// Apply command-line flags to config
	cfg.DIFCMode = difcMode
	cfg.SequentialLaunch = sequentialLaunch

	// Override gateway config with command-line flags
	if cfg.Gateway == nil {
		cfg.Gateway = &config.GatewayConfig{}
	}

	policyOverride, policySource, err := resolveGuardPolicyOverride(cmd)
	if err != nil {
		return fmt.Errorf("invalid guard policy configuration: %w", err)
	}
	if policyOverride != nil {
		cfg.GuardPolicy = policyOverride
		cfg.GuardPolicySource = policySource
		logger.StartupInfo("Guard policy override configured (source=%s)", policySource)
	}

	if envSinkServerIDs, exists := os.LookupEnv("MCP_GATEWAY_GUARDS_SINK_SERVER_IDS"); exists {
		logger.StartupInfo("MCP_GATEWAY_GUARDS_SINK_SERVER_IDS=%q", envSinkServerIDs)
	}

	resolvedSinkServerIDs, err := parseDIFCSinkServerIDs(difcSinkServerIDs)
	if err != nil {
		return fmt.Errorf("invalid --guards-sink-server-ids value: %w", err)
	}
	difc.SetSinkServerIDs(resolvedSinkServerIDs)
	if len(resolvedSinkServerIDs) == 0 {
		logger.StartupInfo("Guards sink server ID logging enrichment disabled (no sink server IDs configured)")
	} else {
		logger.StartupInfo("Guards sink server IDs configured for JSONL tag enrichment: %v", resolvedSinkServerIDs)
		for _, sinkServerID := range resolvedSinkServerIDs {
			if _, exists := cfg.Servers[sinkServerID]; !exists {
				logger.StartupWarn("Guards sink server ID '%s' is not configured in mcpServers", sinkServerID)
			}
		}
	}

	// Apply payload directory flag (if different from default, it was explicitly set)
	if cmd.Flags().Changed("payload-dir") {
		cfg.Gateway.PayloadDir = payloadDir
	} else if payloadDir != "" && payloadDir != config.DefaultPayloadDir {
		// Environment variable was set
		cfg.Gateway.PayloadDir = payloadDir
	}

	// Apply payload path prefix: CLI flag takes priority, then env-derived non-empty value.
	if cmd.Flags().Changed("payload-path-prefix") {
		cfg.Gateway.PayloadPathPrefix = payloadPathPrefix
	} else if payloadPathPrefix != "" {
		// envutil.GetEnvString returned a non-empty value from MCP_GATEWAY_PAYLOAD_PATH_PREFIX
		cfg.Gateway.PayloadPathPrefix = payloadPathPrefix
	}

	// Apply payload size threshold flag (if different from default, it was explicitly set)
	if cmd.Flags().Changed("payload-size-threshold") {
		cfg.Gateway.PayloadSizeThreshold = payloadSizeThreshold
	} else if payloadSizeThreshold != config.DefaultPayloadSizeThreshold {
		// Environment variable was set
		cfg.Gateway.PayloadSizeThreshold = payloadSizeThreshold
	}

	if sequentialLaunch {
		log.Println("Sequential server launching enabled")
	} else {
		log.Println("Parallel server launching enabled (default)")
	}

	// Determine mode (default to routed if neither flag is set)
	mode := "routed"
	if unifiedMode {
		mode = "unified"
	}

	debugLog.Printf("Server mode: %s, guards mode: %s", mode, cfg.DIFCMode)

	// Per spec §7.3: generate a random API key on startup if none is configured.
	// The generated key is set in the config so it propagates to both the HTTP
	// server authentication and the stdout configuration output (spec §5.4).
	if cfg.GetAPIKey() == "" {
		randomKey, err := auth.GenerateRandomAPIKey()
		if err != nil {
			return fmt.Errorf("failed to generate random API key: %w", err)
		}
		cfg.Gateway.APIKey = randomKey
		logger.StartupInfo("No API key configured — generated temporary random API key (spec §7.3)")
	}

	// Apply tracing flags: CLI flags override config values.
	// Merge CLI/env tracing settings into gateway config.
	if otlpEndpoint != "" || cmd.Flags().Changed("otlp-endpoint") {
		ensureTracingConfig(cfg).Endpoint = otlpEndpoint
	}
	if cmd.Flags().Changed("otlp-service-name") {
		ensureTracingConfig(cfg).ServiceName = otlpServiceName
	}
	if cmd.Flags().Changed("otlp-sample-rate") {
		ensureTracingConfig(cfg).SampleRate = &otlpSampleRate
	}

	// Initialize OpenTelemetry tracer provider.
	// When no endpoint is configured, a noop provider is used (zero overhead).
	var tracingCfg *config.TracingConfig
	if cfg.Gateway != nil {
		tracingCfg = cfg.Gateway.Tracing
	}
	tracingProvider := initTracingProviderWithFallback(
		ctx,
		tracingCfg,
		"Failed to initialize tracing provider: %v",
		func(format string, args ...any) {
			logger.StartupWarn(format, args...)
		},
	)
	defer func() {
		shutdownTracingProviderWithTimeout(tracingProvider, func(format string, args ...any) {
			log.Printf("Warning: "+format, args...)
		})
	}()

	// Apply W3C parent context from configured traceId/spanId (spec §4.1.3.6).
	// This links the gateway process lifetime span into a pre-existing trace when provided.
	ctx = tracing.ParentContext(ctx, tracingCfg)

	if tracingProvider.Tracer() != nil {
		// Log what InitProvider actually resolved (config already has env var defaults merged via CLI flags)
		endpoint := ""
		sampleRate := config.DefaultTracingSampleRate
		serviceName := config.DefaultTracingServiceName
		if tracingCfg != nil {
			endpoint = tracingCfg.Endpoint
			sampleRate = tracingCfg.GetSampleRate()
			serviceName = tracingCfg.ServiceName
		}
		if endpoint != "" {
			logger.StartupInfo("OpenTelemetry tracing enabled: endpoint=%s, service=%s, sampleRate=%.2f",
				endpoint, serviceName, sampleRate)
		} else {
			logger.StartupInfo("OpenTelemetry tracing disabled (no OTLP endpoint configured)")
		}
	}

	// Create unified MCP server (backend for both modes)
	unifiedServer, err := server.NewUnified(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create unified server: %w", err)
	}
	defer unifiedServer.Close()

	// Handle graceful shutdown via context cancellation
	go func() {
		<-ctx.Done()
		logger.LogInfoMd("shutdown", "Shutting down gateway...")
		log.Println("Shutting down...")
		unifiedServer.Close()
	}()

	// Create HTTP server based on mode
	var httpServer *http.Server
	if mode == "routed" {
		logger.StartupInfo("Starting MCPG in ROUTED mode on %s", listenAddr)
		logger.StartupInfo("Routes: /mcp/<server> where <server> is one of: %v", unifiedServer.GetServerIDs())

		// Extract API key from gateway config (spec 7.1)
		apiKey := cfg.GetAPIKey()

		httpServer = server.CreateHTTPServerForRoutedMode(listenAddr, unifiedServer, apiKey, hmacSecret)
	} else {
		logger.StartupInfo("Starting MCPG in UNIFIED mode on %s", listenAddr)
		logger.StartupInfo("Endpoint: /mcp")

		// Extract API key from gateway config (spec 7.1)
		apiKey := cfg.GetAPIKey()

		httpServer = server.CreateHTTPServerForMCP(listenAddr, unifiedServer, apiKey, hmacSecret)
	}
	// Register the HTTP server shutdown function so the /close handler can drain
	// in-flight requests before exiting (spec 5.1.3)
	unifiedServer.SetHTTPShutdown(httpServer.Shutdown)

	// Build net.Listener — optionally wrapping with TLS (ASI-07 Phase 1).
	// Plain HTTP is still used when no TLS certificate is configured (backward compatible).
	// Validate that TLS flags are consistent: cert+key must both be provided together,
	// and CA cert requires cert+key to be set.
	hasCert := tlsCertPath != ""
	hasKey := tlsKeyPath != ""
	hasCA := tlsCAPath != ""
	if hasCert != hasKey {
		return fmt.Errorf("--tls-cert and --tls-key must both be provided together")
	}
	if hasCA && !hasCert {
		return fmt.Errorf("--tls-ca requires --tls-cert and --tls-key to also be set")
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
	}
	tlsEnabled := hasCert && hasKey
	var tlsCfg *tls.Config
	if tlsEnabled {
		tlsCfg, err = server.LoadGatewayTLS(tlsCertPath, tlsKeyPath, tlsCAPath)
		if err != nil {
			_ = listener.Close()
			return fmt.Errorf("failed to configure TLS: %w", err)
		}
		listener = tls.NewListener(listener, tlsCfg)
		mtlsNote := ""
		if tlsCAPath != "" {
			mtlsNote = " (mTLS enabled)"
		}
		logger.StartupInfo("TLS enabled: cert=%s, key=%s, ca=%s — listening on https://%s%s", tlsCertPath, tlsKeyPath, tlsCAPath, listenAddr, mtlsNote)
	} else {
		logger.StartupInfo("TLS not configured — listening on http://%s (set --tls-cert/--tls-key to enable)", listenAddr)
	}
	if hmacSecret != "" {
		logger.StartupInfo("HMAC request signing enabled (ASI-07)")
	}

	// Start HTTP server in background
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
			cancel()
		}
	}()

	// Write gateway configuration to stdout per spec section 5.4
	if err := writeGatewayConfigToStdout(cfg, listenAddr, mode, tlsEnabled); err != nil {
		log.Printf("Warning: failed to write gateway configuration to stdout: %v", err)
	}

	// Wait for shutdown signal
	<-ctx.Done()

	// Gracefully shutdown HTTP server with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	return nil
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// SetVersion sets the version string for the CLI
func SetVersion(v string) {
	cliVersion = v
	rootCmd.Version = v
	version.Set(v)
}

// Package proxy implements a filtering HTTP proxy for the GitHub API.
// It intercepts gh CLI requests (via GH_HOST redirect) and applies
// the same DIFC enforcement pipeline as the MCP gateway, reusing the
// guard WASM module, evaluator, and agent registry.
package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logProxy = logger.New("proxy:proxy")

const (
	// DefaultGitHubAPIBase is the upstream GitHub API URL.
	DefaultGitHubAPIBase = "https://api.github.com"

	// ghHostPathPrefix is the /api/v3/ prefix that gh adds when using GH_HOST.
	ghHostPathPrefix = "/api/v3"
)

// Server is a filtering HTTP forward proxy for the GitHub REST/GraphQL API.
// It loads the same WASM guard used by the MCP gateway and runs the 6-phase
// DIFC pipeline on every proxied response.
type Server struct {
	guard         guard.Guard
	evaluator     *difc.Evaluator
	agentRegistry *difc.AgentRegistry
	capabilities  *difc.Capabilities

	githubToken  string
	githubAPIURL string // upstream base URL (no trailing slash)

	httpClient *http.Client

	// guardInitialized tracks whether LabelAgent has been called
	guardInitialized bool
	enforcementMode  difc.EnforcementMode
}

// Config holds the configuration for creating a proxy Server.
type Config struct {
	// WasmPath is the file path to the guard WASM module.
	WasmPath string

	// Policy is the guard policy JSON (e.g. {"allow-only":{...}}).
	Policy string

	// GitHubToken is a fallback token for upstream GitHub API requests.
	// When empty, the proxy forwards the client's Authorization header instead.
	GitHubToken string

	// GitHubAPIURL overrides the upstream API base URL (default: https://api.github.com).
	GitHubAPIURL string

	// DIFCMode is the enforcement mode (strict, filter, propagate).
	DIFCMode string
}

// New creates a new proxy Server from the given Config.
func New(ctx context.Context, cfg Config) (*Server, error) {
	logProxy.Printf("Creating proxy server: wasmPath=%s, apiURL=%s, difcMode=%s, hasToken=%v, hasPolicy=%v",
		cfg.WasmPath, cfg.GitHubAPIURL, cfg.DIFCMode, cfg.GitHubToken != "", cfg.Policy != "")

	if cfg.WasmPath == "" {
		return nil, fmt.Errorf("guard WASM path is required")
	}

	apiURL := cfg.GitHubAPIURL
	if apiURL == "" {
		apiURL = DefaultGitHubAPIBase
	}
	apiURL = strings.TrimRight(apiURL, "/")
	logProxy.Printf("Using upstream GitHub API URL: %s", apiURL)

	// Parse enforcement mode
	difcMode, err := difc.ParseEnforcementMode(cfg.DIFCMode)
	if err != nil {
		if cfg.DIFCMode != "" {
			log.Printf("[proxy] WARNING: invalid DIFC mode %q, defaulting to filter", cfg.DIFCMode)
		}
		difcMode = difc.EnforcementFilter // default to filter for proxy
	}
	logProxy.Printf("Enforcement mode resolved: %s", difcMode)

	// Load the WASM guard
	logProxy.Printf("Loading WASM guard from: %s", cfg.WasmPath)
	g, err := guard.NewWasmGuard(ctx, "github", cfg.WasmPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load WASM guard from %s: %w", cfg.WasmPath, err)
	}
	logProxy.Printf("WASM guard loaded successfully")

	s := &Server{
		guard:           g,
		evaluator:       difc.NewEvaluatorWithMode(difcMode),
		agentRegistry:   difc.NewAgentRegistryWithDefaults(nil, nil),
		capabilities:    difc.NewCapabilities(),
		githubToken:     cfg.GitHubToken,
		githubAPIURL:    apiURL,
		enforcementMode: difcMode,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			},
		},
	}

	// Initialize guard policy (LabelAgent)
	if cfg.Policy != "" {
		logProxy.Printf("Initializing guard policy from config")
		if err := s.initGuardPolicy(ctx, cfg.Policy); err != nil {
			return nil, fmt.Errorf("failed to initialize guard policy: %w", err)
		}
	} else {
		logProxy.Printf("No guard policy configured, running without policy enforcement")
	}

	logProxy.Printf("Proxy server created successfully: mode=%s", difcMode)
	return s, nil
}

// initGuardPolicy calls LabelAgent with the provided policy JSON.
func (s *Server) initGuardPolicy(ctx context.Context, policyJSON string) error {
	logProxy.Printf("Initializing guard policy: policyJSON_len=%d", len(policyJSON))

	var policy interface{}
	if err := json.Unmarshal([]byte(policyJSON), &policy); err != nil {
		return fmt.Errorf("invalid policy JSON: %w", err)
	}

	// Validate the policy structure
	policyMap, ok := policy.(map[string]interface{})
	if !ok {
		return fmt.Errorf("policy must be a JSON object")
	}
	guardPolicy := &config.GuardPolicy{}
	if ao, hasAO := policyMap["allow-only"]; hasAO {
		logProxy.Printf("Parsing allow-only policy from guard configuration")
		aoBytes, _ := json.Marshal(ao)
		var allowOnly config.AllowOnlyPolicy
		if err := json.Unmarshal(aoBytes, &allowOnly); err != nil {
			return fmt.Errorf("invalid allow-only policy: %w", err)
		}
		guardPolicy.AllowOnly = &allowOnly
	}
	if err := config.ValidateGuardPolicy(guardPolicy); err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	logProxy.Printf("Calling LabelAgent to initialize agent labels from guard")
	backend := &stubBackendCaller{}
	result, err := s.guard.LabelAgent(ctx, policy, backend, s.capabilities)
	if err != nil {
		return fmt.Errorf("LabelAgent failed: %w", err)
	}

	// Apply agent labels
	agentLabels := s.agentRegistry.GetOrCreate("proxy")
	for _, tag := range result.Agent.Secrecy {
		agentLabels.AddSecrecyTag(difc.Tag(tag))
	}
	for _, tag := range result.Agent.Integrity {
		agentLabels.AddIntegrityTag(difc.Tag(tag))
	}
	logProxy.Printf("Agent labels applied: secrecy=%v, integrity=%v", result.Agent.Secrecy, result.Agent.Integrity)

	// Parse enforcement mode from guard response
	if result.DIFCMode != "" {
		mode, err := difc.ParseEnforcementMode(result.DIFCMode)
		if err == nil {
			logProxy.Printf("Enforcement mode overridden by guard response: %s → %s", s.enforcementMode, mode)
			s.enforcementMode = mode
			s.evaluator.SetMode(mode)
		}
	}

	s.guardInitialized = true
	log.Printf("[proxy] Guard initialized: mode=%s, secrecy=%v, integrity=%v",
		s.enforcementMode, result.Agent.Secrecy, result.Agent.Integrity)

	return nil
}

// Handler returns an http.Handler for the proxy server.
func (s *Server) Handler() http.Handler {
	return &proxyHandler{server: s}
}

// stubBackendCaller is a no-op BackendCaller for the proxy.
// The guard receives the full API response in LabelResponse, so it
// does not need to make recursive backend calls.
type stubBackendCaller struct{}

func (s *stubBackendCaller) CallTool(_ context.Context, toolName string, _ interface{}) (interface{}, error) {
	logProxy.Printf("stub BackendCaller: ignoring CallTool(%s) — proxy provides full responses", toolName)
	return nil, fmt.Errorf("CallTool not supported in proxy mode")
}

// forwardToGitHub sends a request to the upstream GitHub API.
// clientAuth is the Authorization header from the inbound client request;
// if non-empty it is forwarded as-is, otherwise the configured fallback token is used.
func (s *Server) forwardToGitHub(ctx context.Context, method, path string, body io.Reader, contentType string, clientAuth string) (*http.Response, error) {
	url := s.githubAPIURL + path
	logProxy.Printf("forwarding %s %s → %s", method, path, url)

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create upstream request: %w", err)
	}

	// Prefer the client's own Authorization header; fall back to configured token.
	if clientAuth != "" {
		req.Header.Set("Authorization", clientAuth)
	} else if s.githubToken != "" {
		req.Header.Set("Authorization", "token "+s.githubToken)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "awmg-proxy/1.0")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return s.httpClient.Do(req)
}

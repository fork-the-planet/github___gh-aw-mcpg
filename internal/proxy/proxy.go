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
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/mcp"
	"github.com/github/gh-aw-mcpg/internal/strutil"
	"github.com/github/gh-aw-mcpg/internal/tracing"
)

var logProxy = logger.New("proxy:proxy")

const (
	// DefaultGitHubAPIBase is the upstream GitHub API URL.
	DefaultGitHubAPIBase = "https://api.github.com"

	// ghHostPathPrefix is the /api/v3/ prefix that gh adds when using GH_HOST.
	ghHostPathPrefix = "/api/v3"

	proxyAgentID = "proxy"
)

// Server is a filtering HTTP forward proxy for the GitHub REST/GraphQL API.
// It loads the same WASM guard used by the MCP gateway and runs the 6-phase
// DIFC pipeline on every proxied response.
type Server struct {
	guard guard.Guard
	difc.DIFCComponents

	githubToken  string
	githubAPIURL string // upstream base URL (no trailing slash)

	httpClient *http.Client

	// guardInitialized tracks whether LabelAgent has been called
	guardInitialized bool
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

	// TrustedBots is an optional list of additional trusted bot usernames.
	// These are passed to the guard alongside the policy during LabelAgent
	// initialization, extending the guard's built-in trusted bot list
	// (e.g. dependabot[bot], github-actions[bot]).
	TrustedBots []string

	// TrustedUsers is an optional list of GitHub usernames to elevate to approved
	// (writer) integrity, regardless of their author_association. These are injected
	// into the allow-only policy's trusted-users field during LabelAgent initialization.
	TrustedUsers []string
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

	// Initialize DIFC components (defaults to filter mode for the proxy).
	// NewComponents returns any parse error so we can warn without parsing twice.
	difcComponents, difcParseErr := difc.NewComponents(cfg.DIFCMode, difc.EnforcementFilter)
	if difcParseErr != nil {
		logger.LogWarn("startup", "invalid DIFC mode %q, defaulting to filter: %v", cfg.DIFCMode, difcParseErr)
	}
	logProxy.Printf("Enforcement mode resolved: %s", difcComponents.Mode)

	// Load the WASM guard
	logProxy.Printf("Loading WASM guard from: %s", cfg.WasmPath)
	g, err := guard.NewWasmGuard(ctx, "github", cfg.WasmPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load WASM guard from %s: %w", cfg.WasmPath, err)
	}
	logProxy.Printf("WASM guard loaded successfully")

	s := &Server{
		guard:          g,
		DIFCComponents: difcComponents,
		githubToken:    cfg.GitHubToken,
		githubAPIURL:   apiURL,
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
		if err := s.initGuardPolicy(ctx, cfg.Policy, cfg.TrustedBots, cfg.TrustedUsers); err != nil {
			return nil, fmt.Errorf("failed to initialize guard policy: %w", err)
		}
	} else {
		logProxy.Printf("No guard policy configured, running without policy enforcement")
	}

	logProxy.Printf("Proxy server created successfully: mode=%s", difcComponents.Mode)
	return s, nil
}

// initGuardPolicy calls LabelAgent with the provided policy JSON, optional trusted bots, and optional trusted users.
func (s *Server) initGuardPolicy(ctx context.Context, policyJSON string, trustedBots []string, trustedUsers []string) error {
	logProxy.Printf("Initializing guard policy: policyJSON_len=%d, trustedBots=%d, trustedUsers=%d", len(policyJSON), len(trustedBots), len(trustedUsers))

	var policy interface{}
	if err := json.Unmarshal([]byte(policyJSON), &policy); err != nil {
		return fmt.Errorf("invalid policy JSON: %w", err)
	}

	// Validate via the canonical helper so proxy.go stays in sync with any
	// future changes to GuardPolicy.UnmarshalJSON or ValidateGuardPolicy.
	parsedPolicy, err := config.ParseGuardPolicyJSON(policyJSON)
	if err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}
	if parsedPolicy.WriteSink != nil {
		return fmt.Errorf("policy validation failed: write-sink policies are not supported for guard initialization; expected top-level allow-only/allowonly policy")
	}

	// Normalize legacy top-level policy keys before building the payload so
	// trusted user injection works for both canonical and legacy input forms.
	if policyMap, ok := policy.(map[string]interface{}); ok {
		if _, hasCanonical := policyMap["allow-only"]; !hasCanonical {
			if legacy, hasLegacy := policyMap["allowonly"]; hasLegacy {
				policyMap["allow-only"] = legacy
			}
		}
	}

	// Build payload with optional trusted bots and trusted users
	payload := guard.BuildLabelAgentPayload(policy, trustedBots, trustedUsers)

	logProxy.Printf("Calling LabelAgent to initialize agent labels from guard")
	backend := &restBackendCaller{server: s}
	agentLabels := s.AgentRegistry.GetOrCreate(proxyAgentID)
	newMode, result, err := guard.RunLabelAgent(ctx, s.guard, payload, backend, s.Capabilities, agentLabels, s.Mode)
	if err != nil {
		return err
	}
	logProxy.Printf("Agent labels applied: secrecy=%v, integrity=%v", result.Agent.Secrecy, result.Agent.Integrity)

	if result.DIFCMode != "" {
		logProxy.Printf("Enforcement mode overridden by guard response: %s → %s", s.Mode, newMode)
		s.Mode = newMode
		s.Evaluator.SetMode(newMode)
	}

	s.guardInitialized = true
	logProxy.Printf("Guard initialized: mode=%s, secrecy=%v, integrity=%v",
		s.Mode, result.Agent.Secrecy, result.Agent.Integrity)

	return nil
}

// Handler returns an http.Handler for the proxy server.
// Every request is wrapped with an OTEL "proxy.request" span so the full
// proxy lifecycle (DIFC pipeline + GitHub API round-trip) appears in traces.
func (s *Server) Handler() http.Handler {
	return tracing.WrapHTTPHandler(&proxyHandler{
		server:       s,
		CachedTracer: tracing.CachedTracer{Tracer: tracing.Tracer()},
	}, "proxy.request")
}

// restBackendCaller translates guard CallTool requests into GitHub REST API
// calls, enabling backend enrichment (author_association, repo visibility, etc.)
// that the WASM guard needs for accurate integrity labeling.
type restBackendCaller struct {
	server     *Server
	clientAuth string
}

// extractOwnerRepoNumber reads owner, repo, and a numeric resource identifier
// from tool arguments, accepting either string or float64 JSON number inputs for
// the identifier.
func extractOwnerRepoNumber(argsMap map[string]interface{}, ownerKey, repoKey, numberKey, toolName string) (owner, repo, number string, err error) {
	owner = strutil.GetStringFromMap(argsMap, ownerKey)
	repo = strutil.GetStringFromMap(argsMap, repoKey)
	number = strutil.GetStringFromMap(argsMap, numberKey)
	if number == "" {
		if n, ok := argsMap[numberKey].(float64); ok {
			number = fmt.Sprintf("%d", int(n))
		}
	}
	if owner == "" || repo == "" || number == "" {
		err = fmt.Errorf("%s: missing %s/%s/%s", toolName, ownerKey, repoKey, numberKey)
	}
	return
}

func (r *restBackendCaller) CallTool(ctx context.Context, toolName string, args interface{}) (interface{}, error) {
	argsMap, ok := args.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected args type: %T", args)
	}

	var (
		apiPath                                 string
		collabOwner, collabRepo, collabUsername string
	)
	switch toolName {
	case "pull_request_read":
		owner, repo, number, err := extractOwnerRepoNumber(argsMap, "owner", "repo", "pullNumber", toolName)
		if err != nil {
			return nil, err
		}
		apiPath = fmt.Sprintf("/repos/%s/%s/pulls/%s", owner, repo, number)

	case "issue_read":
		owner, repo, number, err := extractOwnerRepoNumber(argsMap, "owner", "repo", "issue_number", toolName)
		if err != nil {
			return nil, err
		}
		apiPath = fmt.Sprintf("/repos/%s/%s/issues/%s", owner, repo, number)

	case "search_repositories":
		query, _ := argsMap["query"].(string)
		if query == "" {
			return nil, fmt.Errorf("search_repositories: missing query")
		}
		perPage := "10"
		if pp, ok := argsMap["perPage"].(float64); ok {
			perPage = fmt.Sprintf("%d", int(pp))
		}
		apiPath = fmt.Sprintf("/search/repositories?q=%s&per_page=%s", url.QueryEscape(query), perPage)

	case "get_collaborator_permission":
		var parseErr error
		collabOwner, collabRepo, collabUsername, parseErr = httputil.ParseCollaboratorPermissionArgs(argsMap)
		if parseErr != nil {
			logProxy.Printf("restBackendCaller: get_collaborator_permission missing args (owner=%q repo=%q username=%q)", collabOwner, collabRepo, collabUsername)
			return nil, parseErr
		}
		apiPath = fmt.Sprintf("/repos/%s/%s/collaborators/%s/permission", collabOwner, collabRepo, collabUsername)

	default:
		logProxy.Printf("restBackendCaller: unsupported tool %s", toolName)
		return nil, fmt.Errorf("unsupported tool: %s", toolName)
	}

	logProxy.Printf("restBackendCaller: %s → GET %s", toolName, apiPath)

	// Use the server's configured token for enrichment calls rather than the
	// client's auth header. Enrichment needs org-level visibility (e.g. to get
	// correct author_association) which the client's GITHUB_TOKEN may lack.
	enrichmentAuth := ""
	if r.server.githubToken != "" {
		enrichmentAuth = "token " + r.server.githubToken
	} else if r.clientAuth != "" {
		enrichmentAuth = r.clientAuth
	}
	// For get_collaborator_permission, reuse shared REST call helper.
	if toolName == "get_collaborator_permission" {
		result, err := httputil.FetchCollaboratorPermission(
			ctx,
			collabOwner,
			collabRepo,
			collabUsername,
			func(ctx context.Context, collabAPIPath string) (*http.Response, error) {
				resp, err := r.server.forwardToGitHub(ctx, "GET", collabAPIPath, nil, "", enrichmentAuth)
				if err != nil {
					return nil, fmt.Errorf("REST call failed: %w", err)
				}
				return resp, nil
			},
			logProxy.Printf,
		)
		if err != nil {
			logProxy.Printf("restBackendCaller: %s returned error: %v", toolName, err)
			return nil, err
		}
		return result, nil
	}

	resp, err := r.server.forwardToGitHub(ctx, "GET", apiPath, nil, "", enrichmentAuth)
	if err != nil {
		return nil, fmt.Errorf("REST call failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		logProxy.Printf("restBackendCaller: %s returned %d", toolName, resp.StatusCode)
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	// Wrap in MCP response format: {content: [{type: "text", text: "..."}]}
	return mcp.BuildMCPTextResponse(string(body)), nil
}

// upstreamHost returns the hostname of the upstream GitHub API URL.
// It is used to populate the server.address OTel attribute on the
// proxy.backend.forward span.
func (s *Server) upstreamHost() string {
	u, err := url.Parse(s.githubAPIURL)
	if err == nil && u.Host != "" {
		return u.Hostname()
	}

	// Handle scheme-less config values like "api.github.com" or "api.github.com/api/v3".
	u, err = url.Parse("https://" + strings.TrimLeft(s.githubAPIURL, "/"))
	if err == nil && u.Host != "" {
		return u.Hostname()
	}

	host, _, _ := strings.Cut(strings.TrimLeft(s.githubAPIURL, "/"), "/")
	return host
}

func (s *Server) isGHECDataResidencyHost() bool {
	host := strings.ToLower(s.upstreamHost())
	return strings.HasPrefix(host, "copilot-api.") && strings.HasSuffix(host, ".ghe.com")
}

// forwardToGitHub sends a request to the upstream GitHub API.
// clientAuth is the Authorization header from the inbound client request;
// if non-empty it is forwarded as-is, otherwise the configured fallback token is used.
func (s *Server) forwardToGitHub(ctx context.Context, method, path string, body io.Reader, contentType string, clientAuth string) (*http.Response, error) {
	url := s.githubAPIURL + path
	pathOnly, query, hasQuery := strings.Cut(path, "?")
	if IsGraphQLPath(pathOnly) {
		normalizedPath := strings.TrimSuffix(pathOnly, "/")
		var graphqlURL string
		if strings.HasSuffix(s.githubAPIURL, "/api/v3") {
			// GHES: strip /api/v3, GraphQL lives at /api/graphql
			graphqlURL = strings.TrimSuffix(s.githubAPIURL, "/api/v3") + "/api/graphql"
		} else if s.isGHECDataResidencyHost() && strings.HasSuffix(normalizedPath, "/api/graphql") {
			// GHE Cloud data residency (e.g. copilot-api.sj.ghe.com):
			// the client already sent /api/graphql — use the same path on upstream
			graphqlURL = s.githubAPIURL + "/api/graphql"
		} else {
			// github.com: GraphQL lives at /graphql
			graphqlURL = s.githubAPIURL + "/graphql"
		}
		url = graphqlURL
		if hasQuery {
			url += "?" + query
		}
	}
	logProxy.Printf("forwarding %s %s → %s", method, path, url)

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create upstream request: %w", err)
	}

	// Prefer the client's own Authorization header; fall back to configured token.
	var authHeader string
	if clientAuth != "" {
		authHeader = clientAuth
	} else if s.githubToken != "" {
		authHeader = "token " + s.githubToken
	}
	httputil.ApplyGitHubAPIHeaders(req, authHeader)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return s.httpClient.Do(req)
}

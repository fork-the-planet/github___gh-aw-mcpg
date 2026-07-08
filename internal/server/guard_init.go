package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/github/gh-aw-mcpg/internal/githubhttp"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/util"
)

var logGuardInit = logger.New("server:guard_init")

// legacyPolicySource is returned by resolveGuardPolicy when no explicit policy
// is configured and the caller should fall back to legacy session-label semantics.
const legacyPolicySource = "legacy"

// hasServerGuardPolicies reports whether any server in cfg has per-server guard policies
// configured. This is used during DIFC auto-detection to enable enforcement when policies
// are present even if no non-noop guard was registered (e.g., guard missing or failed to load).
func hasServerGuardPolicies(cfg *config.Config) bool {
	logGuardInit.Printf("Checking for server guard policies: serverCount=%d", len(cfg.Servers))
	for _, srv := range cfg.Servers {
		if len(srv.GuardPolicies) > 0 {
			logGuardInit.Print("Found at least one server with guard policies configured")
			return true
		}
	}
	logGuardInit.Print("No server guard policies found")
	return false
}

// registerGuard registers a guard for a specific backend server
// Guards are loaded based on the server's configuration:
// 1. If server has a "guard" field, look up the guard config by name
// 2. Create the appropriate guard type (wasm, noop, etc.)
// 3. Fall back to noop guard if no guard is configured
func (us *UnifiedServer) registerGuard(serverID string) error {
	var g guard.Guard
	us.logServerGuardPolicies(serverID)

	// Check if a per-server WASM guard exists in MCP_GATEWAY_WASM_GUARDS_DIR.
	// If found and loadable, it takes precedence over config-defined guards.
	if wasmPath, found, err := guard.FindServerWASMGuardFile(serverID); err != nil {
		logger.LogWarnToServer(serverID, "difc", "Failed to discover WASM guard from %s: %v", guard.WASMGuardsDirEnvVar, err)
	} else if found {
		ctx := context.Background()
		loadedGuard, loadErr := guard.NewWasmGuard(ctx, serverID, wasmPath, nil)
		if loadErr != nil {
			logger.LogWarnToServer(serverID, "difc", "Failed to load discovered WASM guard from %s: %v", wasmPath, loadErr)
		} else {
			logger.LogInfoToServer(serverID, "difc", "Loaded discovered WASM guard from file: %s", filepath.Base(wasmPath))
			g = loadedGuard
		}
	}

	if g == nil {
		// Check if server has a write-sink policy — create WriteSinkGuard directly
		if ws := us.resolveWriteSinkPolicy(serverID); ws != nil {
			effectiveVisibility := us.verifySinkVisibilityAtRuntime(serverID, ws.SinkVisibility)
			g = guard.NewWriteSinkGuardWithVisibility(ws.Accept, effectiveVisibility)
			logger.LogInfoToServer(serverID, "difc", "Created write-sink guard with %d accept patterns, sink-visibility=%q", len(ws.Accept), effectiveVisibility)
		}
	}

	if g == nil {
		// Check if server has a guard configured
		serverCfg, hasServer := us.cfg.Servers[serverID]
		if hasServer && serverCfg.Guard != "" {
			guardName := serverCfg.Guard

			// Look up guard config
			guardCfg, hasGuardCfg := us.cfg.Guards[guardName]
			if hasGuardCfg {
				// Create guard based on type
				var err error
				g, err = us.createGuardFromConfig(guardName, guardCfg)
				if err != nil {
					logger.LogWarnToServer(serverID, "difc", "Failed to create guard '%s': %v (falling back to noop)", guardName, err)
					g = guard.NewNoopGuard()
				}
			} else {
				// Guard name specified but no config found - try registered guard types
				var err error
				g, err = guard.CreateGuard(guardName)
				if err != nil {
					logger.LogWarnToServer(serverID, "difc", "Guard '%s' not found: %v (falling back to noop)", guardName, err)
					g = guard.NewNoopGuard()
				}
			}
		} else {
			// No guard configured - use noop
			g = guard.NewNoopGuard()
		}
	}

	// Before guard policy validation: apply forced repos="public" override when
	// the workflow repo is public. This modifies the in-memory config so that
	// subsequent resolveGuardPolicy calls for this server use the overridden value.
	if us.shouldForcePublicRepos() {
		us.overrideToPublicScope(serverID)
	}

	var policyErr error
	g, policyErr = us.requireGuardPolicyIfGuardEnabled(serverID, g)
	if policyErr != nil {
		return policyErr
	}

	us.guardRegistry.Register(serverID, g)
	logger.LogInfoToServer(serverID, "difc", "Registered guard '%s'", g.Name())
	return nil
}

func (us *UnifiedServer) requireGuardPolicyIfGuardEnabled(serverID string, g guard.Guard) (guard.Guard, error) {
	if g == nil || g.Name() == "noop" {
		return g, nil
	}

	policy, _, err := us.resolveGuardPolicy(serverID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		// Check if this server has guard policies configured.
		// If it does, keep the non-noop guard because DIFC will be auto-enabled later.
		// If not, fall back to noop guard.
		if us.cfg != nil && us.cfg.Servers != nil {
			if serverCfg, ok := us.cfg.Servers[serverID]; ok && serverCfg != nil && len(serverCfg.GuardPolicies) > 0 {
				logger.LogInfoToServer(serverID, "difc", "Guard '%s' loaded with guard-policies config (policy will be resolved during guard initialization)", g.Name())
				return g, nil
			}
		}

		logger.LogWarnToServer(serverID, "difc", "Guard '%s' is available but no guard policy is set; falling back to noop guard", g.Name())
		return guard.NewNoopGuard(), nil
	}

	return g, nil
}

func (us *UnifiedServer) logServerGuardPolicies(serverID string) {
	if us.cfg == nil || us.cfg.Servers == nil {
		logger.LogInfoToServer(serverID, "difc", "No guard policy was set")
		return
	}

	serverCfg, ok := us.cfg.Servers[serverID]
	if !ok || serverCfg == nil || len(serverCfg.GuardPolicies) == 0 {
		logger.LogInfoToServer(serverID, "difc", "No guard policy was set")
		return
	}

	policyJSON, err := json.Marshal(serverCfg.GuardPolicies)
	if err != nil {
		logger.LogWarnToServer(serverID, "difc", "Guard policy is set (failed to serialize policy: %v)", err)
		return
	}

	logger.LogInfoToServer(serverID, "difc", "Guard policy: %s", string(policyJSON))
}

func (us *UnifiedServer) logWASMGuardsDirConfiguration() {
	guardsRootDir := guard.GetWASMGuardsRootDir()
	if guardsRootDir == "" {
		logger.LogInfo("difc", "%s is not set", guard.WASMGuardsDirEnvVar)
		return
	}

	logger.LogInfo("difc", "%s=%s", guard.WASMGuardsDirEnvVar, guardsRootDir)
}

// createGuardFromConfig creates a guard instance from a guard configuration
func (us *UnifiedServer) createGuardFromConfig(name string, cfg *config.GuardConfig) (guard.Guard, error) {
	switch cfg.Type {
	case "noop", "":
		return guard.NewNoopGuard(), nil

	case "wasm":
		// WASM guard loading - requires path
		if cfg.Path == "" {
			return nil, fmt.Errorf("wasm guard '%s' requires a 'path' field", name)
		}
		// Create WASM guard directly with the path
		ctx := context.Background()
		// Create a backend caller that can be updated later per-request
		g, err := guard.NewWasmGuard(ctx, name, cfg.Path, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to load WASM guard from %s: %w", cfg.Path, err)
		}
		logger.LogInfo("difc", "Created WASM guard '%s' from path: %s", name, cfg.Path)
		return g, nil

	default:
		// Try registered guard types
		return guard.CreateGuard(cfg.Type)
	}
}

func (us *UnifiedServer) resolveGuardPolicy(serverID string) (*config.GuardPolicy, string, error) {
	logGuardInit.Printf("Resolving guard policy: serverID=%s", serverID)
	if us.cfg != nil && us.cfg.GuardPolicy != nil {
		if err := config.ValidateGuardPolicy(us.cfg.GuardPolicy); err != nil {
			return nil, "", err
		}
		source := us.cfg.GuardPolicySource
		if source == "" {
			source = "override"
		}
		logGuardInit.Printf("Using global guard policy: serverID=%s, source=%s", serverID, source)
		return us.cfg.GuardPolicy, source, nil
	}

	if us.cfg == nil {
		logGuardInit.Printf("No config available for guard policy: serverID=%s, using legacy", serverID)
		return nil, legacyPolicySource, nil
	}

	serverCfg, ok := us.cfg.Servers[serverID]
	if !ok || serverCfg == nil {
		logGuardInit.Printf("No server config found for guard policy: serverID=%s, using legacy", serverID)
		return nil, legacyPolicySource, nil
	}

	if policy, err := config.ParseServerGuardPolicy(serverID, serverCfg.GuardPolicies); err != nil {
		return nil, "", err
	} else if policy != nil {
		logGuardInit.Printf("Using server-level guard policy: serverID=%s", serverID)
		return policy, "server", nil
	}

	if serverCfg.Guard == "" {
		logGuardInit.Printf("No guard configured for server: serverID=%s, using legacy", serverID)
		return nil, legacyPolicySource, nil
	}

	guardCfg, ok := us.cfg.Guards[serverCfg.Guard]
	if !ok || guardCfg == nil || guardCfg.Policy == nil {
		logGuardInit.Printf("No guard config policy found: serverID=%s, guard=%s, using legacy", serverID, serverCfg.Guard)
		return nil, legacyPolicySource, nil
	}

	if err := config.ValidateGuardPolicy(guardCfg.Policy); err != nil {
		return nil, "", err
	}

	logGuardInit.Printf("Using guard config policy: serverID=%s, guard=%s", serverID, serverCfg.Guard)
	return guardCfg.Policy, "config", nil
}

// resolveWriteSinkPolicy checks if a server has a write-sink guard policy.
func (us *UnifiedServer) resolveWriteSinkPolicy(serverID string) *config.WriteSinkPolicy {
	policy, _, err := us.resolveGuardPolicy(serverID)
	if err != nil || policy == nil {
		return nil
	}
	return policy.WriteSink
}

func (us *UnifiedServer) ensureGuardInitialized(
	ctx context.Context,
	sessionID string,
	serverID string,
	g guard.Guard,
	backendCaller guard.BackendCaller,
) (difc.EnforcementMode, error) {
	defaultMode := us.Evaluator.GetMode()

	policy, source, err := us.resolveGuardPolicy(serverID)
	if err != nil {
		return defaultMode, fmt.Errorf("failed to resolve guard policy: %w", err)
	}
	if policy == nil {
		logger.LogInfoToServer(serverID, "difc", "Guard policy not configured; using legacy session labels")
		return defaultMode, nil
	}

	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return defaultMode, fmt.Errorf("failed to serialize guard policy: %w", err)
	}

	// Build the label_agent payload, merging in any configured trusted bots.
	// trusted-users is not injected here as a separate list because in gateway mode
	// it is specified directly inside the allow-only policy JSON (not as a standalone
	// gateway config field). The policy object already carries trusted-users when set.
	// The policyHash covers both the policy and trusted bots so that any change
	// to either field invalidates the cached guard session state.
	trustedBots := us.getTrustedBots()
	labelAgentPayload := guard.BuildLabelAgentPayload(policy, trustedBots, nil)
	payloadJSON, err := json.Marshal(labelAgentPayload)
	if err != nil {
		return defaultMode, fmt.Errorf("failed to serialize label_agent payload: %w", err)
	}
	policyHash := string(payloadJSON)

	us.sessionMu.RLock()
	session := us.sessions[sessionID]
	if session != nil {
		if state, ok := session.GuardInit[serverID]; ok && state.Initialized && state.PolicyHash == policyHash {
			mode := state.DIFCMode
			us.sessionMu.RUnlock()
			logGuardInit.Printf("Guard session cache hit: server=%s, session=%s, mode=%s", serverID, sessionID, mode)
			return mode, nil
		}
	}
	us.sessionMu.RUnlock()

	logger.LogInfoToServer(serverID, "difc", "Initializing guard session state: session=%s, policy_source=%s", sessionID, source)
	logger.LogInfoToServer(serverID, "difc", "Calling label_agent: session=%s, guard=%s, policy=%s", sessionID, g.Name(), string(policyJSON))

	agentID := guard.GetAgentIDFromContext(ctx)

	// Merge labels into existing agent (union semantics).
	// Multiple guards may contribute labels for the same agent; each guard's
	// label_agent output is additive so that later guards do not overwrite
	// labels set by earlier ones.
	mode, labelAgentResult, err := guard.RunLabelAgentForAgent(
		ctx,
		g,
		labelAgentPayload,
		backendCaller,
		us.Capabilities,
		us.AgentRegistry,
		agentID,
		defaultMode,
	)
	if err != nil {
		logger.LogErrorToServer(serverID, "difc", "label_agent failed: session=%s, guard=%s, error=%v", sessionID, g.Name(), err)
		return defaultMode, err
	}
	logger.LogMarshaledForDebugf(
		labelAgentResult,
		func(format string, args ...interface{}) {
			logger.LogInfoToServer(serverID, "difc", format, args...)
		},
		"label_agent response: session=%s, guard=%s, response=%s",
		func(format string, args ...interface{}) {
			logger.LogWarnToServer(serverID, "difc", format, args...)
		},
		"label_agent response (failed to serialize for logging): session=%s, guard=%s, error=%v",
		sessionID, g.Name(),
	)

	us.sessionMu.Lock()
	session = us.sessions[sessionID]
	normalizedPolicy := config.NormalizeScopeKind(labelAgentResult.NormalizedPolicy)
	if session == nil {
		session = NewSession(sessionID, "")
		us.sessions[sessionID] = session
	}
	if session.GuardInit == nil {
		session.GuardInit = make(map[string]*GuardSessionState)
	}
	var toolCallLimits map[string]int
	if policy.AllowOnly != nil {
		toolCallLimits = util.CopyTrimmedStringIntMap(policy.AllowOnly.ToolCallLimits)
	}
	session.GuardInit[serverID] = &GuardSessionState{
		Initialized:      true,
		PolicyHash:       policyHash,
		PolicySource:     source,
		DIFCMode:         mode,
		NormalizedPolicy: normalizedPolicy,
		ToolCallLimits:   toolCallLimits,
	}
	us.sessionMu.Unlock()

	logger.LogInfoToServer(serverID, "difc", "Guard policy initialized: session=%s, guard_policy.source=%s, difc_mode=%s, guard_policy.normalized=%v",
		sessionID, source, mode, normalizedPolicy)

	return mode, nil
}

// getTrustedBots returns the configured list of additional trusted bot usernames,
// or nil if none are configured.
func (us *UnifiedServer) getTrustedBots() []string {
	if us.cfg == nil || us.cfg.Gateway == nil {
		return nil
	}
	return us.cfg.Gateway.TrustedBots
}

// verifySinkVisibilityAtRuntime checks the actual repository visibility via the
// GitHub API and overrides the configured sink-visibility if the repo is more
// public than declared. This is a defense-in-depth measure: even if the compile-
// time config says "private", a runtime check catches cases where the repo was
// made public after the workflow was compiled.
//
// Emits a warning when overriding the configured value.
// Falls back to the configured value on any API error (non-fatal).
func (us *UnifiedServer) verifySinkVisibilityAtRuntime(serverID, configuredVisibility string) string {
	// Skip runtime check when sink-visibility is not explicitly configured.
	// This preserves backward-compatible behavior where omitted sink-visibility
	// uses accept patterns without any override.
	if configuredVisibility == "" {
		logGuardInit.Printf("sink-visibility runtime check skipped: sink-visibility not configured (serverID=%s)", serverID)
		return configuredVisibility
	}

	nwo := os.Getenv("GITHUB_REPOSITORY")
	if nwo == "" {
		logGuardInit.Printf("sink-visibility runtime check skipped: GITHUB_REPOSITORY not set (serverID=%s)", serverID)
		return configuredVisibility
	}

	token := envutil.LookupGitHubToken()
	if token == "" {
		logGuardInit.Printf("sink-visibility runtime check skipped: no GitHub token available (serverID=%s)", serverID)
		return configuredVisibility
	}

	vis, ok := us.resolveWorkflowRepoVisibility()
	if !ok {
		logger.LogWarnToServer(serverID, "difc", "Sink visibility runtime verification failed (using configured value %q): API error or unavailable", configuredVisibility)
		return configuredVisibility
	}

	configured := strings.ToLower(strings.TrimSpace(configuredVisibility))

	// If actual is "public" but configured is not "public",
	// override to "public" — this is the security-critical case.
	if vis == githubhttp.RepoVisibilityPublic && configured != "public" {
		logger.LogWarnToServer(serverID, "difc",
			"SINK VISIBILITY OVERRIDE: configured=%q but runtime check shows repo %s is %q — overriding to %q to prevent potential data exfiltration",
			configuredVisibility, nwo, vis, "public")
		return "public"
	}

	logger.LogInfoToServer(serverID, "difc", "Sink visibility runtime verification passed: repo=%s, visibility=%q", nwo, vis)
	return string(vis)
}

// resolveWorkflowRepoVisibility fetches and caches the visibility of the workflow
// repository identified by GITHUB_REPOSITORY. The API call is made at most once
// per gateway lifetime; subsequent calls return the cached result immediately.
//
// Returns (visibility, true) on success, or ("", false) when the repository is
// unknown, the token is unavailable, or the API call fails (fail-open semantics).
func (us *UnifiedServer) resolveWorkflowRepoVisibility() (githubhttp.RepoVisibility, bool) {
	us.repoVisibilityOnce.Do(func() {
		nwo := os.Getenv("GITHUB_REPOSITORY")
		if nwo == "" {
			return
		}
		token := envutil.LookupGitHubToken()
		if token == "" {
			return
		}
		apiURL := envutil.DeriveGitHubAPIURL(envutil.DefaultGitHubAPIBaseURL)
		authHeader := "token " + token

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		vis, err := githubhttp.FetchRepoVisibility(ctx, apiURL, nwo, authHeader)
		if err != nil {
			logGuardInit.Printf("resolveWorkflowRepoVisibility: API call failed for %s: %v", nwo, err)
			return // cache remains empty; repoVisibilityCacheOK stays false
		}
		us.repoVisibilityCached = vis
		us.repoVisibilityCacheOK = true
		logGuardInit.Printf("resolveWorkflowRepoVisibility: cached repo visibility for %s: %s", nwo, vis)
	})
	return us.repoVisibilityCached, us.repoVisibilityCacheOK
}

// shouldForcePublicRepos returns true when the gateway should automatically
// override allow-only policies to repos="public". This is true when:
//   - The feature is not explicitly disabled (config ForcePublicRepos=false or env MCP_GATEWAY_FORCE_PUBLIC_REPOS=false)
//   - GITHUB_REPOSITORY is set
//   - A GitHub token is available
//   - The GitHub API confirms the repository is public
//
// The result is cached after the first call (backed by forcePublicReposOnce).
func (us *UnifiedServer) shouldForcePublicRepos() bool {
	us.forcePublicReposOnce.Do(func() {
		us.forcePublicReposResult = us.computeForcePublicRepos()
	})
	return us.forcePublicReposResult
}

func (us *UnifiedServer) computeForcePublicRepos() bool {
	// Config opt-out: forcePublicRepos=false in gateway config disables the feature.
	if us.cfg != nil && us.cfg.Gateway != nil && us.cfg.Gateway.ForcePublicRepos != nil && !*us.cfg.Gateway.ForcePublicRepos {
		logGuardInit.Print("shouldForcePublicRepos: disabled by config (forcePublicRepos=false)")
		return false
	}

	// Env opt-out: MCP_GATEWAY_FORCE_PUBLIC_REPOS=false disables the feature.
	// Default is true (feature enabled), so only an explicit "false" disables it.
	if !envutil.GetEnvBool(config.EnvForcePublicRepos, true) {
		logGuardInit.Printf("shouldForcePublicRepos: disabled by env var %s=false", config.EnvForcePublicRepos)
		return false
	}

	nwo := os.Getenv("GITHUB_REPOSITORY")
	if nwo == "" {
		logGuardInit.Print("shouldForcePublicRepos: GITHUB_REPOSITORY not set — skipping")
		return false
	}

	token := envutil.LookupGitHubToken()
	if token == "" {
		logGuardInit.Print("shouldForcePublicRepos: no GitHub token available — skipping")
		return false
	}

	vis, ok := us.resolveWorkflowRepoVisibility()
	if !ok {
		logger.LogWarn("difc", "shouldForcePublicRepos: failed to determine visibility for %s (fail-open, not forcing repos=public)", nwo)
		return false
	}

	return vis == githubhttp.RepoVisibilityPublic
}

// overrideToPublicScope modifies the in-memory guard policy for serverID so that
// any allow-only scope is set to repos="public". This is called when the workflow
// repository is confirmed to be public, closing the GitLost read-path attack vector.
//
// Override precedence (first match wins):
//  1. Global guard policy override (us.cfg.GuardPolicy) — applies to all servers.
//  2. Per-server guard policies in serverCfg.GuardPolicies.
//
// When an existing AllowOnly is found its Repos field is set to "public".
// When no AllowOnly exists in the policy and it is not a write-sink-only policy,
// an AllowOnly with min-integrity="none" is added.
// When no policy exists at all for the server, a new allow-only policy is created.
// Write-sink-only policies are left untouched: allow-only and write-sink are
// mutually exclusive in the GuardPolicy schema, and write-sink guards use
// SinkVisibility (not AllowOnly) to enforce public-repo restrictions.
//
// Changes are permanent for the gateway's lifetime and affect all subsequent
// resolveGuardPolicy calls for the given server.
func (us *UnifiedServer) overrideToPublicScope(serverID string) {
	nwo := os.Getenv("GITHUB_REPOSITORY")

	// Case 1: global policy override (set via CLI or env flags).
	if us.cfg != nil && us.cfg.GuardPolicy != nil {
		gp := us.cfg.GuardPolicy
		if gp.AllowOnly == nil && gp.WriteSink != nil {
			// Write-sink-only global policy: AllowOnly and WriteSink are mutually
			// exclusive. The write-sink guard uses SinkVisibility for public-repo
			// enforcement; no AllowOnly override needed.
			logGuardInit.Printf("overrideToPublicScope: skipping write-sink-only global policy for serverID=%s", serverID)
			return
		}
		if gp.AllowOnly == nil {
			gp.AllowOnly = &config.AllowOnlyPolicy{
				Repos:        "public",
				MinIntegrity: config.IntegrityNone,
			}
		} else {
			gp.AllowOnly.Repos = "public"
		}
		logger.LogWarnToServer(serverID, "difc",
			"FORCED REPOS=PUBLIC: workflow repo %s is public — overriding allow-only scope to 'public' to prevent private data reads (source: global policy)",
			nwo)
		return
	}

	// Case 2: per-server guard policies in config.
	if us.cfg == nil || us.cfg.Servers == nil {
		return
	}
	serverCfg, ok := us.cfg.Servers[serverID]
	if !ok || serverCfg == nil {
		return
	}

	if len(serverCfg.GuardPolicies) == 0 {
		// No existing per-server policy — inject a minimal allow-only policy so
		// the WASM guard (if loaded) will restrict reads to public repos.
		serverCfg.GuardPolicies = map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": config.IntegrityNone,
			},
		}
	} else {
		policy, err := config.ParseServerGuardPolicy(serverID, serverCfg.GuardPolicies)
		if err != nil {
			logger.LogWarnToServer(serverID, "difc",
				"FORCED REPOS=PUBLIC: failed to parse existing policy for override (skipping): %v", err)
			return
		}
		if policy == nil {
			// Unrecognized policy format — inject allow-only directly into the map
			// only if there is no write-sink key present (to avoid creating an
			// invalid combined policy).
			if _, hasWriteSink := serverCfg.GuardPolicies["write-sink"]; !hasWriteSink {
				serverCfg.GuardPolicies["allow-only"] = map[string]interface{}{
					"repos":         "public",
					"min-integrity": config.IntegrityNone,
				}
			} else {
				logGuardInit.Printf("overrideToPublicScope: skipping write-sink-only per-server policy for serverID=%s", serverID)
				return
			}
		} else if policy.AllowOnly == nil && policy.WriteSink != nil {
			// Write-sink-only per-server policy: see note above.
			logGuardInit.Printf("overrideToPublicScope: skipping write-sink-only per-server policy for serverID=%s", serverID)
			return
		} else {
			if policy.AllowOnly == nil {
				policy.AllowOnly = &config.AllowOnlyPolicy{
					Repos:        "public",
					MinIntegrity: config.IntegrityNone,
				}
			} else {
				policy.AllowOnly.Repos = "public"
			}
			newMap, err := config.GuardPolicyToMap(policy)
			if err != nil {
				logger.LogWarnToServer(serverID, "difc",
					"FORCED REPOS=PUBLIC: failed to serialize overridden policy (skipping): %v", err)
				return
			}
			serverCfg.GuardPolicies = newMap
		}
	}

	logger.LogWarnToServer(serverID, "difc",
		"FORCED REPOS=PUBLIC: workflow repo %s is public — overriding allow-only scope to 'public' to prevent private data reads",
		nwo)
}

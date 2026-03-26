package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logGuardInit = logger.New("server:guard_init")

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
	if wasmPath, found, err := findServerWASMGuardFile(serverID); err != nil {
		log.Printf("[DIFC] WARNING: Failed to discover WASM guard for server '%s' from %s: %v", serverID, wasmGuardsDirEnvVar, err)
	} else if found {
		ctx := context.Background()
		loadedGuard, loadErr := guard.NewWasmGuard(ctx, serverID, wasmPath, nil)
		if loadErr != nil {
			log.Printf("[DIFC] WARNING: Failed to load discovered WASM guard for server '%s' from %s: %v", serverID, wasmPath, loadErr)
		} else {
			log.Printf("[DIFC] Loaded discovered WASM guard for server '%s' from file: %s", serverID, filepath.Base(wasmPath))
			g = loadedGuard
		}
	}

	if g == nil {
		// Check if server has a write-sink policy — create WriteSinkGuard directly
		if ws := us.resolveWriteSinkPolicy(serverID); ws != nil {
			g = guard.NewWriteSinkGuard(ws.Accept)
			log.Printf("[DIFC] Created write-sink guard for server '%s' with %d accept patterns", serverID, len(ws.Accept))
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
					log.Printf("[DIFC] WARNING: Failed to create guard '%s' for server '%s': %v (falling back to noop)", guardName, serverID, err)
					g = guard.NewNoopGuard()
				}
			} else {
				// Guard name specified but no config found - try registered guard types
				var err error
				g, err = guard.CreateGuard(guardName)
				if err != nil {
					log.Printf("[DIFC] WARNING: Guard '%s' not found for server '%s': %v (falling back to noop)", guardName, serverID, err)
					g = guard.NewNoopGuard()
				}
			}
		} else {
			// No guard configured - use noop
			g = guard.NewNoopGuard()
		}
	}

	var policyErr error
	g, policyErr = us.requireGuardPolicyIfGuardEnabled(serverID, g)
	if policyErr != nil {
		return policyErr
	}

	us.guardRegistry.Register(serverID, g)
	log.Printf("[DIFC] Registered guard '%s' for server '%s'", g.Name(), serverID)
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
				log.Printf("[DIFC] Guard '%s' loaded for server '%s' with guard-policies config (policy will be resolved during guard initialization)", g.Name(), serverID)
				return g, nil
			}
		}

		log.Printf("[DIFC] WARNING: Guard '%s' is available for MCP server '%s' but no guard policy is set; falling back to noop guard", g.Name(), serverID)
		return guard.NewNoopGuard(), nil
	}

	return g, nil
}

func (us *UnifiedServer) logServerGuardPolicies(serverID string) {
	if us.cfg == nil || us.cfg.Servers == nil {
		log.Printf("[DIFC] no guard policy was set for MCP server '%s'", serverID)
		return
	}

	serverCfg, ok := us.cfg.Servers[serverID]
	if !ok || serverCfg == nil || len(serverCfg.GuardPolicies) == 0 {
		log.Printf("[DIFC] no guard policy was set for MCP server '%s'", serverID)
		return
	}

	policyJSON, err := json.Marshal(serverCfg.GuardPolicies)
	if err != nil {
		log.Printf("[DIFC] guard policy is set for MCP server '%s' (failed to serialize policy: %v)", serverID, err)
		return
	}

	log.Printf("[DIFC] guard policy for MCP server '%s': %s", serverID, string(policyJSON))
}

func findServerWASMGuardFile(serverID string) (string, bool, error) {
	guardsRootDir := strings.TrimSpace(os.Getenv(wasmGuardsDirEnvVar))
	if guardsRootDir == "" {
		logGuardInit.Printf("Skipping WASM guard discovery: %s is not set", wasmGuardsDirEnvVar)
		return "", false, nil
	}

	serverGuardDir := filepath.Join(guardsRootDir, serverID)
	logGuardInit.Printf("Searching for WASM guard file: serverID=%s, dir=%s", serverID, serverGuardDir)
	entries, err := os.ReadDir(serverGuardDir)
	if err != nil {
		if os.IsNotExist(err) {
			logGuardInit.Printf("No WASM guard directory found for serverID=%s", serverID)
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read server guard directory %q: %w", serverGuardDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if strings.EqualFold(filepath.Ext(entry.Name()), ".wasm") {
			wasmPath := filepath.Join(serverGuardDir, entry.Name())
			logGuardInit.Printf("Found WASM guard file: serverID=%s, path=%s", serverID, wasmPath)
			return wasmPath, true, nil
		}
	}

	logGuardInit.Printf("No WASM guard file found in directory: serverID=%s, dir=%s", serverID, serverGuardDir)
	return "", false, nil
}

func (us *UnifiedServer) logWASMGuardsDirConfiguration() {
	guardsRootDir := strings.TrimSpace(os.Getenv(wasmGuardsDirEnvVar))
	if guardsRootDir == "" {
		log.Printf("[DIFC] %s is not set", wasmGuardsDirEnvVar)
		return
	}

	log.Printf("[DIFC] %s=%s", wasmGuardsDirEnvVar, guardsRootDir)
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
		log.Printf("[DIFC] Created WASM guard '%s' from path: %s", name, cfg.Path)
		return g, nil

	default:
		// Try registered guard types
		return guard.CreateGuard(cfg.Type)
	}
}

func normalizeScopeKind(policy map[string]interface{}) map[string]interface{} {
	if policy == nil {
		return nil
	}

	normalized := make(map[string]interface{}, len(policy))
	for key, value := range policy {
		normalized[key] = value
	}

	if scopeKind, ok := normalized["scope_kind"].(string); ok {
		normalized["scope_kind"] = strings.ToLower(strings.TrimSpace(scopeKind))
	}

	return normalized
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
		return nil, "legacy", nil
	}

	serverCfg, ok := us.cfg.Servers[serverID]
	if !ok || serverCfg == nil {
		logGuardInit.Printf("No server config found for guard policy: serverID=%s, using legacy", serverID)
		return nil, "legacy", nil
	}

	if policy, err := config.ParseServerGuardPolicy(serverID, serverCfg.GuardPolicies); err != nil {
		return nil, "", err
	} else if policy != nil {
		logGuardInit.Printf("Using server-level guard policy: serverID=%s", serverID)
		return policy, "server", nil
	}

	if serverCfg.Guard == "" {
		logGuardInit.Printf("No guard configured for server: serverID=%s, using legacy", serverID)
		return nil, "legacy", nil
	}

	guardCfg, ok := us.cfg.Guards[serverCfg.Guard]
	if !ok || guardCfg == nil || guardCfg.Policy == nil {
		logGuardInit.Printf("No guard config policy found: serverID=%s, guard=%s, using legacy", serverID, serverCfg.Guard)
		return nil, "legacy", nil
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
	defaultMode := us.evaluator.GetMode()

	policy, source, err := us.resolveGuardPolicy(serverID)
	if err != nil {
		return defaultMode, fmt.Errorf("failed to resolve guard policy: %w", err)
	}
	if policy == nil {
		log.Printf("[DIFC] Guard policy not configured for server '%s'; using legacy session labels", serverID)
		return defaultMode, nil
	}

	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return defaultMode, fmt.Errorf("failed to serialize guard policy: %w", err)
	}

	// Build the label_agent payload, merging in any configured trusted bots.
	// The policyHash covers both the policy and trusted bots so that any change
	// to either field invalidates the cached guard session state.
	trustedBots := us.getTrustedBots()
	labelAgentPayload := guard.BuildLabelAgentPayload(policy, trustedBots)
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

	log.Printf("[DIFC] Initializing guard session state: server=%s, session=%s, policy_source=%s", serverID, sessionID, source)
	log.Printf("[DIFC] Calling label_agent: server=%s, session=%s, guard=%s, policy=%s", serverID, sessionID, g.Name(), string(policyJSON))
	labelAgentResult, err := g.LabelAgent(ctx, labelAgentPayload, backendCaller, us.capabilities)
	if err != nil {
		log.Printf("[DIFC] label_agent failed: server=%s, session=%s, guard=%s, error=%v", serverID, sessionID, g.Name(), err)
		return defaultMode, fmt.Errorf("label_agent failed: %w", err)
	}
	if labelAgentResult == nil {
		log.Printf("[DIFC] label_agent returned nil result: server=%s, session=%s, guard=%s", serverID, sessionID, g.Name())
		return defaultMode, fmt.Errorf("label_agent returned nil result")
	}
	resultJSON, marshalErr := json.Marshal(labelAgentResult)
	if marshalErr != nil {
		log.Printf("[DIFC] label_agent returned result (failed to serialize for logging): server=%s, session=%s, guard=%s, error=%v", serverID, sessionID, g.Name(), marshalErr)
	} else {
		log.Printf("[DIFC] label_agent response: server=%s, session=%s, guard=%s, response=%s", serverID, sessionID, g.Name(), string(resultJSON))
	}

	mode := defaultMode
	if labelAgentResult.DIFCMode != "" {
		parsedMode, err := difc.ParseEnforcementMode(labelAgentResult.DIFCMode)
		if err != nil {
			return defaultMode, fmt.Errorf("invalid difc_mode from label_agent: %w", err)
		}
		mode = parsedMode
	}

	agentID := guard.GetAgentIDFromContext(ctx)
	secrecyTags := difc.StringsToTags(labelAgentResult.Agent.Secrecy)
	integrityTags := difc.StringsToTags(labelAgentResult.Agent.Integrity)

	// Merge labels into existing agent (union semantics).
	// Multiple guards may contribute labels for the same agent; each guard's
	// label_agent output is additive so that later guards do not overwrite
	// labels set by earlier ones.
	agentLabels := us.agentRegistry.GetOrCreate(agentID)
	agentLabels.AddSecrecyTags(secrecyTags)
	agentLabels.AddIntegrityTags(integrityTags)

	us.sessionMu.Lock()
	session = us.sessions[sessionID]
	normalizedPolicy := normalizeScopeKind(labelAgentResult.NormalizedPolicy)
	if session == nil {
		session = NewSession(sessionID, "")
		us.sessions[sessionID] = session
	}
	if session.GuardInit == nil {
		session.GuardInit = make(map[string]*GuardSessionState)
	}
	session.GuardInit[serverID] = &GuardSessionState{
		Initialized:      true,
		PolicyHash:       policyHash,
		PolicySource:     source,
		DIFCMode:         mode,
		NormalizedPolicy: normalizedPolicy,
	}
	us.sessionMu.Unlock()

	log.Printf("[DIFC] Guard policy initialized: server=%s, session=%s, guard_policy.source=%s, difc_mode=%s, guard_policy.normalized=%v",
		serverID, sessionID, source, mode, normalizedPolicy)

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

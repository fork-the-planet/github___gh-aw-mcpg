package cmd

// DIFC (Decentralized Information Flow Control) related flags

import (
	"os"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/spf13/cobra"
)

// DIFC flag variables
var (
	difcMode          string
	difcSinkServerIDs string // Comma-separated server IDs that should include DIFC tag snapshots in RPC JSONL logs
	guardPolicyJSON   string
	allowOnlyPublic   bool
	allowOnlyOwner    string
	allowOnlyRepo     string
	allowOnlyMinInt   string
)

// containerGuardWasmPath is the baked-in guard path in the container image.
const containerGuardWasmPath = "/guards/github/00-github-guard.wasm"

func init() {
	RegisterFlag(func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&difcMode, "guards-mode", difc.DefaultEnforcementMode(), "Guards enforcement mode: strict (deny violations), filter (remove denied tools), or propagate (auto-adjust agent labels on reads)")
		cmd.Flags().StringVar(&difcSinkServerIDs, "guards-sink-server-ids", envutil.GetEnvString("MCP_GATEWAY_GUARDS_SINK_SERVER_IDS", ""), "Comma-separated server IDs whose RPC JSONL logs should include agent secrecy/integrity tag snapshots")
		cmd.Flags().StringVar(&guardPolicyJSON, "guard-policy-json", envutil.GetEnvString(config.EnvGuardPolicyJSON, ""), "Guard policy JSON (e.g. {\"allow-only\":{\"repos\":\"public\",\"min-integrity\":\"none\"}})")
		cmd.Flags().BoolVar(&allowOnlyPublic, "allowonly-scope-public", envutil.GetEnvBool(config.EnvAllowOnlyScopePublic, false), "Use public AllowOnly scope")
		cmd.Flags().StringVar(&allowOnlyOwner, "allowonly-scope-owner", envutil.GetEnvString(config.EnvAllowOnlyScopeOwner, ""), "AllowOnly owner scope value")
		cmd.Flags().StringVar(&allowOnlyRepo, "allowonly-scope-repo", envutil.GetEnvString(config.EnvAllowOnlyScopeRepo, ""), "AllowOnly repo name (requires owner)")
		cmd.Flags().StringVar(&allowOnlyMinInt, "allowonly-min-integrity", envutil.GetEnvString(config.EnvAllowOnlyMinIntegrity, ""), "AllowOnly integrity: none|unapproved|approved|merged")
	})
}

// detectGuardWasm returns the baked-in container guard path if it exists,
// or empty string if not found (requiring the user to specify --guard-wasm).
func detectGuardWasm() string {
	debugLog.Printf("Checking for baked-in guard at %s", containerGuardWasmPath)
	if _, err := os.Stat(containerGuardWasmPath); err == nil {
		debugLog.Printf("Auto-detected baked-in guard: %s", containerGuardWasmPath)
		return containerGuardWasmPath
	}
	debugLog.Print("Baked-in guard not found, --guard-wasm flag required")
	return ""
}

func resolveGuardPolicyOverride(cmd *cobra.Command) (*config.GuardPolicy, string, error) {
	cliGuardPolicyChanged := cmd.Flags().Changed("guard-policy-json")
	cliChanged := cliGuardPolicyChanged ||
		cmd.Flags().Changed("allowonly-scope-public") ||
		cmd.Flags().Changed("allowonly-scope-owner") ||
		cmd.Flags().Changed("allowonly-scope-repo") ||
		cmd.Flags().Changed("allowonly-min-integrity")

	debugLog.Printf("Resolving guard policy override: cliChanged=%v, cliGuardPolicyChanged=%v, allowOnlyPublic=%v, owner=%q, repo=%q, minIntegrity=%q",
		cliChanged, cliGuardPolicyChanged, allowOnlyPublic, allowOnlyOwner, allowOnlyRepo, allowOnlyMinInt)

	cliPolicyJSON := ""
	if cliGuardPolicyChanged {
		cliPolicyJSON = guardPolicyJSON
		debugLog.Printf("Using CLI guard-policy-json: %q", cliPolicyJSON)
	}

	policy, policyJSON, err := config.ResolveGuardPolicyOverride(
		cliChanged,
		cliPolicyJSON,
		allowOnlyPublic,
		allowOnlyOwner,
		allowOnlyRepo,
		allowOnlyMinInt,
	)
	if err != nil {
		debugLog.Printf("Guard policy resolution failed: %v", err)
	} else if policy != nil {
		debugLog.Printf("Guard policy resolved: policyJSON=%q", policyJSON)
	} else {
		debugLog.Print("No guard policy override configured")
	}
	return policy, policyJSON, err
}

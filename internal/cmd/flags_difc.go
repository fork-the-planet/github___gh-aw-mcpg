package cmd

// DIFC (Decentralized Information Flow Control) related flags

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/github/gh-aw-mcpg/internal/strutil"
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

func init() {
	RegisterFlag(func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&difcMode, "guards-mode", getDefaultDIFCMode(), "Guards enforcement mode: strict (deny violations), filter (remove denied tools), or propagate (auto-adjust agent labels on reads)")
		cmd.Flags().StringVar(&difcSinkServerIDs, "guards-sink-server-ids", getDefaultDIFCSinkServerIDs(), "Comma-separated server IDs whose RPC JSONL logs should include agent secrecy/integrity tag snapshots")
		cmd.Flags().StringVar(&guardPolicyJSON, "guard-policy-json", getDefaultGuardPolicyJSON(), "Guard policy JSON (e.g. {\"allow-only\":{\"repos\":\"public\",\"min-integrity\":\"none\"}})")
		cmd.Flags().BoolVar(&allowOnlyPublic, "allowonly-scope-public", getDefaultAllowOnlyScopePublic(), "Use public AllowOnly scope")
		cmd.Flags().StringVar(&allowOnlyOwner, "allowonly-scope-owner", getDefaultAllowOnlyScopeOwner(), "AllowOnly owner scope value")
		cmd.Flags().StringVar(&allowOnlyRepo, "allowonly-scope-repo", getDefaultAllowOnlyScopeRepo(), "AllowOnly repo name (requires owner)")
		cmd.Flags().StringVar(&allowOnlyMinInt, "allowonly-min-integrity", getDefaultAllowOnlyMinIntegrity(), "AllowOnly integrity: none|unapproved|approved|merged")
	})
}

// getDefaultDIFCMode returns the default guards mode, checking MCP_GATEWAY_GUARDS_MODE
// environment variable first, then falling back to the hardcoded default (strict)
func getDefaultDIFCMode() string {
	if envMode := os.Getenv("MCP_GATEWAY_GUARDS_MODE"); envMode != "" {
		mode := strings.ToLower(envMode)
		if isValidDIFCMode(mode) {
			debugLog.Printf("Guards mode set from MCP_GATEWAY_GUARDS_MODE: %s", mode)
			return mode
		}
		debugLog.Printf("MCP_GATEWAY_GUARDS_MODE value %q is invalid, falling back to default: %s", envMode, difc.ModeStrict)
	}
	return difc.ModeStrict
}

// isValidDIFCMode checks if the given mode string is a valid DIFC mode
func isValidDIFCMode(mode string) bool {
	for _, valid := range difc.ValidModes {
		if mode == valid {
			return true
		}
	}
	return false
}

func getDefaultDIFCSinkServerIDs() string {
	return os.Getenv("MCP_GATEWAY_GUARDS_SINK_SERVER_IDS")
}

func getDefaultGuardPolicyJSON() string {
	return os.Getenv("MCP_GATEWAY_GUARD_POLICY_JSON")
}

func getDefaultAllowOnlyScopePublic() bool {
	return envutil.GetEnvBool("MCP_GATEWAY_ALLOWONLY_SCOPE_PUBLIC", false)
}

func getDefaultAllowOnlyScopeOwner() string {
	return os.Getenv("MCP_GATEWAY_ALLOWONLY_SCOPE_OWNER")
}

func getDefaultAllowOnlyScopeRepo() string {
	return os.Getenv("MCP_GATEWAY_ALLOWONLY_SCOPE_REPO")
}

func getDefaultAllowOnlyMinIntegrity() string {
	return os.Getenv("MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY")
}

// ValidateDIFCMode validates the guards mode flag value and returns an error if invalid
func ValidateDIFCMode(mode string) error {
	if !isValidDIFCMode(strings.ToLower(mode)) {
		return fmt.Errorf("invalid guards mode %q: must be one of: %s, %s, %s", mode, difc.ModeStrict, difc.ModeFilter, difc.ModePropagate)
	}
	return nil
}

func parseDIFCSinkServerIDs(input string) ([]string, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}

	debugLog.Printf("Parsing DIFC sink server IDs: input=%q", input)

	parts := strings.Split(input, ",")
	validated := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, " \t\n\r") {
			return nil, fmt.Errorf("invalid guards sink server ID %q: whitespace is not allowed", value)
		}
		validated = append(validated, value)
	}

	result := strutil.DeduplicateStrings(validated, false)
	debugLog.Printf("Parsed %d unique DIFC sink server IDs: %v", len(result), result)
	return result, nil
}

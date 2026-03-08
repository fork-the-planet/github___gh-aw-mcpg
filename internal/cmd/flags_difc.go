package cmd

// DIFC (Decentralized Information Flow Control) related flags

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/spf13/cobra"
)

// DIFC flag defaults
const (
	defaultEnableDIFC       = false
	defaultConfigExtensions = false
)

// DIFC flag variables
var (
	enableDIFC        bool
	difcMode          string
	enableConfigExt   bool   // Enable config extensions (guards, session labels)
	sessionSecrecy    string // Comma-separated initial secrecy labels
	sessionIntegrity  string // Comma-separated initial integrity labels
	difcSinkServerIDs string // Comma-separated server IDs that should include DIFC tag snapshots in RPC JSONL logs
	guardPolicyJSON   string
	allowOnlyPublic   bool
	allowOnlyOwner    string
	allowOnlyRepo     string
	allowOnlyMinInt   string
)

func init() {
	RegisterFlag(func(cmd *cobra.Command) {
		cmd.Flags().BoolVar(&enableDIFC, "enable-difc", getDefaultEnableDIFC(), "Enable DIFC enforcement for information flow control")
		cmd.Flags().MarkHidden("enable-difc")
		cmd.Flags().StringVar(&difcMode, "difc-mode", getDefaultDIFCMode(), "DIFC enforcement mode: strict (deny violations), filter (remove denied tools), or propagate (auto-adjust agent labels on reads)")
		cmd.Flags().BoolVar(&enableConfigExt, "enable-config-extensions", getDefaultConfigExtensions(), "Enable config extensions (guards, session labels) - required for DIFC session label features")
		cmd.Flags().StringVar(&sessionSecrecy, "session-secrecy", getDefaultSessionSecrecy(), "Comma-separated initial secrecy labels for agent sessions (requires --enable-config-extensions)")
		cmd.Flags().StringVar(&sessionIntegrity, "session-integrity", getDefaultSessionIntegrity(), "Comma-separated initial integrity labels for agent sessions (requires --enable-config-extensions)")
		cmd.Flags().StringVar(&difcSinkServerIDs, "difc-sink-server-ids", getDefaultDIFCSinkServerIDs(), "Comma-separated server IDs whose RPC JSONL logs should include agent secrecy/integrity tag snapshots")
		cmd.Flags().StringVar(&guardPolicyJSON, "guard-policy-json", getDefaultGuardPolicyJSON(), "Guard policy JSON (e.g. {\"allow-only\":{\"repos\":\"public\",\"min-integrity\":\"none\"}})")
		cmd.Flags().BoolVar(&allowOnlyPublic, "allowonly-scope-public", getDefaultAllowOnlyScopePublic(), "Use public AllowOnly scope")
		cmd.Flags().StringVar(&allowOnlyOwner, "allowonly-scope-owner", getDefaultAllowOnlyScopeOwner(), "AllowOnly owner scope value")
		cmd.Flags().StringVar(&allowOnlyRepo, "allowonly-scope-repo", getDefaultAllowOnlyScopeRepo(), "AllowOnly repo name (requires owner)")
		cmd.Flags().StringVar(&allowOnlyMinInt, "allowonly-min-integrity", getDefaultAllowOnlyMinIntegrity(), "AllowOnly integrity: none|unapproved|approved|merged")
	})
}

// getDefaultEnableDIFC returns the default DIFC setting, checking MCP_GATEWAY_ENABLE_DIFC
// environment variable first, then falling back to the hardcoded default (false)
func getDefaultEnableDIFC() bool {
	return envutil.GetEnvBool("MCP_GATEWAY_ENABLE_DIFC", defaultEnableDIFC)
}

// getDefaultDIFCMode returns the default DIFC mode, checking MCP_GATEWAY_DIFC_MODE
// environment variable first, then falling back to the hardcoded default (strict)
func getDefaultDIFCMode() string {
	if envMode := os.Getenv("MCP_GATEWAY_DIFC_MODE"); envMode != "" {
		mode := strings.ToLower(envMode)
		if isValidDIFCMode(mode) {
			return mode
		}
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

// getDefaultConfigExtensions returns the default config extensions setting,
// checking MCP_GATEWAY_CONFIG_EXTENSIONS environment variable first
func getDefaultConfigExtensions() bool {
	return envutil.GetEnvBool("MCP_GATEWAY_CONFIG_EXTENSIONS", defaultConfigExtensions)
}

// getDefaultSessionSecrecy returns the default session secrecy labels from
// MCP_GATEWAY_SESSION_SECRECY environment variable
func getDefaultSessionSecrecy() string {
	return os.Getenv("MCP_GATEWAY_SESSION_SECRECY")
}

// getDefaultSessionIntegrity returns the default session integrity labels from
// MCP_GATEWAY_SESSION_INTEGRITY environment variable
func getDefaultSessionIntegrity() string {
	return os.Getenv("MCP_GATEWAY_SESSION_INTEGRITY")
}

func getDefaultDIFCSinkServerIDs() string {
	return os.Getenv("MCP_GATEWAY_DIFC_SINK_SERVER_IDS")
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

func buildAllowOnlyPolicy(public bool, owner, repo, minIntegrity string) (*config.GuardPolicy, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	integrityInput := strings.TrimSpace(minIntegrity)
	integrityKey := strings.ToLower(strings.ReplaceAll(integrityInput, "-", ""))

	integrityByInput := map[string]string{
		"none":       config.IntegrityNone,
		"unapproved": config.IntegrityUnapproved,
		"approved":   config.IntegrityApproved,
		"merged":     config.IntegrityMerged,
	}
	integrity, hasIntegrity := integrityByInput[integrityKey]

	scopeCount := 0
	if public {
		scopeCount++
	}
	if owner != "" {
		scopeCount++
	}
	if repo != "" && owner == "" {
		return nil, fmt.Errorf("allowonly scope repo requires allowonly scope owner")
	}

	if scopeCount == 0 && minIntegrity == "" {
		return nil, nil
	}
	if scopeCount != 1 {
		return nil, fmt.Errorf("exactly one AllowOnly scope variant must be set (public or owner[/repo])")
	}
	if integrityInput == "" {
		return nil, fmt.Errorf("allowonly integrity is required")
	}
	if !hasIntegrity {
		return nil, fmt.Errorf("allowonly integrity must be one of: none, unapproved, approved, merged")
	}

	var repos interface{}
	if public {
		repos = "public"
	} else {
		scope := owner + "/*"
		if repo != "" {
			scope = owner + "/" + repo
		}
		repos = []string{scope}
	}

	policy := &config.GuardPolicy{
		AllowOnly: &config.AllowOnlyPolicy{
			Repos:        repos,
			MinIntegrity: integrity,
		},
	}

	if err := config.ValidateGuardPolicy(policy); err != nil {
		return nil, err
	}

	return policy, nil
}

// ValidateDIFCMode validates the DIFC mode flag value and returns an error if invalid
func ValidateDIFCMode(mode string) error {
	if !isValidDIFCMode(strings.ToLower(mode)) {
		return fmt.Errorf("invalid DIFC mode %q: must be one of: %s, %s, %s", mode, difc.ModeStrict, difc.ModeFilter, difc.ModePropagate)
	}
	return nil
}

// parseSessionLabels parses a comma-separated string of labels into a slice
func parseSessionLabels(labels string) []string {
	if labels == "" {
		return nil
	}
	parts := strings.Split(labels, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func parseDIFCSinkServerIDs(input string) ([]string, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}

	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, " \t\n\r") {
			return nil, fmt.Errorf("invalid DIFC sink server ID %q: whitespace is not allowed", value)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result, nil
}

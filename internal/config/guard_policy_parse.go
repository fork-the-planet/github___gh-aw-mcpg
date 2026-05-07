package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/guard"
)

// ParseServerGuardPolicy parses a guard policy from a server-specific raw policy map.
// It handles both the modern allow-only/write-sink format and the legacy repos/min-integrity format.
// The serverID is used to look for a server-keyed nested policy map.
func ParseServerGuardPolicy(serverID string, raw map[string]interface{}) (*GuardPolicy, error) {
	logGuardPolicy.Printf("ParseServerGuardPolicy: serverID=%s, keyCount=%d", serverID, len(raw))
	if len(raw) == 0 {
		return nil, nil
	}

	if policy, err := ParsePolicyMap(raw); err != nil {
		return nil, err
	} else if policy != nil {
		return policy, nil
	}

	if nested, ok := raw[serverID]; ok {
		nestedMap, ok := nested.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid guard policy for server '%s': expected object", serverID)
		}
		if policy, err := ParsePolicyMap(nestedMap); err != nil {
			return nil, err
		} else if policy != nil {
			return policy, nil
		}
	}

	if len(raw) == 1 {
		for _, value := range raw {
			nestedMap, ok := value.(map[string]interface{})
			if !ok {
				continue
			}
			if policy, err := ParsePolicyMap(nestedMap); err != nil {
				return nil, err
			} else if policy != nil {
				return policy, nil
			}
		}
	}

	return nil, nil
}

// ParsePolicyMap parses a GuardPolicy from a raw map using either the modern
// allow-only/write-sink format or the legacy repos/min-integrity format.
func ParsePolicyMap(raw map[string]interface{}) (*GuardPolicy, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	hasAllowOnly := false
	if _, ok := raw["allow-only"]; ok {
		hasAllowOnly = true
	} else if _, ok := raw["allowonly"]; ok { // Accept legacy "allowonly" form for backward compatibility
		hasAllowOnly = true
	}

	hasWriteSink := false
	if _, ok := raw["write-sink"]; ok {
		hasWriteSink = true
	} else if _, ok := raw["writesink"]; ok {
		hasWriteSink = true
	}

	logGuardPolicy.Printf("ParsePolicyMap: hasAllowOnly=%v, hasWriteSink=%v, keyCount=%d", hasAllowOnly, hasWriteSink, len(raw))

	if hasAllowOnly || hasWriteSink {
		policyBytes, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize server guard policy: %w", err)
		}
		policy, err := ParseGuardPolicyJSON(string(policyBytes))
		if err != nil {
			return nil, fmt.Errorf("invalid server guard policy: %w", err)
		}
		return policy, nil
	}

	repos, hasRepos := raw["repos"]
	if !hasRepos {
		return nil, nil
	}

	integrityValue, hasIntegrity := raw["min-integrity"]
	if !hasIntegrity {
		integrityValue, hasIntegrity = raw["integrity"]
	}
	if !hasIntegrity {
		return nil, fmt.Errorf("invalid server guard policy: repos specified without min-integrity")
	}

	policy := &GuardPolicy{
		AllowOnly: &AllowOnlyPolicy{
			Repos:        repos,
			MinIntegrity: fmt.Sprintf("%v", integrityValue),
		},
	}
	if err := ValidateGuardPolicy(policy); err != nil {
		return nil, fmt.Errorf("invalid server guard policy: %w", err)
	}

	return policy, nil
}

// BuildAllowOnlyPolicy constructs an AllowOnly GuardPolicy from the provided parameters.
// Exactly one of public or owner must be set. If repo is set, owner must also be set.
// Returns nil, nil if no scope or integrity is specified (indicating no policy).
func BuildAllowOnlyPolicy(public bool, owner, repo, minIntegrity string) (*GuardPolicy, error) {
	logGuardPolicy.Printf("Building AllowOnly policy: public=%v, owner=%q, repo=%q, minIntegrity=%q", public, owner, repo, minIntegrity)

	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	integrityInput := strings.TrimSpace(minIntegrity)
	integrityKey := strings.ToLower(strings.ReplaceAll(integrityInput, "-", ""))

	integrityByInput := map[string]string{
		"none":       IntegrityNone,
		"unapproved": IntegrityUnapproved,
		"approved":   IntegrityApproved,
		"merged":     IntegrityMerged,
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
		return nil, fmt.Errorf("allow-only scope repo requires allow-only scope owner")
	}

	if scopeCount == 0 && minIntegrity == "" {
		logGuardPolicy.Print("No AllowOnly scope or integrity specified, returning nil policy")
		return nil, nil
	}
	if scopeCount != 1 {
		return nil, fmt.Errorf("exactly one AllowOnly scope variant must be set (public or owner[/repo])")
	}
	if integrityInput == "" {
		return nil, fmt.Errorf("min-integrity is required")
	}
	if !hasIntegrity {
		return nil, fmt.Errorf("min-integrity must be one of: %s", strings.Join(guard.AllowedIntegrityLevels, ", "))
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

	logGuardPolicy.Printf("AllowOnly policy scope resolved: repos=%v, minIntegrity=%s", repos, integrity)

	policy := &GuardPolicy{
		AllowOnly: &AllowOnlyPolicy{
			Repos:        repos,
			MinIntegrity: integrity,
		},
	}

	if err := ValidateGuardPolicy(policy); err != nil {
		return nil, err
	}

	logGuardPolicy.Print("AllowOnly policy built and validated successfully")
	return policy, nil
}

// ParseGuardPolicyJSON parses policy JSON and validates it.
func ParseGuardPolicyJSON(policyJSON string) (*GuardPolicy, error) {
	logGuardPolicy.Printf("Parsing guard policy JSON: len=%d", len(policyJSON))
	policy := &GuardPolicy{}
	if err := json.Unmarshal([]byte(policyJSON), policy); err != nil {
		return nil, fmt.Errorf("invalid guard policy JSON: %w", err)
	}
	if err := ValidateGuardPolicy(policy); err != nil {
		return nil, err
	}
	return policy, nil
}

// NormalizeScopeKind returns a copy of the policy map with the scope_kind field
// normalized to lowercase trimmed string form. Other fields are preserved as-is.
func NormalizeScopeKind(policy map[string]interface{}) map[string]interface{} {
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

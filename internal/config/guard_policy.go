package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	IntegrityNone       = "none"
	IntegrityUnapproved = "unapproved"
	IntegrityApproved   = "approved"
	IntegrityMerged     = "merged"
)

const (
	integrityNoneValue       = "none"
	integrityUnapprovedValue = "unapproved"
	integrityApprovedValue   = "approved"
	integrityMergedValue     = "merged"
)

var validMinIntegrityValues = map[string]struct{}{
	integrityNoneValue:       {},
	integrityUnapprovedValue: {},
	integrityApprovedValue:   {},
	integrityMergedValue:     {},
}

// GuardPolicy represents the policy payload passed to guard label_agent.
type GuardPolicy struct {
	AllowOnly *AllowOnlyPolicy `toml:"AllowOnly" json:"allow-only,omitempty"`
}

// AllowOnlyPolicy configures scope and minimum required integrity.
type AllowOnlyPolicy struct {
	Repos        interface{} `toml:"Repos" json:"repos"`
	MinIntegrity string      `toml:"MinIntegrity" json:"min-integrity"`
}

// NormalizedGuardPolicy is a canonical policy representation for caching and observability.
type NormalizedGuardPolicy struct {
	ScopeKind    string   `json:"scope_kind"`
	ScopeValues  []string `json:"scope_values,omitempty"`
	MinIntegrity string   `json:"min-integrity"`
}

func (p *GuardPolicy) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	var allowOnlyRaw json.RawMessage
	for key, value := range raw {
		switch strings.ToLower(key) {
		case "allow-only", "allowonly":
			allowOnlyRaw = value
		default:
			return fmt.Errorf("policy contains unsupported field %q", key)
		}
	}

	if len(allowOnlyRaw) == 0 {
		return fmt.Errorf("policy must include allow-only")
	}

	var allowOnly AllowOnlyPolicy
	if err := json.Unmarshal(allowOnlyRaw, &allowOnly); err != nil {
		return err
	}
	p.AllowOnly = &allowOnly
	return nil
}

func (p GuardPolicy) MarshalJSON() ([]byte, error) {
	type serializedPolicy struct {
		AllowOnly *AllowOnlyPolicy `json:"allow-only,omitempty"`
	}

	return json.Marshal(serializedPolicy(p))
}

func (p *AllowOnlyPolicy) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	for key, value := range raw {
		switch strings.ToLower(key) {
		case "repos":
			if err := json.Unmarshal(value, &p.Repos); err != nil {
				return fmt.Errorf("invalid allow-only.repos: %w", err)
			}
		case "min-integrity", "integrity":
			if err := json.Unmarshal(value, &p.MinIntegrity); err != nil {
				return fmt.Errorf("invalid allow-only.min-integrity: %w", err)
			}
		default:
			return fmt.Errorf("allow-only contains unsupported field %q", key)
		}
	}

	if p.Repos == nil {
		return fmt.Errorf("allow-only must include repos")
	}
	if strings.TrimSpace(p.MinIntegrity) == "" {
		return fmt.Errorf("allow-only must include min-integrity")
	}

	return nil
}

func (p AllowOnlyPolicy) MarshalJSON() ([]byte, error) {
	type serializedAllowOnly struct {
		Repos        interface{} `json:"repos"`
		MinIntegrity string      `json:"min-integrity"`
	}

	return json.Marshal(serializedAllowOnly(p))
}

// ValidateGuardPolicy validates AllowOnly policy input.
func ValidateGuardPolicy(policy *GuardPolicy) error {
	_, err := NormalizeGuardPolicy(policy)
	return err
}

// NormalizeGuardPolicy validates and normalizes policy shape.
func NormalizeGuardPolicy(policy *GuardPolicy) (*NormalizedGuardPolicy, error) {
	if policy == nil || policy.AllowOnly == nil {
		return nil, fmt.Errorf("policy must include allow-only")
	}

	integrity := strings.ToLower(strings.TrimSpace(policy.AllowOnly.MinIntegrity))
	if _, ok := validMinIntegrityValues[integrity]; !ok {
		return nil, fmt.Errorf("allow-only.min-integrity must be one of: none, unapproved, approved, merged")
	}

	normalized := &NormalizedGuardPolicy{MinIntegrity: integrity}

	switch scope := policy.AllowOnly.Repos.(type) {
	case string:
		scopeValue := strings.ToLower(strings.TrimSpace(scope))
		if scopeValue != "all" && scopeValue != "public" {
			return nil, fmt.Errorf("allow-only.repos string must be 'all' or 'public'")
		}
		normalized.ScopeKind = scopeValue
		return normalized, nil

	case []interface{}:
		scopes, err := normalizeAndValidateScopeArray(scope)
		if err != nil {
			return nil, err
		}
		normalized.ScopeKind = "scoped"
		normalized.ScopeValues = scopes
		return normalized, nil

	case []string:
		generic := make([]interface{}, len(scope))
		for i := range scope {
			generic[i] = scope[i]
		}
		scopes, err := normalizeAndValidateScopeArray(generic)
		if err != nil {
			return nil, err
		}
		normalized.ScopeKind = "scoped"
		normalized.ScopeValues = scopes
		return normalized, nil

	default:
		return nil, fmt.Errorf("allow-only.repos must be 'all', 'public', or a non-empty array of repo scope strings")
	}
}

func normalizeAndValidateScopeArray(scopes []interface{}) ([]string, error) {
	if len(scopes) == 0 {
		return nil, fmt.Errorf("allow-only.repos array must contain at least one scope")
	}

	seen := make(map[string]struct{}, len(scopes))
	normalized := make([]string, 0, len(scopes))

	for _, scopeValue := range scopes {
		scopeString, ok := scopeValue.(string)
		if !ok {
			return nil, fmt.Errorf("allow-only.repos array values must be strings")
		}

		scopeString = strings.TrimSpace(scopeString)
		if scopeString == "" {
			return nil, fmt.Errorf("allow-only.repos scope entries must not be empty")
		}

		if !isValidRepoScope(scopeString) {
			return nil, fmt.Errorf("allow-only.repos scope %q is invalid; expected owner/*, owner/repo, or owner/re*", scopeString)
		}

		if _, exists := seen[scopeString]; exists {
			return nil, fmt.Errorf("allow-only.repos must not contain duplicates")
		}
		seen[scopeString] = struct{}{}
		normalized = append(normalized, scopeString)
	}

	sort.Strings(normalized)
	return normalized, nil
}

func isValidRepoScope(scope string) bool {
	parts := strings.Split(scope, "/")
	if len(parts) != 2 {
		return false
	}

	owner := parts[0]
	repoPart := parts[1]

	if !isValidRepoOwner(owner) {
		return false
	}

	if repoPart == "*" {
		return true
	}

	if strings.Count(repoPart, "*") > 1 {
		return false
	}

	isPrefixWildcard := strings.HasSuffix(repoPart, "*")
	if strings.Contains(repoPart, "*") && !isPrefixWildcard {
		return false
	}

	repoName := repoPart
	if isPrefixWildcard {
		repoName = strings.TrimSuffix(repoPart, "*")
		if repoName == "" {
			return false
		}
	}

	if !isValidRepoName(repoName) {
		return false
	}

	if isPrefixWildcard && strings.HasSuffix(repoName, ".") {
		return false
	}

	return true
}

func isValidRepoOwner(owner string) bool {
	if len(owner) < 1 || len(owner) > 39 {
		return false
	}

	for i := 0; i < len(owner); i++ {
		char := owner[i]
		if isScopeTokenChar(char) {
			continue
		}
		return false
	}

	return true
}

func isValidRepoName(repo string) bool {
	if len(repo) < 1 || len(repo) > 100 {
		return false
	}

	for i := 0; i < len(repo); i++ {
		char := repo[i]
		if isScopeTokenChar(char) {
			continue
		}
		return false
	}

	return true
}

func isScopeTokenChar(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-'
}

// ParseGuardPolicyJSON parses policy JSON and validates it.
func ParseGuardPolicyJSON(policyJSON string) (*GuardPolicy, error) {
	policy := &GuardPolicy{}
	if err := json.Unmarshal([]byte(policyJSON), policy); err != nil {
		return nil, fmt.Errorf("invalid guard policy JSON: %w", err)
	}
	if err := ValidateGuardPolicy(policy); err != nil {
		return nil, err
	}
	return policy, nil
}

func validateGuardPolicies(cfg *Config) error {
	for name, guardCfg := range cfg.Guards {
		if guardCfg != nil && guardCfg.Policy != nil {
			if err := ValidateGuardPolicy(guardCfg.Policy); err != nil {
				return fmt.Errorf("invalid policy for guard '%s': %w", name, err)
			}
		}
	}
	return nil
}

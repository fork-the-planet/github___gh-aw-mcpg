package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logGuardPolicy = logger.New("config:guard_policy")

const (
	IntegrityNone       = "none"
	IntegrityUnapproved = "unapproved"
	IntegrityApproved   = "approved"
	IntegrityMerged     = "merged"
)

var validMinIntegrityValues = map[string]struct{}{
	IntegrityNone:       {},
	IntegrityUnapproved: {},
	IntegrityApproved:   {},
	IntegrityMerged:     {},
}

// GuardPolicy represents the policy payload passed to guard label_agent.
type GuardPolicy struct {
	AllowOnly *AllowOnlyPolicy `toml:"AllowOnly" json:"allow-only,omitempty"`
	WriteSink *WriteSinkPolicy `toml:"WriteSink" json:"write-sink,omitempty"`
}

// WriteSinkPolicy configures a write-sink guard that accepts writes from
// agents carrying the listed secrecy labels.
type WriteSinkPolicy struct {
	Accept []string `toml:"Accept" json:"accept"`
}

// AllowOnlyPolicy configures scope and minimum required integrity.
type AllowOnlyPolicy struct {
	Repos          interface{} `toml:"Repos" json:"repos"`
	MinIntegrity   string      `toml:"MinIntegrity" json:"min-integrity"`
	BlockedUsers   []string    `toml:"BlockedUsers" json:"blocked-users,omitempty"`
	ApprovalLabels []string    `toml:"ApprovalLabels" json:"approval-labels,omitempty"`
	TrustedUsers   []string    `toml:"TrustedUsers" json:"trusted-users,omitempty"`
}

// NormalizedGuardPolicy is a canonical policy representation for caching and observability.
type NormalizedGuardPolicy struct {
	ScopeKind      string   `json:"scope_kind"`
	ScopeValues    []string `json:"scope_values,omitempty"`
	MinIntegrity   string   `json:"min-integrity"`
	BlockedUsers   []string `json:"blocked-users,omitempty"`
	ApprovalLabels []string `json:"approval-labels,omitempty"`
	TrustedUsers   []string `json:"trusted-users,omitempty"`
}

func (p *GuardPolicy) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	var allowOnlyRaw json.RawMessage
	var writeSinkRaw json.RawMessage
	for key, value := range raw {
		switch strings.ToLower(key) {
		case "allow-only", "allowonly":
			allowOnlyRaw = value
		case "write-sink", "writesink":
			writeSinkRaw = value
		default:
			return fmt.Errorf("policy contains unsupported field %q", key)
		}
	}

	if len(allowOnlyRaw) == 0 && len(writeSinkRaw) == 0 {
		return fmt.Errorf("policy must include allow-only or write-sink")
	}
	if len(allowOnlyRaw) > 0 && len(writeSinkRaw) > 0 {
		return fmt.Errorf("policy must include either allow-only or write-sink, not both")
	}

	if len(allowOnlyRaw) > 0 {
		var allowOnly AllowOnlyPolicy
		if err := json.Unmarshal(allowOnlyRaw, &allowOnly); err != nil {
			return err
		}
		p.AllowOnly = &allowOnly
	}

	if len(writeSinkRaw) > 0 {
		var writeSink WriteSinkPolicy
		if err := json.Unmarshal(writeSinkRaw, &writeSink); err != nil {
			return err
		}
		p.WriteSink = &writeSink
	}

	return nil
}

func (p GuardPolicy) MarshalJSON() ([]byte, error) {
	type serializedPolicy struct {
		AllowOnly *AllowOnlyPolicy `json:"allow-only,omitempty"`
		WriteSink *WriteSinkPolicy `json:"write-sink,omitempty"`
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
		case "blocked-users":
			if err := json.Unmarshal(value, &p.BlockedUsers); err != nil {
				return fmt.Errorf("invalid allow-only.blocked-users: %w", err)
			}
		case "approval-labels":
			if err := json.Unmarshal(value, &p.ApprovalLabels); err != nil {
				return fmt.Errorf("invalid allow-only.approval-labels: %w", err)
			}
		case "trusted-users":
			if err := json.Unmarshal(value, &p.TrustedUsers); err != nil {
				return fmt.Errorf("invalid allow-only.trusted-users: %w", err)
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
		Repos          interface{} `json:"repos"`
		MinIntegrity   string      `json:"min-integrity"`
		BlockedUsers   []string    `json:"blocked-users,omitempty"`
		ApprovalLabels []string    `json:"approval-labels,omitempty"`
		TrustedUsers   []string    `json:"trusted-users,omitempty"`
	}

	return json.Marshal(serializedAllowOnly(p))
}

// ValidateGuardPolicy validates AllowOnly or WriteSink policy input.
func ValidateGuardPolicy(policy *GuardPolicy) error {
	if policy == nil {
		logGuardPolicy.Print("ValidateGuardPolicy: policy is nil")
		return fmt.Errorf("policy must include allow-only or write-sink")
	}
	if policy.WriteSink != nil {
		logGuardPolicy.Printf("ValidateGuardPolicy: delegating to write-sink validation, acceptCount=%d", len(policy.WriteSink.Accept))
		return ValidateWriteSinkPolicy(policy.WriteSink)
	}
	logGuardPolicy.Print("ValidateGuardPolicy: delegating to allow-only normalization")
	_, err := NormalizeGuardPolicy(policy)
	return err
}

// ValidateWriteSinkPolicy validates a write-sink policy.
func ValidateWriteSinkPolicy(ws *WriteSinkPolicy) error {
	if ws == nil {
		return fmt.Errorf("write-sink policy must not be nil")
	}
	logGuardPolicy.Printf("ValidateWriteSinkPolicy: acceptCount=%d", len(ws.Accept))
	if len(ws.Accept) == 0 {
		return fmt.Errorf("write-sink.accept must contain at least one entry")
	}
	// Special case: ["*"] is a valid wildcard that accepts all writes
	if len(ws.Accept) == 1 && strings.TrimSpace(ws.Accept[0]) == "*" {
		logGuardPolicy.Print("ValidateWriteSinkPolicy: wildcard accept, policy is valid")
		return nil
	}
	seen := make(map[string]struct{})
	for _, entry := range ws.Accept {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return fmt.Errorf("write-sink.accept entries must not be empty")
		}
		if entry == "*" {
			return fmt.Errorf("write-sink.accept wildcard \"*\" must be the only entry")
		}
		if _, exists := seen[entry]; exists {
			return fmt.Errorf("write-sink.accept must not contain duplicates")
		}
		seen[entry] = struct{}{}
		if err := validateAcceptEntry(entry); err != nil {
			return fmt.Errorf("write-sink.accept entry %q is invalid: %w", entry, err)
		}
	}
	return nil
}

// validateAcceptEntry validates a single accept entry.
// Accepted formats:
//   - "visibility:owner/repo-pattern" (e.g., "private:github/gh-aw*")
//   - "visibility:owner"              (e.g., "private:myorg" — for owner-wildcard scopes)
//   - "owner/repo-pattern"            (e.g., "github/gh-aw*" — without visibility prefix)
//   - "owner"                         (e.g., "myorg" — bare owner without visibility prefix)
//
// The accept entries must match the secrecy tags produced by the GitHub guard's
// label_agent. See WriteSinkAcceptRules for the mapping from allow-only repos
// to the required accept values.
func validateAcceptEntry(entry string) error {
	scope := entry
	if idx := strings.Index(entry, ":"); idx > 0 {
		visibility := entry[:idx]
		scope = entry[idx+1:]
		validVisibility := map[string]bool{
			"private": true, "public": true, "internal": true,
		}
		if !validVisibility[visibility] {
			return fmt.Errorf("visibility prefix must be private, public, or internal; got %q", visibility)
		}
	}
	// Accept either "owner/repo-pattern" or bare "owner" (for owner-wildcard scopes
	// where repos=["owner/*"] produces agent secrecy "private:owner")
	if !isValidRepoScope(scope) && !isValidRepoOwner(scope) {
		return fmt.Errorf("scope %q is invalid; expected owner, owner/*, owner/repo, or owner/re*", scope)
	}
	return nil
}

// WriteSinkAcceptRules documents the mapping from allow-only repos configuration
// to the required write-sink accept values.
//
// The write-sink accept field must be a superset of the agent's secrecy tags,
// which are determined by the allow-only repos configuration:
//
//	repos = "all"              → agent secrecy = []           → accept = ["*"] (wildcard)
//	repos = "public"           → agent secrecy = []           → accept = ["*"] (wildcard)
//	repos = ["O/R"]            → agent secrecy = ["private:O/R"]
//	                             accept = ["private:O/R"]
//	repos = ["O/*"]            → agent secrecy = ["private:O"]
//	                             accept = ["private:O"]
//	repos = ["O/P*"]           → agent secrecy = ["private:O/P*"]
//	                             accept = ["private:O/P*"]
//	repos = ["O/R1", "O/R2"]  → agent secrecy = ["private:O/R1", "private:O/R2"]
//	                             accept = ["private:O/R1", "private:O/R2"]
//	repos = ["O1/*", "O2/R"]  → agent secrecy = ["private:O1", "private:O2/R"]
//	                             accept = ["private:O1", "private:O2/R"]
//
// The transformation rule:
//
//	repos entry "O/*"  (owner wildcard)  → accept "private:O"    (bare owner)
//	repos entry "O/P*" (prefix wildcard) → accept "private:O/P*" (prefix preserved)
//	repos entry "O/R"  (exact repo)      → accept "private:O/R"  (exact preserved)
//
// Wildcard accept:
//
//	accept = ["*"] means "accept writes from any agent regardless of secrecy".
//	This is the correct configuration for repos="all" and repos="public" where
//	the agent has no secrecy tags. The write-sink is still required to prevent
//	the noop guard integrity violation (see WriteSinkGuard godoc).
//	The wildcard "*" must be the sole entry — it cannot be mixed with other patterns.
//
// Note: min-integrity has no effect on these rules (it only affects integrity labels).
var WriteSinkAcceptRules = "see godoc" // exists for documentation only

// IsWriteSinkPolicy returns true if this policy configures a write-sink guard.
func (p *GuardPolicy) IsWriteSinkPolicy() bool {
	return p != nil && p.WriteSink != nil
}

// NormalizeGuardPolicy validates and normalizes an allow-only policy shape.
func NormalizeGuardPolicy(policy *GuardPolicy) (*NormalizedGuardPolicy, error) {
	if policy == nil || (policy.AllowOnly == nil && policy.WriteSink == nil) {
		return nil, fmt.Errorf("policy must include allow-only or write-sink")
	}
	if policy.AllowOnly == nil {
		// Write-sink policies don't produce a NormalizedGuardPolicy
		return nil, fmt.Errorf("policy must include allow-only")
	}

	integrity := strings.ToLower(strings.TrimSpace(policy.AllowOnly.MinIntegrity))
	if _, ok := validMinIntegrityValues[integrity]; !ok {
		return nil, fmt.Errorf("allow-only.min-integrity must be one of: none, unapproved, approved, merged")
	}

	normalized := &NormalizedGuardPolicy{MinIntegrity: integrity}

	logGuardPolicy.Printf("Normalizing guard policy: integrity=%s, reposType=%T", integrity, policy.AllowOnly.Repos)

	// Validate and normalize blocked-users.
	// Dedup uses lowercased keys to match Rust guard's case-insensitive comparison.
	if len(policy.AllowOnly.BlockedUsers) > 0 {
		seen := make(map[string]struct{}, len(policy.AllowOnly.BlockedUsers))
		for _, u := range policy.AllowOnly.BlockedUsers {
			u = strings.TrimSpace(u)
			if u == "" {
				return nil, fmt.Errorf("allow-only.blocked-users entries must not be empty")
			}
			key := strings.ToLower(u)
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				normalized.BlockedUsers = append(normalized.BlockedUsers, u)
			}
		}
	}

	// Validate and normalize approval-labels.
	// Dedup uses lowercased keys to match Rust guard's case-insensitive comparison.
	if len(policy.AllowOnly.ApprovalLabels) > 0 {
		seen := make(map[string]struct{}, len(policy.AllowOnly.ApprovalLabels))
		for _, l := range policy.AllowOnly.ApprovalLabels {
			l = strings.TrimSpace(l)
			if l == "" {
				return nil, fmt.Errorf("allow-only.approval-labels entries must not be empty")
			}
			key := strings.ToLower(l)
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				normalized.ApprovalLabels = append(normalized.ApprovalLabels, l)
			}
		}
	}

	// Validate and normalize trusted-users.
	// Dedup uses lowercased keys to match Rust guard's case-insensitive comparison.
	if len(policy.AllowOnly.TrustedUsers) > 0 {
		seen := make(map[string]struct{}, len(policy.AllowOnly.TrustedUsers))
		for _, u := range policy.AllowOnly.TrustedUsers {
			u = strings.TrimSpace(u)
			if u == "" {
				return nil, fmt.Errorf("allow-only.trusted-users entries must not be empty")
			}
			key := strings.ToLower(u)
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				normalized.TrustedUsers = append(normalized.TrustedUsers, u)
			}
		}
	}

	switch scope := policy.AllowOnly.Repos.(type) {
	case string:
		scopeValue := strings.ToLower(strings.TrimSpace(scope))
		if scopeValue != "all" && scopeValue != "public" {
			return nil, fmt.Errorf("allow-only.repos string must be 'all' or 'public'")
		}
		normalized.ScopeKind = scopeValue
		logGuardPolicy.Printf("Guard policy normalized: scopeKind=%s, integrity=%s", normalized.ScopeKind, normalized.MinIntegrity)
		return normalized, nil

	case []interface{}:
		scopes, err := normalizeAndValidateScopeArray(scope)
		if err != nil {
			return nil, err
		}
		normalized.ScopeKind = "scoped"
		normalized.ScopeValues = scopes
		logGuardPolicy.Printf("Guard policy normalized: scopeKind=scoped, scopeCount=%d, integrity=%s", len(scopes), normalized.MinIntegrity)
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
		logGuardPolicy.Printf("Guard policy normalized: scopeKind=scoped, scopeCount=%d, integrity=%s", len(scopes), normalized.MinIntegrity)
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

// isValidTokenString returns true if s is a non-empty string of at most maxLen
// lowercase-alphanumeric, underscore, or hyphen characters.
func isValidTokenString(s string, maxLen int) bool {
	if len(s) < 1 || len(s) > maxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isScopeTokenChar(s[i]) {
			return false
		}
	}
	return true
}

func isValidRepoOwner(owner string) bool {
	return isValidTokenString(owner, 39)
}

func isValidRepoName(repo string) bool {
	return isValidTokenString(repo, 100)
}

func isScopeTokenChar(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-'
}

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
		return nil, fmt.Errorf("min-integrity must be one of: none, unapproved, approved, merged")
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

func validateGuardPolicies(cfg *Config) error {
	logGuardPolicy.Printf("Validating guard policies: count=%d", len(cfg.Guards))
	for name, guardCfg := range cfg.Guards {
		if guardCfg != nil && guardCfg.Policy != nil {
			if err := ValidateGuardPolicy(guardCfg.Policy); err != nil {
				return fmt.Errorf("invalid policy for guard '%s': %w", name, err)
			}
		}
	}
	return nil
}

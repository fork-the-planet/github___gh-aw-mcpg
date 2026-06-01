package config

import (
	"fmt"
	"sort"
	"strings"
)

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
		return nil, fmt.Errorf("allow-only.min-integrity must be one of: %s", strings.Join(allowedGuardPolicyIntegrityLevels, ", "))
	}

	normalized := &NormalizedGuardPolicy{MinIntegrity: integrity}

	logGuardPolicy.Printf("Normalizing guard policy: integrity=%s, reposType=%T", integrity, policy.AllowOnly.Repos)

	var err error

	normalized.ToolCallLimits, err = normalizeToolCallLimits(policy.AllowOnly.ToolCallLimits)
	if err != nil {
		return nil, err
	}

	// Validate and normalize blocked-users, approval-labels, trusted-users.
	// Dedup uses lowercased keys; original trimmed values are stored.
	normalized.BlockedUsers, err = normalizeStringSlice("blocked-users", policy.AllowOnly.BlockedUsers, strings.ToLower, false)
	if err != nil {
		return nil, err
	}
	normalized.ApprovalLabels, err = normalizeStringSlice("approval-labels", policy.AllowOnly.ApprovalLabels, strings.ToLower, false)
	if err != nil {
		return nil, err
	}
	normalized.TrustedUsers, err = normalizeStringSlice("trusted-users", policy.AllowOnly.TrustedUsers, strings.ToLower, false)
	if err != nil {
		return nil, err
	}

	// Validate and normalize endorsement-reactions and disapproval-reactions.
	// Dedup uses uppercased keys; uppercased values are stored to match the GraphQL ReactionContent enum.
	normalized.EndorsementReactions, err = normalizeStringSlice("endorsement-reactions", policy.AllowOnly.EndorsementReactions, strings.ToUpper, true)
	if err != nil {
		return nil, err
	}
	normalized.DisapprovalReactions, err = normalizeStringSlice("disapproval-reactions", policy.AllowOnly.DisapprovalReactions, strings.ToUpper, true)
	if err != nil {
		return nil, err
	}

	// Validate and normalize disapproval-integrity (optional; empty means feature
	// uses Rust-side default of "none" when endorsement/disapproval is evaluated).
	if v := strings.ToLower(strings.TrimSpace(policy.AllowOnly.DisapprovalIntegrity)); v != "" {
		if _, ok := validMinIntegrityValues[v]; !ok {
			return nil, fmt.Errorf("allow-only.disapproval-integrity must be one of: %s", strings.Join(allowedGuardPolicyIntegrityLevels, ", "))
		}
		normalized.DisapprovalIntegrity = v
	}

	// Validate and normalize endorser-min-integrity (optional; empty means feature
	// uses Rust-side default of "approved" when evaluating reactor eligibility).
	if v := strings.ToLower(strings.TrimSpace(policy.AllowOnly.EndorserMinIntegrity)); v != "" {
		if _, ok := validMinIntegrityValues[v]; !ok {
			return nil, fmt.Errorf("allow-only.endorser-min-integrity must be one of: %s", strings.Join(allowedGuardPolicyIntegrityLevels, ", "))
		}
		normalized.EndorserMinIntegrity = v
	}

	// Pass through promotion-label and demotion-label as-is (validated by Rust guard).
	if v := strings.TrimSpace(policy.AllowOnly.PromotionLabel); v != "" {
		normalized.PromotionLabel = v
	}
	if v := strings.TrimSpace(policy.AllowOnly.DemotionLabel); v != "" {
		normalized.DemotionLabel = v
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
	logGuardPolicy.Printf("normalizeAndValidateScopeArray: validating %d repo scope entries", len(scopes))

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

// normalizeStringSlice trims, validates, deduplicates, and normalizes entries
// for the named allow-only field. caseNorm maps each trimmed entry to its
// deduplication key. When storeNorm is true the normalized key is stored;
// otherwise the original trimmed value is stored.
func normalizeStringSlice(field string, input []string, caseNorm func(string) string, storeNorm bool) ([]string, error) {
	if len(input) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, v := range input {
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, fmt.Errorf("allow-only.%s entries must not be empty", field)
		}
		key := caseNorm(v)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			if storeNorm {
				out = append(out, key)
			} else {
				out = append(out, v)
			}
		}
	}
	return out, nil
}

func normalizeToolCallLimits(input map[string]int) (map[string]int, error) {
	if len(input) == 0 {
		return nil, nil
	}

	out := make(map[string]int, len(input))
	for toolName, limit := range input {
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			return nil, fmt.Errorf("allow-only.tool-call-limits keys must not be empty")
		}
		if limit < 0 {
			return nil, fmt.Errorf("allow-only.tool-call-limits[%q] must be >= 0", toolName)
		}
		out[toolName] = limit
	}
	return out, nil
}

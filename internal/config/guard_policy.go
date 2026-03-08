// Package config provides configuration loading and parsing.
// This file defines guard policy configuration and helpers.
package config

// GuardPolicyConfig provides a type-safe interface for accessing guard policies
// from ServerConfig. Guard policies control access to MCP server resources.
//
// Structure (GitHub MCP Server):
//   - repos: "all", "public", or array of patterns (e.g., ["owner/repo", "owner/*"])
//   - min-integrity: "none", "unapproved", "approved", or "merged"
//
// The guard policies are stored as map[string]interface{} to support
// server-specific schemas without forcing all servers to use the same structure.
type GuardPolicyConfig struct {
	policies map[string]interface{}
}

// NewGuardPolicyConfig creates a GuardPolicyConfig wrapper around the raw policies map.
func NewGuardPolicyConfig(policies map[string]interface{}) *GuardPolicyConfig {
	if policies == nil {
		return &GuardPolicyConfig{policies: make(map[string]interface{})}
	}
	return &GuardPolicyConfig{policies: policies}
}

// GetPolicy returns the policy configuration for a specific service (e.g., "github", "slack").
// Returns nil if the service has no policy configured.
func (gp *GuardPolicyConfig) GetPolicy(service string) map[string]interface{} {
	if gp.policies == nil {
		return nil
	}
	if policy, ok := gp.policies[service].(map[string]interface{}); ok {
		return policy
	}
	return nil
}

// HasPolicy returns true if a policy is configured for the given service.
func (gp *GuardPolicyConfig) HasPolicy(service string) bool {
	return gp.GetPolicy(service) != nil
}

// IsEmpty returns true if no policies are configured.
func (gp *GuardPolicyConfig) IsEmpty() bool {
	return len(gp.policies) == 0
}

// GetGuardPolicies is a helper method on ServerConfig for type-safe access to guard policies.
func (sc *ServerConfig) GetGuardPolicies() *GuardPolicyConfig {
	return NewGuardPolicyConfig(sc.GuardPolicies)
}

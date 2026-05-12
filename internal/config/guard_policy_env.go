package config

// Environment variable names for guard policy configuration.
const (
	EnvGuardPolicyJSON       = "MCP_GATEWAY_GUARD_POLICY_JSON"
	EnvAllowOnlyScopePublic  = "MCP_GATEWAY_ALLOWONLY_SCOPE_PUBLIC"
	EnvAllowOnlyScopeOwner   = "MCP_GATEWAY_ALLOWONLY_SCOPE_OWNER"
	EnvAllowOnlyScopeRepo    = "MCP_GATEWAY_ALLOWONLY_SCOPE_REPO"
	EnvAllowOnlyMinIntegrity = "MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY"
)

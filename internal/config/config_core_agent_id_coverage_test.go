package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEffectiveAgentID tests all branches of GatewayConfig.effectiveAgentID.
//
// effectiveAgentID is an unexported helper that returns the resolved agent ID:
//   - nil receiver → ""
//   - agentIDExplicit set → return AgentID (even if empty)
//   - AgentID non-empty (and not explicit) → return AgentID
//   - fallback → return APIKey
func TestEffectiveAgentID(t *testing.T) {
	tests := []struct {
		name    string
		gateway *GatewayConfig
		want    string
	}{
		{
			name:    "nil receiver returns empty string",
			gateway: nil,
			want:    "",
		},
		{
			name: "explicit agent_id returns AgentID",
			gateway: &GatewayConfig{
				AgentID:         "explicit-agent",
				agentIDExplicit: true,
			},
			want: "explicit-agent",
		},
		{
			name: "explicit agent_id even when empty returns empty AgentID",
			gateway: &GatewayConfig{
				AgentID:         "",
				APIKey:          "legacy-key",
				agentIDExplicit: true,
			},
			want: "",
		},
		{
			name: "non-explicit non-empty AgentID returns AgentID",
			gateway: &GatewayConfig{
				AgentID:         "derived-agent",
				APIKey:          "legacy-key",
				agentIDExplicit: false,
			},
			want: "derived-agent",
		},
		{
			name: "non-explicit empty AgentID falls back to APIKey",
			gateway: &GatewayConfig{
				AgentID:         "",
				APIKey:          "fallback-key",
				agentIDExplicit: false,
			},
			want: "fallback-key",
		},
		{
			name:    "zero-value GatewayConfig returns empty string",
			gateway: &GatewayConfig{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gateway.effectiveAgentID()
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestNormalizeAgentID tests all branches of GatewayConfig.normalizeAgentID.
//
// normalizeAgentID resolves the precedence between the preferred agent_id field
// and the deprecated api_key alias. It is called after TOML/JSON parsing with
// boolean flags indicating which fields were explicitly set.
func TestNormalizeAgentID(t *testing.T) {
	tests := []struct {
		name                string
		gateway             *GatewayConfig
		agentIDDefined      bool
		legacyAPIKeyDefined bool
		source              string
		wantAgentID         string
		wantAPIKey          string
		wantExplicit        bool
	}{
		{
			name:                "nil receiver is a no-op",
			gateway:             nil,
			agentIDDefined:      false,
			legacyAPIKeyDefined: false,
			source:              "test",
			// All zero values — just verifying no panic.
		},
		{
			name:                "neither field defined leaves zero values",
			gateway:             &GatewayConfig{},
			agentIDDefined:      false,
			legacyAPIKeyDefined: false,
			source:              "test",
			wantAgentID:         "",
			wantAPIKey:          "",
			wantExplicit:        false,
		},
		{
			name: "only agent_id defined marks explicit and sets APIKey alias",
			gateway: &GatewayConfig{
				AgentID: "my-agent",
			},
			agentIDDefined:      true,
			legacyAPIKeyDefined: false,
			source:              "TOML",
			wantAgentID:         "my-agent",
			wantAPIKey:          "my-agent",
			wantExplicit:        true,
		},
		{
			name: "only api_key defined copies api_key to AgentID",
			gateway: &GatewayConfig{
				APIKey: "legacy-key",
			},
			agentIDDefined:      false,
			legacyAPIKeyDefined: true,
			source:              "TOML",
			wantAgentID:         "legacy-key",
			wantAPIKey:          "legacy-key",
			wantExplicit:        false,
		},
		{
			name: "both defined with same value uses agent_id, marks explicit",
			gateway: &GatewayConfig{
				AgentID: "shared-id",
				APIKey:  "shared-id",
			},
			agentIDDefined:      true,
			legacyAPIKeyDefined: true,
			source:              "stdin JSON",
			wantAgentID:         "shared-id",
			wantAPIKey:          "shared-id",
			wantExplicit:        true,
		},
		{
			name: "both defined with different values: agent_id wins, conflict logged",
			gateway: &GatewayConfig{
				AgentID: "new-agent-id",
				APIKey:  "old-api-key",
			},
			agentIDDefined:      true,
			legacyAPIKeyDefined: true,
			source:              "TOML",
			wantAgentID:         "new-agent-id",
			wantAPIKey:          "new-agent-id",
			wantExplicit:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// nil receiver case: just confirm no panic.
			if tt.gateway == nil {
				assert.NotPanics(t, func() {
					tt.gateway.normalizeAgentID(tt.agentIDDefined, tt.legacyAPIKeyDefined, tt.source)
				})
				return
			}

			tt.gateway.normalizeAgentID(tt.agentIDDefined, tt.legacyAPIKeyDefined, tt.source)

			assert.Equal(t, tt.wantAgentID, tt.gateway.AgentID, "AgentID mismatch")
			assert.Equal(t, tt.wantAPIKey, tt.gateway.APIKey, "APIKey mismatch")
			assert.Equal(t, tt.wantExplicit, tt.gateway.agentIDExplicit, "agentIDExplicit mismatch")
		})
	}
}

// TestLoadFromFile_BothAgentIDAndAPIKeySet_DifferentValues verifies that when
// a TOML config defines both [gateway].agent_id and [gateway].api_key with
// different values, LoadFromFile loads successfully, uses agent_id as the
// authoritative identifier, and marks it explicit.
//
// This exercises the conflict-detection branch in normalizeAgentID (the
// "using gateway.agent_id" deprecation warning) that is not reached by any
// other test, since all other tests set at most one of the two fields.
func TestLoadFromFile_BothAgentIDAndAPIKeySet_DifferentValues(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
agent_id = "preferred-agent-id"
api_key  = "old-legacy-key"

[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
`)

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.Gateway)

	// agent_id always wins over api_key when both are present.
	assert.Equal(t, "preferred-agent-id", cfg.Gateway.AgentID, "agent_id should be used as AgentID")
	assert.Equal(t, "preferred-agent-id", cfg.Gateway.APIKey, "APIKey alias should mirror AgentID")
	assert.True(t, cfg.Gateway.agentIDExplicit, "agentIDExplicit should be true when agent_id is present")
}

// TestLoadFromFile_BothAgentIDAndAPIKeySet_SameValue verifies that when a TOML
// config sets agent_id and api_key to the same value, LoadFromFile succeeds and
// the gateway uses that value as the authoritative identifier.
func TestLoadFromFile_BothAgentIDAndAPIKeySet_SameValue(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
agent_id = "shared-key"
api_key  = "shared-key"

[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
`)

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.Gateway)

	assert.Equal(t, "shared-key", cfg.Gateway.AgentID)
	assert.Equal(t, "shared-key", cfg.Gateway.APIKey)
	assert.True(t, cfg.Gateway.agentIDExplicit)
}

// TestLoadFromFile_OnlyAgentIDSet verifies that when only [gateway].agent_id
// is set, the gateway uses it as the identifier and marks it explicit.
func TestLoadFromFile_OnlyAgentIDSet(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
agent_id = "only-agent-id"

[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
`)

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.Gateway)

	assert.Equal(t, "only-agent-id", cfg.Gateway.AgentID)
	assert.Equal(t, "only-agent-id", cfg.Gateway.APIKey, "APIKey alias should mirror AgentID")
	assert.True(t, cfg.Gateway.agentIDExplicit)
}

// TestLoadFromFile_OnlyAPIKeySet verifies that when only the deprecated
// [gateway].api_key is set, its value is promoted to AgentID.
func TestLoadFromFile_OnlyAPIKeySet(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
api_key = "only-api-key"

[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
`)

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.Gateway)

	assert.Equal(t, "only-api-key", cfg.Gateway.AgentID, "api_key should be promoted to AgentID")
	assert.Equal(t, "only-api-key", cfg.Gateway.APIKey)
	assert.False(t, cfg.Gateway.agentIDExplicit, "agentIDExplicit should be false when only api_key is set")
}

// TestGetAgentID_ExplicitAgentID verifies that GetAgentID returns the
// explicitly configured agent_id even when APIKey is also set.
func TestGetAgentID_ExplicitAgentID(t *testing.T) {
	cfg := &Config{
		Gateway: &GatewayConfig{
			AgentID:         "my-explicit-agent",
			APIKey:          "my-legacy-key",
			agentIDExplicit: true,
		},
	}
	assert.Equal(t, "my-explicit-agent", cfg.GetAgentID())
}

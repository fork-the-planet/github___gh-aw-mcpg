package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTempTOML writes content to a temp file and returns its path.
func writeTempTOML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

// validDockerServerTOML is a minimal valid TOML config with a single stdio (docker) server.
const validDockerServerTOML = `
[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
`

// validHTTPServerTOML is a minimal valid TOML config with a single HTTP server.
const validHTTPServerTOML = `
[servers.myservice]
type = "http"
url = "http://localhost:9090/mcp"
`

// TestLoadFromFile_FileNotFound verifies that LoadFromFile returns an error
// when the specified file path does not exist.
func TestLoadFromFile_FileNotFound(t *testing.T) {
	cfg, err := LoadFromFile("/nonexistent/path/to/config.toml")
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "failed to open config file")
}

// TestLoadFromFile_InvalidTOML verifies that LoadFromFile returns an error
// when the TOML file contains a syntax error.
func TestLoadFromFile_InvalidTOML(t *testing.T) {
	path := writeTempTOML(t, `
[servers.github
command = "docker"
`)
	cfg, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "failed to parse TOML")
}

// TestLoadFromFile_EmptyServers verifies that LoadFromFile returns an error
// when the TOML file defines no servers.
func TestLoadFromFile_EmptyServers(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
port = 3000
api_key = "test-key"
`)
	cfg, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "no servers defined")
}

// TestLoadFromFile_StdioNonDockerCommand verifies that LoadFromFile returns an error
// when a stdio server uses a command other than "docker" (Spec §3.2.1).
func TestLoadFromFile_StdioNonDockerCommand(t *testing.T) {
	path := writeTempTOML(t, `
[servers.badserver]
command = "python"
args = ["server.py"]
`)
	cfg, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "badserver")
	assert.ErrorContains(t, err, "docker")
}

// TestLoadFromFile_StdioLocalTypeNonDockerCommand verifies that stdio servers
// declared with type = "local" are also required to use docker.
func TestLoadFromFile_StdioLocalTypeNonDockerCommand(t *testing.T) {
	path := writeTempTOML(t, `
[servers.localserver]
type = "local"
command = "node"
args = ["server.js"]
`)
	cfg, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "localserver")
}

// TestLoadFromFile_HTTPServerValid verifies that an HTTP server does not require
// the docker command (only stdio servers need containerization).
func TestLoadFromFile_HTTPServerValid(t *testing.T) {
	path := writeTempTOML(t, validHTTPServerTOML)
	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Len(t, cfg.Servers, 1)
	server, ok := cfg.Servers["myservice"]
	require.True(t, ok)
	assert.Equal(t, "http", server.Type)
	assert.Equal(t, "http://localhost:9090/mcp", server.URL)
}

// TestLoadFromFile_HTTPServerWithConnectTimeout verifies that connect_timeout
// is parsed from TOML and returned correctly via HTTPConnectTimeout().
func TestLoadFromFile_HTTPServerWithConnectTimeout(t *testing.T) {
	path := writeTempTOML(t, `
[servers.slowservice]
type = "http"
url = "http://localhost:9090/mcp"
connect_timeout = 60
`)
	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	server, ok := cfg.Servers["slowservice"]
	require.True(t, ok)
	assert.Equal(t, 60, server.ConnectTimeout)
	assert.Equal(t, 60*time.Second, server.HTTPConnectTimeout())
}

// TestLoadFromFile_AppliesGatewayDefaults verifies that when no [gateway] section
// is present, default values are applied for port, startup timeout, and tool timeout.
func TestLoadFromFile_AppliesGatewayDefaults(t *testing.T) {
	path := writeTempTOML(t, validDockerServerTOML)
	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.Gateway)
	assert.Equal(t, DefaultPort, cfg.Gateway.Port)
	assert.Equal(t, DefaultStartupTimeout, cfg.Gateway.StartupTimeout)
	assert.Equal(t, DefaultToolTimeout, cfg.Gateway.ToolTimeout)
	// Payload defaults from config_payload.go init()
	assert.Equal(t, DefaultPayloadDir, cfg.Gateway.PayloadDir)
	assert.Equal(t, DefaultPayloadSizeThreshold, cfg.Gateway.PayloadSizeThreshold)
}

// TestLoadFromFile_PreservesExplicitGatewayValues verifies that when the [gateway]
// section defines values, those values are preserved and not overwritten by defaults.
func TestLoadFromFile_PreservesExplicitGatewayValues(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
port = 8888
api_key = "my-secret"
startup_timeout = 30
tool_timeout = 60

[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
`)
	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, 8888, cfg.Gateway.Port)
	assert.Equal(t, "my-secret", cfg.Gateway.APIKey)
	assert.Equal(t, 30, cfg.Gateway.StartupTimeout)
	assert.Equal(t, 60, cfg.Gateway.ToolTimeout)
}

// TestLoadFromFile_ServerFields verifies that all ServerConfig fields are parsed correctly.
func TestLoadFromFile_ServerFields(t *testing.T) {
	path := writeTempTOML(t, `
[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "-e", "GITHUB_TOKEN", "ghcr.io/github/github-mcp-server:latest"]

[servers.github.env]
GITHUB_TOKEN = "mytoken"
`)
	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	server := cfg.Servers["github"]
	require.NotNil(t, server)
	assert.Equal(t, "docker", server.Command)
	assert.Contains(t, server.Args, "run")
	assert.Equal(t, "mytoken", server.Env["GITHUB_TOKEN"])
}

func TestLoadFromFile_ToolResponseFilters(t *testing.T) {
	path := writeTempTOML(t, `
[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]

[servers.github.tool_response_filters]
list_code_scanning_alerts = "map(del(.rule.help))"
`)

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Contains(t, cfg.Servers, "github")
	assert.Equal(t, map[string]string{
		"list_code_scanning_alerts": "map(del(.rule.help))",
	}, cfg.Servers["github"].ToolResponseFilters)
}

// TestLoadFromFile_UnknownKeysDoNotCauseError verifies that unknown configuration
// keys are rejected with an error per spec §4.3.1.
func TestLoadFromFile_UnknownKeysDoNotCauseError(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
prot = 3000

[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
`)
	// Unknown key "prot" (typo for "port") must now return an error per spec §4.3.1
	cfg, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "unrecognized field")
}

// TestLoadFromFile_TrustedBotsEmptyArray verifies that an explicitly set but
// empty trusted_bots array is rejected (spec §4.1.3.4).
func TestLoadFromFile_TrustedBotsEmptyArray(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
trusted_bots = []

[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
`)
	cfg, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "trusted_bots")
}

// TestLoadFromFile_TrustedBotsEmptyString verifies that a trusted_bots entry
// that is an empty or whitespace-only string is rejected.
func TestLoadFromFile_TrustedBotsEmptyString(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
trusted_bots = ["   "]

[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
`)
	cfg, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "trusted_bots")
}

// TestLoadFromFile_TrustedBotsValid verifies that a non-empty trusted_bots list
// with valid entries loads successfully.
func TestLoadFromFile_TrustedBotsValid(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
trusted_bots = ["my-bot[bot]", "another-bot[bot]"]

[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
`)
	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, []string{"my-bot[bot]", "another-bot[bot]"}, cfg.Gateway.TrustedBots)
}

// TestLoadFromFile_MixedStdioAndHTTPServers verifies that multiple servers of different types
// are parsed correctly.
func TestLoadFromFile_MixedStdioAndHTTPServers(t *testing.T) {
	path := writeTempTOML(t, `
[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]

[servers.myapi]
type = "http"
url = "https://api.example.com/mcp"
`)
	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Len(t, cfg.Servers, 2)
	assert.NotNil(t, cfg.Servers["github"])
	assert.NotNil(t, cfg.Servers["myapi"])
}

// TestLoadFromFile_StdioExplicitTypeDocker verifies that a server with
// type = "stdio" and command = "docker" is accepted.
func TestLoadFromFile_StdioExplicitTypeDocker(t *testing.T) {
	path := writeTempTOML(t, `
[servers.myserver]
type = "stdio"
command = "docker"
args = ["run", "--rm", "-i", "my/image:latest"]
`)
	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	server := cfg.Servers["myserver"]
	require.NotNil(t, server)
	assert.Equal(t, "stdio", server.Type)
	assert.Equal(t, "docker", server.Command)
}

// TestApplyGatewayDefaults_AllZero verifies that all defaults are applied when
// the GatewayConfig has all zero values.
func TestApplyGatewayDefaults_AllZero(t *testing.T) {
	cfg := &GatewayConfig{}
	applyGatewayDefaults(cfg)
	assert.Equal(t, DefaultPort, cfg.Port)
	assert.Equal(t, DefaultStartupTimeout, cfg.StartupTimeout)
	assert.Equal(t, DefaultToolTimeout, cfg.ToolTimeout)
}

// TestApplyGatewayDefaults_PreservesExistingValues verifies that already-set values
// are not overwritten by applyGatewayDefaults.
func TestApplyGatewayDefaults_PreservesExistingValues(t *testing.T) {
	cfg := &GatewayConfig{
		Port:           9000,
		StartupTimeout: 120,
		ToolTimeout:    240,
	}
	applyGatewayDefaults(cfg)
	assert.Equal(t, 9000, cfg.Port)
	assert.Equal(t, 120, cfg.StartupTimeout)
	assert.Equal(t, 240, cfg.ToolTimeout)
}

// TestApplyGatewayDefaults_PartialZero verifies that only zero-valued fields
// receive defaults while non-zero fields are preserved.
func TestApplyGatewayDefaults_PartialZero(t *testing.T) {
	cfg := &GatewayConfig{
		Port:           5000,
		StartupTimeout: 0, // should get default
		ToolTimeout:    0, // should get default
	}
	applyGatewayDefaults(cfg)
	assert.Equal(t, 5000, cfg.Port)
	assert.Equal(t, DefaultStartupTimeout, cfg.StartupTimeout)
	assert.Equal(t, DefaultToolTimeout, cfg.ToolTimeout)
}

// TestGetAPIKey_NilGateway verifies that GetAPIKey returns an empty string
// when the Config has a nil Gateway field.
func TestGetAPIKey_NilGateway(t *testing.T) {
	cfg := &Config{Gateway: nil}
	assert.Equal(t, "", cfg.GetAPIKey())
}

// TestGetAPIKey_EmptyKey verifies that GetAPIKey returns an empty string
// when the Gateway has an empty APIKey.
func TestGetAPIKey_EmptyKey(t *testing.T) {
	cfg := &Config{Gateway: &GatewayConfig{APIKey: ""}}
	assert.Equal(t, "", cfg.GetAPIKey())
}

// TestGetAPIKey_ReturnsKey verifies that GetAPIKey returns the configured API key.
func TestGetAPIKey_ReturnsKey(t *testing.T) {
	cfg := &Config{Gateway: &GatewayConfig{APIKey: "super-secret-key"}}
	assert.Equal(t, "super-secret-key", cfg.GetAPIKey())
}

// TestLoadFromFile_OIDCAuthMissingEnvVar verifies that LoadFromFile returns an error
// when a server uses github-oidc auth but ACTIONS_ID_TOKEN_REQUEST_URL is not set.
// This ensures parity with the JSON stdin config path (Spec §9 Fail-Fast Startup).
func TestLoadFromFile_OIDCAuthMissingEnvVar(t *testing.T) {
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")

	path := writeTempTOML(t, `
[servers.secure]
type = "http"
url = "https://example.com/mcp"

[servers.secure.auth]
type = "github-oidc"
audience = "https://example.com"
`)
	cfg, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "ACTIONS_ID_TOKEN_REQUEST_URL")
}

// TestLoadFromFile_OIDCAuthWithEnvVarSet verifies that LoadFromFile succeeds
// when a server uses github-oidc auth and ACTIONS_ID_TOKEN_REQUEST_URL is set.
func TestLoadFromFile_OIDCAuthWithEnvVarSet(t *testing.T) {
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://token.actions.example.com")

	path := writeTempTOML(t, `
[servers.secure]
type = "http"
url = "https://example.com/mcp"

[servers.secure.auth]
type = "github-oidc"
audience = "https://example.com"
`)
	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	server := cfg.Servers["secure"]
	require.NotNil(t, server)
	require.NotNil(t, server.Auth)
	assert.Equal(t, "github-oidc", server.Auth.Type)
	assert.Equal(t, "https://example.com", server.Auth.Audience)
}

// TestLoadFromFile_AuthOnNonHTTPServerRejected verifies that TOML configs reject
// auth blocks on non-HTTP servers so TOML validation stays aligned with stdin rules.
func TestLoadFromFile_AuthOnNonHTTPServerRejected(t *testing.T) {
	path := writeTempTOML(t, `
[servers.local]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]

[servers.local.auth]
type = "github-oidc"
audience = "https://example.com"
`)
	cfg, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "auth")
	assert.ErrorContains(t, err, "Remove the auth configuration or change the server type to \"http\"")
}

// TestLoadFromFile_NegativePayloadSizeThresholdRejected verifies that TOML configs with
// a negative payload_size_threshold are rejected per spec §4.1.3.3.
func TestLoadFromFile_NegativePayloadSizeThresholdRejected(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
payload_size_threshold = -1

[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
`)
	cfg, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "payload_size_threshold must be a positive integer")
}

// TestHTTPKeepaliveInterval tests all branches of the HTTPKeepaliveInterval method.
func TestHTTPKeepaliveInterval(t *testing.T) {
	tests := []struct {
		name     string
		gateway  *GatewayConfig
		expected time.Duration
	}{
		{
			name:     "nil receiver returns default keepalive interval",
			gateway:  nil,
			expected: time.Duration(DefaultKeepaliveInterval) * time.Second,
		},
		{
			name:     "negative value disables keepalive (returns 0)",
			gateway:  &GatewayConfig{KeepaliveInterval: -1},
			expected: 0,
		},
		{
			name:     "highly negative value also disables keepalive",
			gateway:  &GatewayConfig{KeepaliveInterval: -999},
			expected: 0,
		},
		{
			name:     "zero value returns zero duration",
			gateway:  &GatewayConfig{KeepaliveInterval: 0},
			expected: 0,
		},
		{
			name:     "positive value returns correct duration in seconds",
			gateway:  &GatewayConfig{KeepaliveInterval: 300},
			expected: 300 * time.Second,
		},
		{
			name:     "default keepalive interval value returns 25 minutes",
			gateway:  &GatewayConfig{KeepaliveInterval: DefaultKeepaliveInterval},
			expected: time.Duration(DefaultKeepaliveInterval) * time.Second,
		},
		{
			name:     "one second interval",
			gateway:  &GatewayConfig{KeepaliveInterval: 1},
			expected: 1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gateway.HTTPKeepaliveInterval()
			assert.Equal(t, tt.expected, got)
		})
	}
}

// TestHTTPConnectTimeout tests all branches of the ServerConfig.HTTPConnectTimeout method.
func TestHTTPConnectTimeout(t *testing.T) {
	tests := []struct {
		name     string
		server   *ServerConfig
		expected time.Duration
	}{
		{
			name:     "nil receiver returns default",
			server:   nil,
			expected: time.Duration(DefaultConnectTimeout) * time.Second,
		},
		{
			name:     "zero value returns default",
			server:   &ServerConfig{},
			expected: time.Duration(DefaultConnectTimeout) * time.Second,
		},
		{
			name:     "negative value returns default",
			server:   &ServerConfig{ConnectTimeout: -5},
			expected: time.Duration(DefaultConnectTimeout) * time.Second,
		},
		{
			name:     "positive value returns correct duration",
			server:   &ServerConfig{ConnectTimeout: 60},
			expected: 60 * time.Second,
		},
		{
			name:     "default value returns 30 seconds",
			server:   &ServerConfig{ConnectTimeout: DefaultConnectTimeout},
			expected: time.Duration(DefaultConnectTimeout) * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.server.HTTPConnectTimeout()
			assert.Equal(t, tt.expected, got)
		})
	}
}

// TestIsDynamicTOMLPath verifies the branching logic of isDynamicTOMLPath,
// which guards the unknown-field check by exempting map-valued sections
// whose keys are not known at decode time.
func TestIsDynamicTOMLPath(t *testing.T) {
	tests := []struct {
		name     string
		key      toml.Key
		expected bool
	}{
		// ── servers.<name>.guard_policies.<policy>.<key>  (len ≥ 5) ─────────
		{
			name:     "servers guard_policies at minimum length 5",
			key:      toml.Key{"servers", "github", "guard_policies", "mypolicy", "repos"},
			expected: true,
		},
		{
			name:     "servers guard_policies longer than minimum",
			key:      toml.Key{"servers", "github", "guard_policies", "mypolicy", "nested", "key"},
			expected: true,
		},
		{
			name:     "servers guard_policies different server name still true",
			key:      toml.Key{"servers", "slack", "guard_policies", "p1", "field"},
			expected: true,
		},
		{
			name:     "servers guard_policies exactly 4 elements is too short",
			key:      toml.Key{"servers", "github", "guard_policies", "mypolicy"},
			expected: false,
		},
		{
			name:     "servers guard_policies 3 elements is too short",
			key:      toml.Key{"servers", "github", "guard_policies"},
			expected: false,
		},
		{
			name:     "servers with wrong key[2] (not guard_policies)",
			key:      toml.Key{"servers", "github", "command", "whatever", "extra"},
			expected: false,
		},
		{
			name:     "wrong key[0] for servers path (guards instead)",
			key:      toml.Key{"guards", "github", "guard_policies", "mypolicy", "repos"},
			expected: false,
		},
		{
			name:     "gateway prefix with guard_policies shape",
			key:      toml.Key{"gateway", "x", "guard_policies", "p", "k"},
			expected: false,
		},

		// ── guards.<name>.config.<key>  (len ≥ 4) ───────────────────────────
		{
			name:     "guards config at minimum length 4",
			key:      toml.Key{"guards", "myfence", "config", "somekey"},
			expected: true,
		},
		{
			name:     "guards config longer than minimum",
			key:      toml.Key{"guards", "myfence", "config", "somekey", "nested"},
			expected: true,
		},
		{
			name:     "guards config different guard name still true",
			key:      toml.Key{"guards", "allowonly", "config", "level"},
			expected: true,
		},
		{
			name:     "guards config exactly 3 elements is too short",
			key:      toml.Key{"guards", "myfence", "config"},
			expected: false,
		},
		{
			name:     "guards config 2 elements is too short",
			key:      toml.Key{"guards", "myfence"},
			expected: false,
		},
		{
			name:     "guards with wrong key[2] (not config)",
			key:      toml.Key{"guards", "myfence", "command", "somekey"},
			expected: false,
		},
		{
			name:     "wrong key[0] for guards path (servers instead)",
			key:      toml.Key{"servers", "myfence", "config", "somekey"},
			expected: false,
		},

		// ── Non-dynamic / ordinary TOML paths ────────────────────────────────
		{
			name:     "nil key",
			key:      nil,
			expected: false,
		},
		{
			name:     "empty key",
			key:      toml.Key{},
			expected: false,
		},
		{
			name:     "single element key",
			key:      toml.Key{"servers"},
			expected: false,
		},
		{
			name:     "gateway port path",
			key:      toml.Key{"gateway", "port"},
			expected: false,
		},
		{
			name:     "servers command path",
			key:      toml.Key{"servers", "github", "command"},
			expected: false,
		},
		{
			name:     "servers args path",
			key:      toml.Key{"servers", "github", "args"},
			expected: false,
		},
		{
			name:     "servers env path",
			key:      toml.Key{"servers", "github", "env", "TOKEN"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDynamicTOMLPath(tt.key)
			assert.Equal(t, tt.expected, got, "isDynamicTOMLPath(%v)", tt.key)
		})
	}
}

// TestEnsureGatewayDefaults tests the EnsureGatewayDefaults method.
func TestEnsureGatewayDefaults(t *testing.T) {
	t.Run("nil gateway gets created with defaults", func(t *testing.T) {
		cfg := &Config{
			Gateway: nil,
			Servers: map[string]*ServerConfig{
				"test": {Command: "docker"},
			},
		}
		cfg.EnsureGatewayDefaults()

		require.NotNil(t, cfg.Gateway, "Gateway should be non-nil after EnsureGatewayDefaults")
		assert.Equal(t, DefaultPort, cfg.Gateway.Port, "Default port should be applied")
		assert.Equal(t, DefaultStartupTimeout, cfg.Gateway.StartupTimeout, "Default startup timeout should be applied")
		assert.Equal(t, DefaultToolTimeout, cfg.Gateway.ToolTimeout, "Default tool timeout should be applied")
		assert.Equal(t, DefaultKeepaliveInterval, cfg.Gateway.KeepaliveInterval, "Default keepalive interval should be applied")
	})

	t.Run("non-nil gateway with zero values gets defaults applied", func(t *testing.T) {
		cfg := &Config{
			Gateway: &GatewayConfig{}, // all zero values
			Servers: map[string]*ServerConfig{
				"test": {Command: "docker"},
			},
		}
		cfg.EnsureGatewayDefaults()

		assert.Equal(t, DefaultPort, cfg.Gateway.Port, "Default port should be applied to zero value")
		assert.Equal(t, DefaultStartupTimeout, cfg.Gateway.StartupTimeout, "Default startup timeout should be applied to zero value")
		assert.Equal(t, DefaultToolTimeout, cfg.Gateway.ToolTimeout, "Default tool timeout should be applied to zero value")
		assert.Equal(t, DefaultKeepaliveInterval, cfg.Gateway.KeepaliveInterval, "Default keepalive interval should be applied to zero value")
	})

	t.Run("non-nil gateway with explicit values preserves them", func(t *testing.T) {
		cfg := &Config{
			Gateway: &GatewayConfig{
				Port:              9999,
				StartupTimeout:    45,
				ToolTimeout:       90,
				KeepaliveInterval: 600,
				APIKey:            "my-api-key",
			},
		}
		cfg.EnsureGatewayDefaults()

		assert.Equal(t, 9999, cfg.Gateway.Port, "Explicit port should be preserved")
		assert.Equal(t, 45, cfg.Gateway.StartupTimeout, "Explicit startup timeout should be preserved")
		assert.Equal(t, 90, cfg.Gateway.ToolTimeout, "Explicit tool timeout should be preserved")
		assert.Equal(t, 600, cfg.Gateway.KeepaliveInterval, "Explicit keepalive interval should be preserved")
		assert.Equal(t, "my-api-key", cfg.Gateway.APIKey, "Explicit API key should be preserved")
	})

	t.Run("calling EnsureGatewayDefaults twice is idempotent", func(t *testing.T) {
		cfg := &Config{Gateway: nil}
		cfg.EnsureGatewayDefaults()
		firstPort := cfg.Gateway.Port

		cfg.EnsureGatewayDefaults()
		assert.Equal(t, firstPort, cfg.Gateway.Port, "Second call should not change already-defaulted values")
	})
}

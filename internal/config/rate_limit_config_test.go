package config

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerConfig_RateLimitFields(t *testing.T) {
	t.Parallel()
	toml := `
[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]
rate_limit_threshold = 5
rate_limit_cooldown = 120
`
	path := writeTempTOML(t, toml)
	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	srv := cfg.Servers["github"]
	assert.Equal(t, 5, srv.RateLimitThreshold)
	assert.Equal(t, 120, srv.RateLimitCooldown)
}

func TestServerConfig_RateLimitFieldsDefaultToZero(t *testing.T) {
	t.Parallel()
	toml := validDockerServerTOML
	path := writeTempTOML(t, toml)
	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	srv := cfg.Servers["github"]
	assert.Equal(t, 0, srv.RateLimitThreshold)
	assert.Equal(t, 0, srv.RateLimitCooldown)
}

func TestServerConfig_RateLimitFields_AreExcludedFromJSONTags(t *testing.T) {
	t.Parallel()

	serverType := reflect.TypeOf(ServerConfig{})
	thresholdField, ok := serverType.FieldByName("RateLimitThreshold")
	require.True(t, ok)
	assert.Equal(t, "-", thresholdField.Tag.Get("json"))

	cooldownField, ok := serverType.FieldByName("RateLimitCooldown")
	require.True(t, ok)
	assert.Equal(t, "-", cooldownField.Tag.Get("json"))
}

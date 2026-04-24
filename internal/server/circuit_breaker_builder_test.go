package server

import (
	"testing"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildCircuitBreakers verifies that buildCircuitBreakers creates per-server
// circuit breakers with the configuration values provided in the config.
func TestBuildCircuitBreakers(t *testing.T) {
	t.Parallel()

	t.Run("nil config returns empty map", func(t *testing.T) {
		t.Parallel()
		cbs := buildCircuitBreakers(nil)
		assert.Empty(t, cbs, "nil config should produce an empty circuit breaker map")
	})

	t.Run("empty servers returns empty map", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{},
		}
		cbs := buildCircuitBreakers(cfg)
		assert.Empty(t, cbs, "empty servers map should produce no circuit breakers")
	})

	t.Run("single server uses configured threshold and cooldown", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{
				"github": {
					RateLimitThreshold: 5,
					RateLimitCooldown:  120,
				},
			},
		}
		cbs := buildCircuitBreakers(cfg)
		require.Len(t, cbs, 1)

		cb, ok := cbs["github"]
		require.True(t, ok, "circuit breaker for 'github' should exist")
		assert.Equal(t, 5, cb.threshold, "threshold should match server config")
		assert.Equal(t, 120*time.Second, cb.cooldown, "cooldown should match server config")
	})

	t.Run("multiple servers each get their own circuit breaker", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{
				"github": {
					RateLimitThreshold: 3,
					RateLimitCooldown:  60,
				},
				"slack": {
					RateLimitThreshold: 10,
					RateLimitCooldown:  30,
				},
			},
		}
		cbs := buildCircuitBreakers(cfg)
		require.Len(t, cbs, 2)

		ghCB, ok := cbs["github"]
		require.True(t, ok)
		assert.Equal(t, 3, ghCB.threshold)
		assert.Equal(t, 60*time.Second, ghCB.cooldown)

		slackCB, ok := cbs["slack"]
		require.True(t, ok)
		assert.Equal(t, 10, slackCB.threshold)
		assert.Equal(t, 30*time.Second, slackCB.cooldown)
	})

	t.Run("zero threshold falls back to default", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{
				"github": {
					RateLimitThreshold: 0,
					RateLimitCooldown:  0,
				},
			},
		}
		cbs := buildCircuitBreakers(cfg)
		require.Len(t, cbs, 1)

		cb, ok := cbs["github"]
		require.True(t, ok, "circuit breaker for 'github' should exist")
		// newCircuitBreaker replaces zero values with defaults.
		assert.Equal(t, DefaultRateLimitThreshold, cb.threshold, "zero threshold should use default")
		assert.Equal(t, DefaultRateLimitCooldown, cb.cooldown, "zero cooldown should use default")
	})

	t.Run("each circuit breaker starts in CLOSED state", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{
				"github": {RateLimitThreshold: 5, RateLimitCooldown: 60},
				"slack":  {RateLimitThreshold: 5, RateLimitCooldown: 60},
			},
		}
		cbs := buildCircuitBreakers(cfg)
		require.Len(t, cbs, 2, "expected circuit breakers for both 'github' and 'slack'")
		for serverID, cb := range cbs {
			assert.Equal(t, circuitClosed, cb.State(), "circuit breaker for %s should start CLOSED", serverID)
			assert.NoError(t, cb.Allow(), "CLOSED circuit breaker for %s should allow requests", serverID)
		}
	})

	t.Run("circuit breakers are independent per server", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{
				"github": {RateLimitThreshold: 1, RateLimitCooldown: 60},
				"slack":  {RateLimitThreshold: 3, RateLimitCooldown: 60},
			},
		}
		cbs := buildCircuitBreakers(cfg)
		require.Len(t, cbs, 2, "expected circuit breakers for both 'github' and 'slack'")
		require.Contains(t, cbs, "github", "circuit breaker for 'github' should exist")
		require.Contains(t, cbs, "slack", "circuit breaker for 'slack' should exist")

		// Open the github circuit breaker by hitting the threshold.
		cbs["github"].RecordRateLimit(time.Time{})
		assert.Equal(t, circuitOpen, cbs["github"].State(), "github CB should be open after 1 error (threshold=1)")

		// slack's circuit breaker should still be closed (threshold=3, no errors recorded).
		assert.Equal(t, circuitClosed, cbs["slack"].State(), "slack CB should remain CLOSED")
		assert.NoError(t, cbs["slack"].Allow(), "slack CB should still allow requests")
	})
}

// TestGetCircuitBreaker verifies the lazy-initialisation behaviour of getCircuitBreaker.
func TestGetCircuitBreaker(t *testing.T) {
	t.Parallel()

	t.Run("returns existing circuit breaker when present", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Servers: map[string]*config.ServerConfig{
				"github": {RateLimitThreshold: 5, RateLimitCooldown: 30},
			},
		}
		us := &UnifiedServer{
			circuitBreakers: buildCircuitBreakers(cfg),
		}

		cb := us.getCircuitBreaker("github")
		require.NotNil(t, cb)
		assert.Equal(t, 5, cb.threshold, "should return the pre-configured circuit breaker")
		assert.Equal(t, 30*time.Second, cb.cooldown)
	})

	t.Run("creates default circuit breaker for unknown server", func(t *testing.T) {
		t.Parallel()
		us := &UnifiedServer{
			circuitBreakers: map[string]*circuitBreaker{},
		}

		cb := us.getCircuitBreaker("unknown")
		require.NotNil(t, cb)
		assert.Equal(t, DefaultRateLimitThreshold, cb.threshold, "should use default threshold")
		assert.Equal(t, DefaultRateLimitCooldown, cb.cooldown, "should use default cooldown")
	})

	t.Run("nil circuitBreakers map is lazily initialised", func(t *testing.T) {
		t.Parallel()
		// Simulate a server created without the map (e.g. in unit tests that bypass NewUnified).
		us := &UnifiedServer{}
		assert.Nil(t, us.circuitBreakers, "precondition: circuitBreakers is nil before first call")

		cb := us.getCircuitBreaker("github")
		require.NotNil(t, cb)
		assert.NotNil(t, us.circuitBreakers, "circuitBreakers map should be initialised after first call")
		assert.Equal(t, DefaultRateLimitThreshold, cb.threshold)
	})

	t.Run("cached: second call returns same instance", func(t *testing.T) {
		t.Parallel()
		us := &UnifiedServer{}

		cb1 := us.getCircuitBreaker("github")
		cb2 := us.getCircuitBreaker("github")
		assert.Same(t, cb1, cb2, "repeated calls should return the same circuit breaker instance")
	})

	t.Run("different servers return different instances", func(t *testing.T) {
		t.Parallel()
		us := &UnifiedServer{}

		cbGitHub := us.getCircuitBreaker("github")
		cbSlack := us.getCircuitBreaker("slack")
		assert.NotSame(t, cbGitHub, cbSlack, "different server IDs should return different instances")
	})

	t.Run("newly created default CB starts CLOSED", func(t *testing.T) {
		t.Parallel()
		us := &UnifiedServer{}
		cb := us.getCircuitBreaker("new-server")
		require.NotNil(t, cb)
		assert.Equal(t, circuitClosed, cb.State(), "lazily created CB should start CLOSED")
		assert.NoError(t, cb.Allow())
	})
}

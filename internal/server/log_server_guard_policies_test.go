package server

import (
	"bytes"
	"log"
	"math"
	"strings"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/stretchr/testify/assert"
)

// captureStdLog redirects the standard logger to a buffer for the duration of fn
// and returns the captured output. Restores the original logger output afterwards.
func captureStdLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(oldOutput)
	})
	fn()
	return buf.String()
}

// newServerForLogTest creates the minimal UnifiedServer needed to exercise
// logServerGuardPolicies (only cfg and guardRegistry are required).
func newServerForLogTest(cfg *config.Config) *UnifiedServer {
	return &UnifiedServer{
		cfg:           cfg,
		guardRegistry: guard.NewRegistry(),
	}
}

func TestLogServerGuardPolicies_NilConfig(t *testing.T) {
	us := newServerForLogTest(nil)

	output := captureStdLog(t, func() {
		us.logServerGuardPolicies("github")
	})

	assert.Contains(t, output, "[DIFC]")
	assert.Contains(t, output, "no guard policy was set")
	assert.Contains(t, output, "github")
}

func TestLogServerGuardPolicies_NilServersMap(t *testing.T) {
	us := newServerForLogTest(&config.Config{Servers: nil})

	output := captureStdLog(t, func() {
		us.logServerGuardPolicies("github")
	})

	assert.Contains(t, output, "no guard policy was set")
	assert.Contains(t, output, "github")
}

func TestLogServerGuardPolicies_ServerNotFound(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"other": {Type: "http", URL: "https://example.com/mcp"},
		},
	}
	us := newServerForLogTest(cfg)

	output := captureStdLog(t, func() {
		us.logServerGuardPolicies("github")
	})

	assert.Contains(t, output, "no guard policy was set")
	assert.Contains(t, output, "github")
}

func TestLogServerGuardPolicies_NilServerConfig(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": nil,
		},
	}
	us := newServerForLogTest(cfg)

	output := captureStdLog(t, func() {
		us.logServerGuardPolicies("github")
	})

	assert.Contains(t, output, "no guard policy was set")
	assert.Contains(t, output, "github")
}

func TestLogServerGuardPolicies_EmptyGuardPolicies(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type:          "http",
				URL:           "https://example.com/mcp",
				GuardPolicies: map[string]interface{}{},
			},
		},
	}
	us := newServerForLogTest(cfg)

	output := captureStdLog(t, func() {
		us.logServerGuardPolicies("github")
	})

	assert.Contains(t, output, "no guard policy was set")
	assert.Contains(t, output, "github")
}

func TestLogServerGuardPolicies_ValidPolicies(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type: "http",
				URL:  "https://example.com/mcp",
				GuardPolicies: map[string]interface{}{
					"allow-only": map[string]interface{}{
						"repos":         []interface{}{"github/gh-aw*"},
						"min-integrity": "approved",
					},
				},
			},
		},
	}
	us := newServerForLogTest(cfg)

	output := captureStdLog(t, func() {
		us.logServerGuardPolicies("github")
	})

	assert.Contains(t, output, "[DIFC]")
	assert.Contains(t, output, "guard policy for MCP server 'github'")
	assert.Contains(t, output, "allow-only")
	assert.NotContains(t, output, "no guard policy was set")
}

func TestLogServerGuardPolicies_MultiplePolicies(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type: "http",
				URL:  "https://example.com/mcp",
				GuardPolicies: map[string]interface{}{
					"allow-only": map[string]interface{}{
						"repos":         "public",
						"min-integrity": "none",
					},
				},
			},
			"slack": {
				Type: "http",
				URL:  "https://slack.example.com/mcp",
				GuardPolicies: map[string]interface{}{
					"write-sink": map[string]interface{}{
						"accept": []interface{}{"*"},
					},
				},
			},
		},
	}
	us := newServerForLogTest(cfg)

	githubOutput := captureStdLog(t, func() {
		us.logServerGuardPolicies("github")
	})
	slackOutput := captureStdLog(t, func() {
		us.logServerGuardPolicies("slack")
	})

	assert.Contains(t, githubOutput, "guard policy for MCP server 'github'")
	assert.Contains(t, slackOutput, "guard policy for MCP server 'slack'")
	assert.Contains(t, slackOutput, "write-sink")
}

func TestLogServerGuardPolicies_UnmarshalablePolicy(t *testing.T) {
	// math.NaN() cannot be marshaled to JSON, triggering the error path
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type: "http",
				URL:  "https://example.com/mcp",
				GuardPolicies: map[string]interface{}{
					"allow-only": math.NaN(),
				},
			},
		},
	}
	us := newServerForLogTest(cfg)

	output := captureStdLog(t, func() {
		us.logServerGuardPolicies("github")
	})

	assert.Contains(t, output, "[DIFC]")
	assert.Contains(t, output, "github")
	// Function logs either the error path or succeeds — either way it must not panic
	// and must produce a [DIFC] log line mentioning the server ID.
	assert.True(t,
		strings.Contains(output, "failed to serialize policy") ||
			strings.Contains(output, "guard policy"),
		"expected a DIFC log line about guard policy, got: %q", output)
}

func TestLogServerGuardPolicies_TableDriven(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *config.Config
		serverID     string
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "nil config",
			cfg:          nil,
			serverID:     "myserver",
			wantContains: []string{"[DIFC]", "no guard policy was set", "myserver"},
		},
		{
			name:         "nil servers",
			cfg:          &config.Config{Servers: nil},
			serverID:     "myserver",
			wantContains: []string{"[DIFC]", "no guard policy was set", "myserver"},
		},
		{
			name: "server absent from map",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"other": {Type: "http", URL: "https://other.example.com/mcp"},
				},
			},
			serverID:     "myserver",
			wantContains: []string{"no guard policy was set", "myserver"},
		},
		{
			name: "server config is nil",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{"myserver": nil},
			},
			serverID:     "myserver",
			wantContains: []string{"no guard policy was set", "myserver"},
		},
		{
			name: "empty guard policies map",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"myserver": {
						Type:          "http",
						URL:           "https://example.com/mcp",
						GuardPolicies: map[string]interface{}{},
					},
				},
			},
			serverID:     "myserver",
			wantContains: []string{"no guard policy was set", "myserver"},
			wantAbsent:   []string{"guard policy for MCP server"},
		},
		{
			name: "valid guard policies",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"myserver": {
						Type: "http",
						URL:  "https://example.com/mcp",
						GuardPolicies: map[string]interface{}{
							"allow-only": map[string]interface{}{
								"repos":         "public",
								"min-integrity": "none",
							},
						},
					},
				},
			},
			serverID:     "myserver",
			wantContains: []string{"[DIFC]", "guard policy for MCP server 'myserver'", "allow-only"},
			wantAbsent:   []string{"no guard policy was set"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			us := newServerForLogTest(tt.cfg)

			output := captureStdLog(t, func() {
				us.logServerGuardPolicies(tt.serverID)
			})

			for _, want := range tt.wantContains {
				assert.Contains(t, output, want, "log output should contain %q", want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, output, absent, "log output should not contain %q", absent)
			}
		})
	}
}

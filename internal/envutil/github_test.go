package envutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLookupGitHubToken(t *testing.T) {
	tokenVars := []string{
		"GITHUB_MCP_SERVER_TOKEN",
		"GITHUB_TOKEN",
		"GITHUB_PERSONAL_ACCESS_TOKEN",
		"GH_TOKEN",
	}
	clearAll := func(t *testing.T) {
		t.Helper()
		for _, k := range tokenVars {
			t.Setenv(k, "")
		}
	}

	t.Run("prefers GITHUB_MCP_SERVER_TOKEN", func(t *testing.T) {
		clearAll(t)
		t.Setenv("GITHUB_MCP_SERVER_TOKEN", "mcp-token")
		t.Setenv("GITHUB_TOKEN", "gh-token")
		t.Setenv("GH_TOKEN", "gh-cli-token")
		assert.Equal(t, "mcp-token", LookupGitHubToken())
	})

	t.Run("falls back to GITHUB_TOKEN", func(t *testing.T) {
		clearAll(t)
		t.Setenv("GITHUB_TOKEN", "gh-token")
		t.Setenv("GH_TOKEN", "gh-cli-token")
		assert.Equal(t, "gh-token", LookupGitHubToken())
	})

	t.Run("falls back to GITHUB_PERSONAL_ACCESS_TOKEN", func(t *testing.T) {
		clearAll(t)
		t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "pat-token")
		t.Setenv("GH_TOKEN", "gh-cli-token")
		assert.Equal(t, "pat-token", LookupGitHubToken())
	})

	t.Run("falls back to GH_TOKEN", func(t *testing.T) {
		clearAll(t)
		t.Setenv("GH_TOKEN", "gh-cli-token")
		assert.Equal(t, "gh-cli-token", LookupGitHubToken())
	})

	t.Run("returns empty when no token set", func(t *testing.T) {
		clearAll(t)
		assert.Equal(t, "", LookupGitHubToken())
	})

	t.Run("trims whitespace", func(t *testing.T) {
		clearAll(t)
		t.Setenv("GITHUB_TOKEN", "  trimmed  ")
		assert.Equal(t, "trimmed", LookupGitHubToken())
	})

	t.Run("skips whitespace-only values", func(t *testing.T) {
		clearAll(t)
		t.Setenv("GITHUB_MCP_SERVER_TOKEN", "   ")
		t.Setenv("GITHUB_TOKEN", "actual-token")
		assert.Equal(t, "actual-token", LookupGitHubToken())
	})
}

func TestLookupGitHubAPIURL(t *testing.T) {
	const defaultURL = DefaultGitHubAPIBaseURL

	t.Run("returns default when env not set", func(t *testing.T) {
		t.Setenv("GITHUB_API_URL", "")
		assert.Equal(t, defaultURL, LookupGitHubAPIURL(defaultURL))
	})

	t.Run("returns custom URL from env", func(t *testing.T) {
		t.Setenv("GITHUB_API_URL", "https://github.example.com/api/v3")
		assert.Equal(t, "https://github.example.com/api/v3", LookupGitHubAPIURL(defaultURL))
	})

	t.Run("strips trailing slash", func(t *testing.T) {
		t.Setenv("GITHUB_API_URL", "https://github.example.com/api/v3/")
		assert.Equal(t, "https://github.example.com/api/v3", LookupGitHubAPIURL(defaultURL))
	})

	t.Run("returns default for whitespace-only env", func(t *testing.T) {
		t.Setenv("GITHUB_API_URL", "   ")
		assert.Equal(t, defaultURL, LookupGitHubAPIURL(defaultURL))
	})

	t.Run("trims whitespace before stripping trailing slash", func(t *testing.T) {
		t.Setenv("GITHUB_API_URL", " https://github.example.com/api/v3/ ")
		assert.Equal(t, "https://github.example.com/api/v3", LookupGitHubAPIURL(defaultURL))
	})
}

func TestDeriveAPIFromServerURL(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		expected  string
	}{
		{
			name:      "github.com returns default",
			serverURL: "https://github.com",
			expected:  DefaultGitHubAPIBaseURL,
		},
		{
			name:      "www.github.com returns default",
			serverURL: "https://www.github.com",
			expected:  DefaultGitHubAPIBaseURL,
		},
		{
			name:      "GHEC tenant derives copilot-api subdomain",
			serverURL: "https://mycompany.ghe.com",
			expected:  "https://copilot-api.mycompany.ghe.com",
		},
		{
			name:      "GHES uses /api/v3 path",
			serverURL: "https://github.mycompany.com",
			expected:  "https://github.mycompany.com/api/v3",
		},
		{
			name:      "GHEC tenant with port",
			serverURL: "https://mycompany.ghe.com:8443",
			expected:  "https://copilot-api.mycompany.ghe.com:8443",
		},
		{
			name:      "GHES with port",
			serverURL: "https://github.example.com:8443",
			expected:  "https://github.example.com:8443/api/v3",
		},
		{
			name:      "invalid URL",
			serverURL: "not-a-url",
			expected:  "",
		},
		{
			name:      "missing scheme is rejected",
			serverURL: "//github.example.com",
			expected:  "",
		},
		{
			name:      "unsupported scheme is rejected",
			serverURL: "ftp://github.example.com",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, deriveAPIFromServerURL(tt.serverURL))
		})
	}
}

func TestDeriveGitHubAPIURL(t *testing.T) {
	tests := []struct {
		name       string
		envAPIURL  string
		envSrvURL  string
		defaultURL string
		expected   string
	}{
		{
			name:       "default when no env vars",
			envAPIURL:  "",
			envSrvURL:  "",
			defaultURL: DefaultGitHubAPIBaseURL,
			expected:   DefaultGitHubAPIBaseURL,
		},
		{
			name:       "empty default when no env vars",
			envAPIURL:  "",
			envSrvURL:  "",
			defaultURL: "",
			expected:   "",
		},
		{
			name:       "GITHUB_API_URL takes priority",
			envAPIURL:  "https://api.custom.ghe.com",
			envSrvURL:  "https://other.ghe.com",
			defaultURL: DefaultGitHubAPIBaseURL,
			expected:   "https://api.custom.ghe.com",
		},
		{
			name:       "derive from GITHUB_SERVER_URL",
			envAPIURL:  "",
			envSrvURL:  "https://mycompany.ghe.com",
			defaultURL: DefaultGitHubAPIBaseURL,
			expected:   "https://copilot-api.mycompany.ghe.com",
		},
		{
			name:       "invalid GITHUB_SERVER_URL falls back to default",
			envAPIURL:  "",
			envSrvURL:  "not-a-valid-url",
			defaultURL: DefaultGitHubAPIBaseURL,
			expected:   DefaultGitHubAPIBaseURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_API_URL", tt.envAPIURL)
			t.Setenv("GITHUB_SERVER_URL", tt.envSrvURL)
			assert.Equal(t, tt.expected, DeriveGitHubAPIURL(tt.defaultURL))
		})
	}
}

package envutil

import (
	"os"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logGitHub = logger.New("envutil:github")

// LookupGitHubToken searches environment variables for a GitHub token using
// a canonical priority order. It returns the first non-empty (trimmed) value
// found, or an empty string if none is set.
//
// Priority order:
//  1. GITHUB_MCP_SERVER_TOKEN
//  2. GITHUB_TOKEN
//  3. GITHUB_PERSONAL_ACCESS_TOKEN
//  4. GH_TOKEN
func LookupGitHubToken() string {
	candidates := []string{
		"GITHUB_MCP_SERVER_TOKEN",
		"GITHUB_TOKEN",
		"GITHUB_PERSONAL_ACCESS_TOKEN",
		"GH_TOKEN",
	}
	logGitHub.Printf("Looking up GitHub token: checking %d candidate env vars", len(candidates))
	for _, key := range candidates {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			logGitHub.Printf("GitHub token found: source=%s", key)
			return v
		}
	}
	logGitHub.Print("GitHub token not found in any candidate env var")
	return ""
}

// LookupGitHubAPIURL returns the GitHub API base URL from the GITHUB_API_URL
// environment variable. If the variable is not set or empty, it returns
// defaultURL. Any trailing slash is stripped from the result.
func LookupGitHubAPIURL(defaultURL string) string {
	if v := strings.TrimSpace(os.Getenv("GITHUB_API_URL")); v != "" {
		url := strings.TrimRight(v, "/")
		logGitHub.Printf("GitHub API URL from GITHUB_API_URL: %s", url)
		return url
	}
	url := strings.TrimRight(strings.TrimSpace(defaultURL), "/")
	logGitHub.Printf("GitHub API URL using default: %s", url)
	return url
}

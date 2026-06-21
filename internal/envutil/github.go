package envutil

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/sanitize"
)

var logGitHub = logger.New("envutil:github")

// DefaultGitHubAPIBaseURL is the default GitHub API base URL.
const DefaultGitHubAPIBaseURL = "https://api.github.com"

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

// DeriveGitHubAPIURL resolves the GitHub API URL from environment variables:
//  1. GITHUB_API_URL
//  2. GITHUB_SERVER_URL (derived)
//  3. defaultURL
func DeriveGitHubAPIURL(defaultURL string) string {
	if apiURL := LookupGitHubAPIURL(""); apiURL != "" {
		return apiURL
	}
	if serverURL := strings.TrimSpace(os.Getenv("GITHUB_SERVER_URL")); serverURL != "" {
		derived := deriveAPIFromServerURL(serverURL)
		if derived != "" {
			logGitHub.Printf("GitHub API URL derived from GITHUB_SERVER_URL=%s: %s", sanitize.RedactURL(serverURL), sanitize.RedactURL(derived))
			return derived
		}
	}
	result := strings.TrimRight(strings.TrimSpace(defaultURL), "/")
	logGitHub.Printf("GitHub API URL falling back to provided default: %s", sanitize.RedactURL(result))
	return result
}

// deriveAPIFromServerURL converts a GITHUB_SERVER_URL to the corresponding API endpoint.
// GHEC tenants (*.ghe.com): https://tenant.ghe.com → https://copilot-api.tenant.ghe.com
// GitHub.com: https://github.com → https://api.github.com
// GHES (all others): https://github.example.com → https://github.example.com/api/v3
func deriveAPIFromServerURL(serverURL string) string {
	logGitHub.Printf("Deriving API URL from server URL: %s", sanitize.RedactURL(serverURL))

	parsed, err := url.Parse(strings.TrimRight(serverURL, "/"))
	if err != nil || parsed.Host == "" {
		logGitHub.Printf("Failed to parse server URL or empty host: serverURL=%s, err=%v", sanitize.RedactURL(serverURL), err)
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		logGitHub.Printf("Unsupported scheme in server URL: scheme=%s, serverURL=%s", parsed.Scheme, sanitize.RedactURL(serverURL))
		return ""
	}

	hostname := strings.ToLower(parsed.Hostname())

	switch {
	case hostname == "github.com" || hostname == "www.github.com":
		logGitHub.Printf("GitHub.com detected, using default API URL: %s", DefaultGitHubAPIBaseURL)
		return DefaultGitHubAPIBaseURL
	case strings.HasSuffix(hostname, ".ghe.com"):
		var apiURL string
		if port := parsed.Port(); port != "" {
			apiURL = fmt.Sprintf("%s://copilot-api.%s:%s", parsed.Scheme, hostname, port)
		} else {
			apiURL = fmt.Sprintf("%s://copilot-api.%s", parsed.Scheme, hostname)
		}
		logGitHub.Printf("GHEC tenant detected, using copilot-api subdomain: hostname=%s, apiURL=%s", hostname, apiURL)
		return apiURL
	default:
		apiURL := fmt.Sprintf("%s://%s/api/v3", parsed.Scheme, parsed.Host)
		logGitHub.Printf("GHES instance detected, using /api/v3 path: host=%s, apiURL=%s", parsed.Host, apiURL)
		return apiURL
	}
}

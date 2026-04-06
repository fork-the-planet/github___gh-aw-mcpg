package proxy

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDeriveAPIFromServerURL_ExtendedCoverage covers branches in deriveAPIFromServerURL
// that are not exercised by the existing TestDeriveAPIFromServerURL test:
//
//   - www.github.com hostname (alternate form of github.com handled in same switch case)
//   - Upper-case hostname is normalised before matching
//   - Non-HTTPS (http) GHEC tenants preserve the http scheme
//   - Non-HTTPS GHES servers preserve the http scheme
//   - github.com URL that includes a path component (path is ignored)
//   - URL with empty host (parse returns empty Host)
//   - ghe.com itself is NOT treated as a GHEC tenant (HasSuffix requires a dot prefix)
//   - deeply-nested GHEC subdomain
func TestDeriveAPIFromServerURL_ExtendedCoverage(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		expected  string
	}{
		// --- github.com / www.github.com ---
		{
			// www.github.com is explicitly handled by the same switch case as github.com.
			// The existing test only covers "github.com" without the www prefix.
			name:      "www.github.com returns default API base",
			serverURL: "https://www.github.com",
			expected:  DefaultGitHubAPIBase,
		},
		{
			// www.github.com with trailing slash
			name:      "www.github.com with trailing slash",
			serverURL: "https://www.github.com/",
			expected:  DefaultGitHubAPIBase,
		},
		{
			// Hostname comparisons use strings.ToLower, so upper-case is normalised.
			name:      "GITHUB.COM upper-case normalised to DefaultGitHubAPIBase",
			serverURL: "https://GITHUB.COM",
			expected:  DefaultGitHubAPIBase,
		},
		{
			// A path in the URL should not affect the derived API URL because
			// parsed.Hostname() returns only the host portion.
			name:      "github.com with path component",
			serverURL: "https://github.com/org/repo",
			expected:  DefaultGitHubAPIBase,
		},

		// --- GHEC (*.ghe.com) ---
		{
			// When the scheme is http (not https), the scheme must be preserved.
			name:      "http GHEC tenant preserves http scheme",
			serverURL: "http://mycompany.ghe.com",
			expected:  "http://copilot-api.mycompany.ghe.com",
		},
		{
			// Deeply-nested subdomain: every part before .ghe.com is the tenant label.
			name:      "deeply nested GHEC subdomain",
			serverURL: "https://deep.nested.tenant.ghe.com",
			expected:  "https://copilot-api.deep.nested.tenant.ghe.com",
		},
		{
			// Upper-case .GHE.COM hostname is normalised before the HasSuffix check.
			name:      "upper-case GHE.COM tenant normalised",
			serverURL: "https://MYCOMPANY.GHE.COM",
			expected:  "https://copilot-api.mycompany.ghe.com",
		},
		{
			// ghe.com itself: HasSuffix("ghe.com", ".ghe.com") is false, so it falls
			// through to the GHES case and receives the /api/v3 suffix.
			name:      "bare ghe.com is treated as GHES not GHEC",
			serverURL: "https://ghe.com",
			expected:  "https://ghe.com/api/v3",
		},

		// --- GHES (all other hosts) ---
		{
			// Non-HTTPS GHES server must preserve the http scheme.
			name:      "http GHES server preserves http scheme",
			serverURL: "http://github.mycompany.com",
			expected:  "http://github.mycompany.com/api/v3",
		},
		{
			// URL with a path: the path is NOT included in parsed.Host so it is
			// correctly omitted from the derived GHES URL.
			name:      "GHES URL with path component ignores path",
			serverURL: "https://github.example.com/enterprise",
			expected:  "https://github.example.com/api/v3",
		},

		// --- Edge cases: parse errors and empty host ---
		{
			// A URL with a scheme but no host at all (parsed.Host == "").
			// The function should return "" to signal an unusable URL.
			name:      "URL with empty host returns empty string",
			serverURL: "http://",
			expected:  "",
		},
		{
			// A file:// URL has no network host, so parsed.Host is "".
			name:      "file URL with no host returns empty string",
			serverURL: "file:///some/path",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deriveAPIFromServerURL(tt.serverURL)
			assert.Equal(t, tt.expected, result, "deriveAPIFromServerURL(%q)", tt.serverURL)
		})
	}
}

// TestDeriveGitHubAPIURL_ExtendedCoverage covers branches in DeriveGitHubAPIURL that
// the existing TestDeriveGitHubAPIURL does not exercise:
//
//   - GITHUB_SERVER_URL set to an invalid URL (deriveAPIFromServerURL returns "") → function returns ""
//   - GITHUB_SERVER_URL set to a URL with empty host → function returns ""
//   - GITHUB_API_URL set to empty string explicitly → falls through to GITHUB_SERVER_URL
//   - Neither var set (already tested) just to confirm baseline
func TestDeriveGitHubAPIURL_ExtendedCoverage(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected string
	}{
		{
			// When GITHUB_SERVER_URL is set to an invalid URL that cannot be parsed
			// into a usable host, deriveAPIFromServerURL returns "" and
			// DeriveGitHubAPIURL must propagate that by returning "" as well.
			name:     "GITHUB_SERVER_URL invalid URL returns empty",
			envVars:  map[string]string{"GITHUB_SERVER_URL": "not-a-valid-url"},
			expected: "",
		},
		{
			// A URL that parses without error but has an empty host also causes
			// deriveAPIFromServerURL to return "", which should propagate.
			name:     "GITHUB_SERVER_URL with empty host returns empty",
			envVars:  map[string]string{"GITHUB_SERVER_URL": "http://"},
			expected: "",
		},
		{
			// GITHUB_API_URL set to an explicitly empty string is treated the same
			// as not setting it at all (os.Getenv returns ""), so the function falls
			// through to check GITHUB_SERVER_URL.
			name:     "empty GITHUB_API_URL falls through to GITHUB_SERVER_URL",
			envVars:  map[string]string{"GITHUB_API_URL": "", "GITHUB_SERVER_URL": "https://mycompany.ghe.com"},
			expected: "https://copilot-api.mycompany.ghe.com",
		},
		{
			// When GITHUB_SERVER_URL is www.github.com (the untested alias),
			// DeriveGitHubAPIURL should derive DefaultGitHubAPIBase.
			name:     "GITHUB_SERVER_URL www.github.com returns DefaultGitHubAPIBase",
			envVars:  map[string]string{"GITHUB_SERVER_URL": "https://www.github.com"},
			expected: DefaultGitHubAPIBase,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore the two env vars around each sub-test.
			savedAPI, hadAPI := os.LookupEnv("GITHUB_API_URL")
			savedServer, hadServer := os.LookupEnv("GITHUB_SERVER_URL")
			t.Cleanup(func() {
				if hadAPI {
					_ = os.Setenv("GITHUB_API_URL", savedAPI)
				} else {
					_ = os.Unsetenv("GITHUB_API_URL")
				}
				if hadServer {
					_ = os.Setenv("GITHUB_SERVER_URL", savedServer)
				} else {
					_ = os.Unsetenv("GITHUB_SERVER_URL")
				}
			})
			_ = os.Unsetenv("GITHUB_API_URL")
			_ = os.Unsetenv("GITHUB_SERVER_URL")

			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
			}

			result := DeriveGitHubAPIURL()
			assert.Equal(t, tt.expected, result)
		})
	}
}

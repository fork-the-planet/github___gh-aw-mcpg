// Package envutil tests: coverage for deriveAPIFromServerURL edge cases.
//
// The existing TestDeriveAPIFromServerURL in github_test.go covers only
// https:// scheme URLs. This file adds targeted coverage for:
//   - http:// scheme with a .ghe.com hostname (GHEC tenant)
//   - http:// scheme with a custom GHES hostname
//   - empty URL (triggers the empty-host guard)
//   - URL with a trailing slash (exercises the strings.TrimRight path)
package envutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDeriveAPIFromServerURL_HTTPSchemeAndEdgeCases covers the branches in
// deriveAPIFromServerURL that are not exercised by the existing test suite:
//
//  1. The http (non-HTTPS) scheme is accepted for both GHEC and GHES hosts.
//  2. An empty URL returns "" because url.Parse("") sets Host to "".
//  3. A trailing "/" is stripped by strings.TrimRight before parsing, so the
//     result must equal the no-trailing-slash equivalent.
func TestDeriveAPIFromServerURL_HTTPSchemeAndEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		expected  string
	}{
		{
			// http scheme is explicitly allowed; schemes other than "http" and
			// "https" are rejected.
			// GHEC data-residency tenant: prepend "api." subdomain.
			name:      "http scheme GHEC tenant derives api subdomain",
			serverURL: "http://mycompany.ghe.com",
			expected:  "http://api.mycompany.ghe.com",
		},
		{
			// GHEC tenant with both http scheme and a port number.
			name:      "http scheme GHEC tenant with port",
			serverURL: "http://mycompany.ghe.com:8080",
			expected:  "http://api.mycompany.ghe.com:8080",
		},
		{
			// http scheme is allowed for GHES hosts as well.
			name:      "http scheme GHES instance uses /api/v3 path",
			serverURL: "http://github.example.com",
			expected:  "http://github.example.com/api/v3",
		},
		{
			// http scheme with GHES and an explicit port.
			name:      "http scheme GHES instance with port uses /api/v3 path",
			serverURL: "http://github.example.com:9090",
			expected:  "http://github.example.com:9090/api/v3",
		},
		{
			// url.Parse("") succeeds but sets Host to "", which triggers the
			// "err != nil || parsed.Host == ''" guard and returns "".
			name:      "empty URL returns empty string",
			serverURL: "",
			expected:  "",
		},
		{
			// strings.TrimRight strips the trailing slash before url.Parse, so
			// "https://github.com/" behaves identically to "https://github.com".
			name:      "github.com with trailing slash returns default API URL",
			serverURL: "https://github.com/",
			expected:  DefaultGitHubAPIBaseURL,
		},
		{
			// Multiple trailing slashes are all stripped by TrimRight.
			name:      "GHES URL with multiple trailing slashes",
			serverURL: "https://github.example.com///",
			expected:  "https://github.example.com/api/v3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, deriveAPIFromServerURL(tt.serverURL))
		})
	}
}

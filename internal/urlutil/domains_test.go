package urlutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractURLDomains(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty string returns nil",
			input: "",
			want:  nil,
		},
		{
			name:  "no URLs returns nil",
			input: "this has no URLs at all",
			want:  nil,
		},
		{
			name:  "single http URL",
			input: "http://example.com",
			want:  []string{"example.com"},
		},
		{
			name:  "single https URL",
			input: "https://example.com",
			want:  []string{"example.com"},
		},
		{
			name:  "URL with path",
			input: "https://example.com/some/path",
			want:  []string{"example.com"},
		},
		{
			name:  "URL with query string",
			input: "https://example.com/search?q=test&page=1",
			want:  []string{"example.com"},
		},
		{
			name:  "URL with port number",
			input: "https://example.com:8080/path",
			want:  []string{"example.com"},
		},
		{
			name:  "hostname is lowercased",
			input: "https://EXAMPLE.COM/path",
			want:  []string{"example.com"},
		},
		{
			name:  "scheme is case-insensitive (uppercase)",
			input: "HTTPS://example.com",
			want:  []string{"example.com"},
		},
		{
			name:  "scheme is case-insensitive (mixed case)",
			input: "Https://example.com",
			want:  []string{"example.com"},
		},
		{
			name:  "trailing comma stripped",
			input: "See https://example.com, for details.",
			want:  []string{"example.com"},
		},
		{
			name:  "trailing period stripped",
			input: "Visit https://example.com.",
			want:  []string{"example.com"},
		},
		{
			name:  "trailing semicolon stripped",
			input: "URL: https://example.com; end",
			want:  []string{"example.com"},
		},
		{
			name:  "trailing colon stripped",
			input: "URL: https://example.com: end",
			want:  []string{"example.com"},
		},
		{
			name:  "trailing exclamation stripped",
			input: "Go to https://example.com!",
			want:  []string{"example.com"},
		},
		{
			name:  "trailing closing parenthesis stripped",
			input: "Link (https://example.com)",
			want:  []string{"example.com"},
		},
		{
			name:  "trailing closing bracket stripped",
			input: "[https://example.com]",
			want:  []string{"example.com"},
		},
		{
			name:  "trailing closing brace stripped",
			input: "{https://example.com}",
			want:  []string{"example.com"},
		},
		{
			name:  "trailing double quote stripped",
			input: `href="https://example.com"`,
			want:  []string{"example.com"},
		},
		{
			name:  "multiple different domains returns sorted list",
			input: "https://zebra.com and https://alpha.com and https://middle.com",
			want:  []string{"alpha.com", "middle.com", "zebra.com"},
		},
		{
			name:  "duplicate domains are deduplicated",
			input: "https://example.com/page1 and https://example.com/page2",
			want:  []string{"example.com"},
		},
		{
			name:  "URL embedded in markdown link",
			input: "[Click here](https://example.com/page)",
			want:  []string{"example.com"},
		},
		{
			name:  "URL embedded in HTML anchor",
			input: `<a href="https://example.com">text</a>`,
			want:  []string{"example.com"},
		},
		{
			name:  "subdomain is preserved",
			input: "https://api.github.com/repos",
			want:  []string{"api.github.com"},
		},
		{
			name:  "multiple subdomains preserved as distinct domains",
			input: "https://api.example.com and https://www.example.com",
			want:  []string{"api.example.com", "www.example.com"},
		},
		{
			name:  "URL with fragment",
			input: "https://example.com/page#section",
			want:  []string{"example.com"},
		},
		{
			name:  "URL with user info",
			input: "https://user@example.com/path",
			want:  []string{"example.com"},
		},
		{
			name:  "URL with IPv4 address",
			input: "https://192.168.1.1/path",
			want:  []string{"192.168.1.1"},
		},
		{
			// https://user@ has an empty hostname (userinfo only); url.Parse succeeds but
			// Hostname() returns "". The empty host branch is hit and domainSet stays empty.
			name:  "URL with userinfo but no hostname returns nil",
			input: "https://user@",
			want:  nil,
		},
		{
			// Invalid percent-encoding causes url.Parse to return a *url.Error; the URL is
			// skipped and the function returns nil when no valid hosts remain.
			name:  "URL with invalid percent-encoding is skipped",
			input: "https://example.com/%GG",
			want:  nil,
		},
		{
			// Only the parseable URL contributes when mixed with an invalid one.
			name:  "mix of valid and invalid percent-encoding URLs",
			input: "https://valid.com/path https://example.com/%ZZ",
			want:  []string{"valid.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractURLDomains(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractURLDomainsFromValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  []string
	}{
		{
			name:  "nil value returns nil",
			value: nil,
			want:  nil,
		},
		{
			name:  "integer value (unmatched type) returns nil",
			value: 42,
			want:  nil,
		},
		{
			name:  "boolean value (unmatched type) returns nil",
			value: true,
			want:  nil,
		},
		{
			name:  "string with no URLs returns nil",
			value: "no urls here",
			want:  nil,
		},
		{
			name:  "string with one URL returns domain",
			value: "check https://example.com for info",
			want:  []string{"example.com"},
		},
		{
			name:  "string with multiple URLs returns sorted unique domains",
			value: "https://beta.com and https://alpha.com",
			want:  []string{"alpha.com", "beta.com"},
		},
		{
			name: "map[string]any with URL values",
			value: map[string]any{
				"url": "https://example.com/api",
			},
			want: []string{"example.com"},
		},
		{
			name: "map[string]any with multiple URL values",
			value: map[string]any{
				"url1": "https://alpha.com",
				"url2": "https://beta.com",
			},
			want: []string{"alpha.com", "beta.com"},
		},
		{
			name: "map[string]any with duplicate domains deduplicates",
			value: map[string]any{
				"url1": "https://example.com/page1",
				"url2": "https://example.com/page2",
			},
			want: []string{"example.com"},
		},
		{
			name: "map[string]any with non-URL string value returns nil",
			value: map[string]any{
				"name": "no url here",
			},
			want: nil,
		},
		{
			name: "map[string]any with integer value is ignored",
			value: map[string]any{
				"count": 42,
			},
			want: nil,
		},
		{
			name: "[]any with URL strings",
			value: []any{
				"https://example.com",
				"https://other.com",
			},
			want: []string{"example.com", "other.com"},
		},
		{
			name: "[]any with mixed types, only strings processed",
			value: []any{
				"https://example.com",
				42,
				true,
				"no-url",
			},
			want: []string{"example.com"},
		},
		{
			name:  "[]any empty slice returns nil",
			value: []any{},
			want:  nil,
		},
		{
			name: "[]map[string]any with URL values",
			value: []map[string]any{
				{"url": "https://alpha.com"},
				{"url": "https://beta.com"},
			},
			want: []string{"alpha.com", "beta.com"},
		},
		{
			name: "[]map[string]any with duplicate domains deduplicates",
			value: []map[string]any{
				{"url": "https://example.com/page1"},
				{"url": "https://example.com/page2"},
			},
			want: []string{"example.com"},
		},
		{
			name: "nested map[string]any with nested map",
			value: map[string]any{
				"outer": map[string]any{
					"inner": "https://nested.example.com",
				},
			},
			want: []string{"nested.example.com"},
		},
		{
			name: "[]any containing maps",
			value: []any{
				map[string]any{"href": "https://first.com"},
				map[string]any{"href": "https://second.com"},
			},
			want: []string{"first.com", "second.com"},
		},
		{
			name: "deduplication: same domain in multiple map values",
			value: map[string]any{
				"link1": "https://example.com/a",
				"link2": "https://example.com/b",
			},
			want: []string{"example.com"},
		},
		{
			name: "result is sorted alphabetically",
			value: []any{
				"https://zoo.com",
				"https://apple.com",
				"https://mango.com",
			},
			want: []string{"apple.com", "mango.com", "zoo.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractURLDomainsFromValue(tt.value)
			assert.Equal(t, tt.want, got)
		})
	}
}

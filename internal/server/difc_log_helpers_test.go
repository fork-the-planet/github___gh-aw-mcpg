package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExtractAuthorLogin exercises all code paths of the extractAuthorLogin helper.
func TestExtractAuthorLogin(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]interface{}
		want string
	}{
		{
			name: "empty map returns empty string",
			m:    map[string]interface{}{},
			want: "",
		},
		{
			name: "user object with login returns login",
			m: map[string]interface{}{
				"user": map[string]interface{}{"login": "octocat"},
			},
			want: "octocat",
		},
		{
			name: "author object with login returns login when no user",
			m: map[string]interface{}{
				"author": map[string]interface{}{"login": "monalisa"},
			},
			want: "monalisa",
		},
		{
			name: "user object takes priority over author object",
			m: map[string]interface{}{
				"user":   map[string]interface{}{"login": "user-login"},
				"author": map[string]interface{}{"login": "author-login"},
			},
			want: "user-login",
		},
		{
			name: "user object without login falls through to author",
			m: map[string]interface{}{
				"user":   map[string]interface{}{"name": "Full Name"},
				"author": map[string]interface{}{"login": "fallback-login"},
			},
			want: "fallback-login",
		},
		{
			name: "user object with non-string login returns empty string",
			m: map[string]interface{}{
				"user": map[string]interface{}{"login": 42},
			},
			want: "",
		},
		{
			name: "user is not a map returns empty string",
			m: map[string]interface{}{
				"user": "not-a-map",
			},
			want: "",
		},
		{
			name: "author is not a map returns empty string",
			m: map[string]interface{}{
				"author": "also-not-a-map",
			},
			want: "",
		},
		{
			name: "user nil falls through to author with login",
			m: map[string]interface{}{
				"user":   nil,
				"author": map[string]interface{}{"login": "author-login"},
			},
			want: "author-login",
		},
		{
			name: "neither user nor author present",
			m: map[string]interface{}{
				"title": "some issue",
				"body":  "description",
			},
			want: "",
		},
		{
			name: "user object with empty login returns empty (does not fall through to author)",
			m: map[string]interface{}{
				"user":   map[string]interface{}{"login": ""},
				"author": map[string]interface{}{"login": "author-login"},
			},
			// user.login="" is a valid string type assertion (ok=true), so it returns ""
			// immediately without checking author.login.
			want: "",
		},
		{
			name: "commit-style author with user.login nested two levels deep is not extracted",
			m: map[string]interface{}{
				"author": map[string]interface{}{
					"user":  map[string]interface{}{"login": "nested-login"},
					"email": "dev@example.com",
				},
			},
			// extractAuthorLogin looks for author["login"] (a direct string field),
			// not author["user"]["login"]. So this returns "" since author.login is absent.
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAuthorLogin(tt.m)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestExtractNumberField exercises all code paths of the extractNumberField helper.
func TestExtractNumberField(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]interface{}
		want string
	}{
		{
			name: "empty map returns empty string",
			m:    map[string]interface{}{},
			want: "",
		},
		{
			name: "number as float64 returns integer string",
			m:    map[string]interface{}{"number": float64(42)},
			want: "42",
		},
		{
			name: "number as json.Number returns string representation",
			m:    map[string]interface{}{"number": json.Number("1234")},
			want: "1234",
		},
		{
			name: "large float64 number",
			m:    map[string]interface{}{"number": float64(99999)},
			want: "99999",
		},
		{
			name: "number zero as float64",
			m:    map[string]interface{}{"number": float64(0)},
			want: "0",
		},
		{
			name: "number as int returns empty string (not float64 or json.Number)",
			m:    map[string]interface{}{"number": 42},
			want: "",
		},
		{
			name: "number as string returns empty string",
			m:    map[string]interface{}{"number": "42"},
			want: "",
		},
		{
			name: "number as bool returns empty string",
			m:    map[string]interface{}{"number": true},
			want: "",
		},
		{
			name: "no number field returns empty string",
			m:    map[string]interface{}{"title": "issue title", "body": "body text"},
			want: "",
		},
		{
			name: "number field is nil returns empty string",
			m:    map[string]interface{}{"number": nil},
			want: "",
		},
		{
			name: "number as float64 with decimal truncates to integer",
			m:    map[string]interface{}{"number": float64(123.9)},
			want: "123",
		},
		{
			name: "json.Number as large PR number",
			m:    map[string]interface{}{"number": json.Number("9876543")},
			want: "9876543",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractNumberField(tt.m)
			assert.Equal(t, tt.want, got)
		})
	}
}

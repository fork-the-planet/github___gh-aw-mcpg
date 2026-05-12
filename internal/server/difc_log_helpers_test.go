package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetFilteredItemStringField exercises all code paths of the
// getFilteredItemStringField helper.
func TestGetFilteredItemStringField(t *testing.T) {
	tests := []struct {
		name   string
		m      map[string]interface{}
		fields []string
		want   string
	}{
		{
			name:   "empty map returns empty string",
			m:      map[string]interface{}{},
			fields: []string{"title"},
			want:   "",
		},
		{
			name:   "single field found returns value",
			m:      map[string]interface{}{"title": "My Issue"},
			fields: []string{"title"},
			want:   "My Issue",
		},
		{
			name:   "single field present but empty string is skipped",
			m:      map[string]interface{}{"title": ""},
			fields: []string{"title"},
			want:   "",
		},
		{
			name:   "first field missing second field found",
			m:      map[string]interface{}{"htmlUrl": "https://example.com"},
			fields: []string{"html_url", "htmlUrl"},
			want:   "https://example.com",
		},
		{
			name:   "first field found returns first match",
			m:      map[string]interface{}{"html_url": "https://first.com", "htmlUrl": "https://second.com"},
			fields: []string{"html_url", "htmlUrl"},
			want:   "https://first.com",
		},
		{
			name:   "all fields missing returns empty string",
			m:      map[string]interface{}{"other": "value"},
			fields: []string{"html_url", "htmlUrl"},
			want:   "",
		},
		{
			name:   "field present with non-string value is skipped",
			m:      map[string]interface{}{"number": 42},
			fields: []string{"number"},
			want:   "",
		},
		{
			name:   "field present with bool value is skipped",
			m:      map[string]interface{}{"active": true},
			fields: []string{"active"},
			want:   "",
		},
		{
			name:   "field present with nil value is skipped",
			m:      map[string]interface{}{"login": nil},
			fields: []string{"login"},
			want:   "",
		},
		{
			name:   "first field non-string second field string returns second",
			m:      map[string]interface{}{"author_association": 99, "authorAssociation": "CONTRIBUTOR"},
			fields: []string{"author_association", "authorAssociation"},
			want:   "CONTRIBUTOR",
		},
		{
			name:   "no fields argument returns empty string",
			m:      map[string]interface{}{"title": "value"},
			fields: []string{},
			want:   "",
		},
		{
			name:   "nil map returns empty string",
			m:      nil,
			fields: []string{"field"},
			want:   "",
		},
		{
			name:   "sha field extraction",
			m:      map[string]interface{}{"sha": "abc123def456"},
			fields: []string{"sha"},
			want:   "abc123def456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFilteredItemStringField(tt.m, tt.fields...)
			assert.Equal(t, tt.want, got)
		})
	}
}

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

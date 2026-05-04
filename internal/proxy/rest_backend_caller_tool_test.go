package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCallerWithMux creates a restBackendCaller backed by a mock server using the given mux.
func newTestCallerWithMux(t *testing.T, mux *http.ServeMux) (*restBackendCaller, *httptest.Server) {
	t.Helper()
	mockServer := httptest.NewServer(mux)
	t.Cleanup(mockServer.Close)

	proxyServer := &Server{
		githubAPIURL: mockServer.URL,
		githubToken:  "test-token",
		httpClient:   http.DefaultClient,
	}
	return &restBackendCaller{server: proxyServer}, mockServer
}

// extractContentText is a helper that unwraps the MCP content response envelope
// and returns the text of the first content item.
func extractContentText(t *testing.T, result interface{}) string {
	t.Helper()
	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")

	content, ok := resultMap["content"].([]map[string]interface{})
	require.True(t, ok, "content should be []map[string]interface{}")
	require.NotEmpty(t, content)

	text, ok := content[0]["text"].(string)
	require.True(t, ok, "first content item should have a text field")
	return text
}

// TestRestBackendCaller_PullRequestRead tests the pull_request_read branch of CallTool.
func TestRestBackendCaller_PullRequestRead(t *testing.T) {
	mux := http.NewServeMux()

	// Mock a successful PR response
	mux.HandleFunc("/repos/myorg/myrepo/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"number": 42,
			"title":  "Test PR",
			"state":  "open",
		})
	})

	caller, _ := newTestCallerWithMux(t, mux)

	tests := []struct {
		name        string
		args        map[string]interface{}
		wantErr     bool
		errContains string
		wantTitle   string
	}{
		{
			name: "pullNumber as string",
			args: map[string]interface{}{
				"owner":      "myorg",
				"repo":       "myrepo",
				"pullNumber": "42",
			},
			wantTitle: "Test PR",
		},
		{
			name: "pullNumber as float64 (JSON number)",
			args: map[string]interface{}{
				"owner":      "myorg",
				"repo":       "myrepo",
				"pullNumber": float64(42),
			},
			wantTitle: "Test PR",
		},
		{
			name: "missing owner",
			args: map[string]interface{}{
				"repo":       "myrepo",
				"pullNumber": "42",
			},
			wantErr:     true,
			errContains: "missing owner/repo/pullNumber",
		},
		{
			name: "missing repo",
			args: map[string]interface{}{
				"owner":      "myorg",
				"pullNumber": "42",
			},
			wantErr:     true,
			errContains: "missing owner/repo/pullNumber",
		},
		{
			name: "missing pullNumber",
			args: map[string]interface{}{
				"owner": "myorg",
				"repo":  "myrepo",
			},
			wantErr:     true,
			errContains: "missing owner/repo/pullNumber",
		},
		{
			name: "pullNumber empty string and no float64",
			args: map[string]interface{}{
				"owner":      "myorg",
				"repo":       "myrepo",
				"pullNumber": "",
			},
			wantErr:     true,
			errContains: "missing owner/repo/pullNumber",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := caller.CallTool(context.Background(), "pull_request_read", tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.ErrorContains(t, err, tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			text := extractContentText(t, result)
			var parsed map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(text), &parsed))
			assert.Equal(t, tt.wantTitle, parsed["title"])
		})
	}
}

// TestRestBackendCaller_PullRequestRead_404 verifies that a 404 from the upstream API
// is translated to an error.
func TestRestBackendCaller_PullRequestRead_404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/myorg/myrepo/pulls/999", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})

	caller, _ := newTestCallerWithMux(t, mux)

	_, err := caller.CallTool(context.Background(), "pull_request_read", map[string]interface{}{
		"owner":      "myorg",
		"repo":       "myrepo",
		"pullNumber": "999",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "404")
}

// TestRestBackendCaller_IssueRead tests the issue_read branch of CallTool.
func TestRestBackendCaller_IssueRead(t *testing.T) {
	mux := http.NewServeMux()

	// Mock a successful issue response
	mux.HandleFunc("/repos/myorg/myrepo/issues/7", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"number": 7,
			"title":  "Test Issue",
			"state":  "open",
		})
	})

	caller, _ := newTestCallerWithMux(t, mux)

	tests := []struct {
		name        string
		args        map[string]interface{}
		wantErr     bool
		errContains string
		wantTitle   string
	}{
		{
			name: "issue_number as string",
			args: map[string]interface{}{
				"owner":        "myorg",
				"repo":         "myrepo",
				"issue_number": "7",
			},
			wantTitle: "Test Issue",
		},
		{
			name: "issue_number as float64 (JSON number)",
			args: map[string]interface{}{
				"owner":        "myorg",
				"repo":         "myrepo",
				"issue_number": float64(7),
			},
			wantTitle: "Test Issue",
		},
		{
			name: "missing owner",
			args: map[string]interface{}{
				"repo":         "myrepo",
				"issue_number": "7",
			},
			wantErr:     true,
			errContains: "missing owner/repo/issue_number",
		},
		{
			name: "missing repo",
			args: map[string]interface{}{
				"owner":        "myorg",
				"issue_number": "7",
			},
			wantErr:     true,
			errContains: "missing owner/repo/issue_number",
		},
		{
			name: "missing issue_number",
			args: map[string]interface{}{
				"owner": "myorg",
				"repo":  "myrepo",
			},
			wantErr:     true,
			errContains: "missing owner/repo/issue_number",
		},
		{
			name: "issue_number empty string and no float64",
			args: map[string]interface{}{
				"owner":        "myorg",
				"repo":         "myrepo",
				"issue_number": "",
			},
			wantErr:     true,
			errContains: "missing owner/repo/issue_number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := caller.CallTool(context.Background(), "issue_read", tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.ErrorContains(t, err, tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			text := extractContentText(t, result)
			var parsed map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(text), &parsed))
			assert.Equal(t, tt.wantTitle, parsed["title"])
		})
	}
}

// TestRestBackendCaller_IssueRead_404 verifies that a 404 from the upstream API is
// translated to an error.
func TestRestBackendCaller_IssueRead_404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/myorg/myrepo/issues/999", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})

	caller, _ := newTestCallerWithMux(t, mux)

	_, err := caller.CallTool(context.Background(), "issue_read", map[string]interface{}{
		"owner":        "myorg",
		"repo":         "myrepo",
		"issue_number": "999",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "404")
}

// TestRestBackendCaller_SearchRepositories tests the search_repositories branch of CallTool.
func TestRestBackendCaller_SearchRepositories(t *testing.T) {
	mux := http.NewServeMux()

	// Mock a successful search response
	mux.HandleFunc("/search/repositories", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		q := r.URL.Query().Get("q")
		assert.NotEmpty(t, q)

		perPage := r.URL.Query().Get("per_page")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 1,
			"per_page":    perPage,
			"items": []map[string]interface{}{
				{"full_name": "myorg/myrepo"},
			},
		})
	})

	caller, _ := newTestCallerWithMux(t, mux)

	tests := []struct {
		name        string
		args        map[string]interface{}
		wantErr     bool
		errContains string
		checkResult func(t *testing.T, text string)
	}{
		{
			name: "basic query with default perPage",
			args: map[string]interface{}{
				"query": "language:go",
			},
			checkResult: func(t *testing.T, text string) {
				assert.Contains(t, text, "myorg/myrepo")
				// Default per_page=10 should be used
				assert.Contains(t, text, `"per_page":"10"`)
			},
		},
		{
			name: "query with custom perPage as float64",
			args: map[string]interface{}{
				"query":   "language:go",
				"perPage": float64(25),
			},
			checkResult: func(t *testing.T, text string) {
				assert.Contains(t, text, "myorg/myrepo")
				assert.Contains(t, text, `"per_page":"25"`)
			},
		},
		{
			name: "missing query",
			args: map[string]interface{}{
				"perPage": float64(10),
			},
			wantErr:     true,
			errContains: "missing query",
		},
		{
			name: "empty query string",
			args: map[string]interface{}{
				"query": "",
			},
			wantErr:     true,
			errContains: "missing query",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := caller.CallTool(context.Background(), "search_repositories", tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.ErrorContains(t, err, tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.checkResult != nil {
				text := extractContentText(t, result)
				tt.checkResult(t, text)
			}
		})
	}
}

// TestRestBackendCaller_SearchRepositories_APIError verifies that a non-200 response
// from the upstream search API is translated to an error.
func TestRestBackendCaller_SearchRepositories_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search/repositories", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"message":"Validation Failed"}`)
	})

	caller, _ := newTestCallerWithMux(t, mux)

	_, err := caller.CallTool(context.Background(), "search_repositories", map[string]interface{}{
		"query": "language:go stars:>1000",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "422")
}

// TestRestBackendCaller_UnsupportedTool verifies that unknown tool names return an error.
func TestRestBackendCaller_UnsupportedTool(t *testing.T) {
	caller := &restBackendCaller{
		server: &Server{
			githubAPIURL: "http://unused",
			httpClient:   http.DefaultClient,
		},
	}

	unsupportedTools := []string{
		"list_issues",
		"get_file_contents",
		"list_pull_requests",
		"create_issue",
		"unknown_tool",
		"",
	}

	for _, toolName := range unsupportedTools {
		t.Run(fmt.Sprintf("tool=%q", toolName), func(t *testing.T) {
			_, err := caller.CallTool(context.Background(), toolName, map[string]interface{}{
				"owner": "myorg",
				"repo":  "myrepo",
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, "unsupported tool")
		})
	}
}

// TestRestBackendCaller_InvalidArgsType verifies that non-map args return an error for all tool types.
func TestRestBackendCaller_InvalidArgsType(t *testing.T) {
	caller := &restBackendCaller{
		server: &Server{
			githubAPIURL: "http://unused",
			httpClient:   http.DefaultClient,
		},
	}

	toolsToTest := []string{
		"pull_request_read",
		"issue_read",
		"search_repositories",
	}

	for _, toolName := range toolsToTest {
		t.Run(toolName, func(t *testing.T) {
			_, err := caller.CallTool(context.Background(), toolName, "not-a-map")
			require.Error(t, err)
			assert.ErrorContains(t, err, "unexpected args type")
		})
	}
}

// TestRestBackendCaller_SearchRepositories_URLForwarding verifies that the query string
// is forwarded to the GitHub API as-is.
func TestRestBackendCaller_SearchRepositories_URLForwarding(t *testing.T) {
	var capturedQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/search/repositories", func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 0,
			"items":       []interface{}{},
		})
	})

	caller, _ := newTestCallerWithMux(t, mux)

	query := "language:go"
	_, err := caller.CallTool(context.Background(), "search_repositories", map[string]interface{}{
		"query": query,
	})
	require.NoError(t, err)
	assert.Equal(t, query, capturedQuery)
}

// TestRestBackendCaller_PullRequestRead_RequestHeaders verifies that proper headers
// are forwarded to the upstream GitHub API.
func TestRestBackendCaller_PullRequestRead_RequestHeaders(t *testing.T) {
	var capturedAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/repo/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"number": 1, "title": "PR"})
	})

	mockServer := httptest.NewServer(mux)
	defer mockServer.Close()

	t.Run("uses server token for enrichment", func(t *testing.T) {
		proxyServer := &Server{
			githubAPIURL: mockServer.URL,
			githubToken:  "server-token",
			httpClient:   http.DefaultClient,
		}
		caller := &restBackendCaller{
			server:     proxyServer,
			clientAuth: "token client-token",
		}

		_, err := caller.CallTool(context.Background(), "pull_request_read", map[string]interface{}{
			"owner":      "org",
			"repo":       "repo",
			"pullNumber": "1",
		})
		require.NoError(t, err)
		assert.Equal(t, "token server-token", capturedAuth)
	})

	t.Run("falls back to client auth when no server token", func(t *testing.T) {
		proxyServer := &Server{
			githubAPIURL: mockServer.URL,
			githubToken:  "",
			httpClient:   http.DefaultClient,
		}
		caller := &restBackendCaller{
			server:     proxyServer,
			clientAuth: "token client-token",
		}

		_, err := caller.CallTool(context.Background(), "pull_request_read", map[string]interface{}{
			"owner":      "org",
			"repo":       "repo",
			"pullNumber": "1",
		})
		require.NoError(t, err)
		assert.Equal(t, "token client-token", capturedAuth)
	})
}

// TestRestBackendCaller_IssueRead_ResponseFormat verifies that the response is wrapped
// in the MCP content envelope format expected by guards.
func TestRestBackendCaller_IssueRead_ResponseFormat(t *testing.T) {
	issueData := map[string]interface{}{
		"number": 5,
		"title":  "Bug report",
		"state":  "closed",
		"user": map[string]interface{}{
			"login": "reporter",
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/repo/issues/5", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(issueData)
	})

	caller, _ := newTestCallerWithMux(t, mux)

	result, err := caller.CallTool(context.Background(), "issue_read", map[string]interface{}{
		"owner":        "org",
		"repo":         "repo",
		"issue_number": "5",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify MCP response envelope: {"content":[{"type":"text","text":"..."}]}
	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	content, ok := resultMap["content"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, content, 1)
	assert.Equal(t, "text", content[0]["type"])

	text, ok := content[0]["text"].(string)
	require.True(t, ok)
	assert.Contains(t, text, "Bug report", "response text should contain the issue title")
}

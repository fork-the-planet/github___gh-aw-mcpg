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

// newRestCallerWithMux creates a mock GitHub API server and a restBackendCaller
// wired to it. The caller uses serverToken for enrichment (empty = use clientAuth).
func newRestCallerWithMux(t *testing.T, mux *http.ServeMux, serverToken, clientToken string) (*restBackendCaller, *httptest.Server) {
	t.Helper()
	mockServer := httptest.NewServer(mux)
	t.Cleanup(mockServer.Close)

	proxyServer := &Server{
		githubAPIURL: mockServer.URL,
		githubToken:  serverToken,
		httpClient:   http.DefaultClient,
	}
	caller := &restBackendCaller{
		server:     proxyServer,
		clientAuth: clientToken,
	}
	return caller, mockServer
}

// extractTextFromMCPResult extracts the first text string from an MCP
// {content:[{type:"text",text:"..."}]} response.
func extractTextFromMCPResult(t *testing.T, result interface{}) string {
	t.Helper()
	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	content, ok := resultMap["content"].([]map[string]interface{})
	require.True(t, ok, "content should be a []map[string]interface{}")
	require.NotEmpty(t, content, "content should have at least one item")
	text, ok := content[0]["text"].(string)
	require.True(t, ok, "first content item should have a text string")
	return text
}

// ---- pull_request_read ----

func TestRestBackendCaller_PullRequestRead(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"number":42,"title":"test PR"}`)
	})

	caller, _ := newRestCallerWithMux(t, mux, "tok", "")

	t.Run("number as string", func(t *testing.T) {
		result, err := caller.CallTool(context.Background(), "pull_request_read", map[string]interface{}{
			"owner":      "owner",
			"repo":       "repo",
			"pullNumber": "42",
		})
		require.NoError(t, err)
		text := extractTextFromMCPResult(t, result)
		var pr map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(text), &pr))
		assert.Equal(t, float64(42), pr["number"])
		assert.Equal(t, "test PR", pr["title"])
	})

	t.Run("number as float64 (JSON number)", func(t *testing.T) {
		result, err := caller.CallTool(context.Background(), "pull_request_read", map[string]interface{}{
			"owner":      "owner",
			"repo":       "repo",
			"pullNumber": float64(42),
		})
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

func TestRestBackendCaller_PullRequestRead_MissingArgs(t *testing.T) {
	caller, _ := newRestCallerWithMux(t, http.NewServeMux(), "tok", "")

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "missing owner",
			args: map[string]interface{}{"repo": "repo", "pullNumber": "1"},
		},
		{
			name: "missing repo",
			args: map[string]interface{}{"owner": "owner", "pullNumber": "1"},
		},
		{
			name: "missing pullNumber",
			args: map[string]interface{}{"owner": "owner", "repo": "repo"},
		},
		{
			name: "all empty",
			args: map[string]interface{}{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := caller.CallTool(context.Background(), "pull_request_read", tc.args)
			require.Error(t, err)
			assert.ErrorContains(t, err, "pull_request_read")
		})
	}
}

func TestRestBackendCaller_PullRequestRead_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/99", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	caller, _ := newRestCallerWithMux(t, mux, "tok", "")

	_, err := caller.CallTool(context.Background(), "pull_request_read", map[string]interface{}{
		"owner":      "owner",
		"repo":       "repo",
		"pullNumber": "99",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "404")
}

// ---- issue_read ----

func TestRestBackendCaller_IssueRead(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/7", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"number":7,"title":"test issue"}`)
	})
	caller, _ := newRestCallerWithMux(t, mux, "tok", "")

	t.Run("number as string", func(t *testing.T) {
		result, err := caller.CallTool(context.Background(), "issue_read", map[string]interface{}{
			"owner":        "owner",
			"repo":         "repo",
			"issue_number": "7",
		})
		require.NoError(t, err)
		text := extractTextFromMCPResult(t, result)
		var issue map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(text), &issue))
		assert.Equal(t, float64(7), issue["number"])
		assert.Equal(t, "test issue", issue["title"])
	})

	t.Run("number as float64 (JSON number)", func(t *testing.T) {
		result, err := caller.CallTool(context.Background(), "issue_read", map[string]interface{}{
			"owner":        "owner",
			"repo":         "repo",
			"issue_number": float64(7),
		})
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

func TestRestBackendCaller_IssueRead_MissingArgs(t *testing.T) {
	caller, _ := newRestCallerWithMux(t, http.NewServeMux(), "tok", "")

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "missing owner",
			args: map[string]interface{}{"repo": "repo", "issue_number": "1"},
		},
		{
			name: "missing repo",
			args: map[string]interface{}{"owner": "owner", "issue_number": "1"},
		},
		{
			name: "missing issue_number",
			args: map[string]interface{}{"owner": "owner", "repo": "repo"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := caller.CallTool(context.Background(), "issue_read", tc.args)
			require.Error(t, err)
			assert.ErrorContains(t, err, "issue_read")
		})
	}
}

func TestRestBackendCaller_IssueRead_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/404", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"message":"Validation Failed"}`)
	})
	caller, _ := newRestCallerWithMux(t, mux, "tok", "")

	_, err := caller.CallTool(context.Background(), "issue_read", map[string]interface{}{
		"owner":        "owner",
		"repo":         "repo",
		"issue_number": "404",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "422")
}

// ---- search_repositories ----

func TestRestBackendCaller_SearchRepositories(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search/repositories", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"total_count":1,"items":[{"full_name":"owner/repo"}]}`)
	})
	caller, _ := newRestCallerWithMux(t, mux, "tok", "")

	t.Run("default perPage", func(t *testing.T) {
		result, err := caller.CallTool(context.Background(), "search_repositories", map[string]interface{}{
			"query": "language:go",
		})
		require.NoError(t, err)
		text := extractTextFromMCPResult(t, result)
		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(text), &resp))
		assert.Equal(t, float64(1), resp["total_count"])
	})

	t.Run("custom perPage", func(t *testing.T) {
		var capturedPerPage string
		mux2 := http.NewServeMux()
		mux2.HandleFunc("/search/repositories", func(w http.ResponseWriter, r *http.Request) {
			capturedPerPage = r.URL.Query().Get("per_page")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"total_count":0,"items":[]}`)
		})
		caller2, _ := newRestCallerWithMux(t, mux2, "tok", "")

		_, err := caller2.CallTool(context.Background(), "search_repositories", map[string]interface{}{
			"query":   "language:rust",
			"perPage": float64(30),
		})
		require.NoError(t, err)
		assert.Equal(t, "30", capturedPerPage)
	})
}

func TestRestBackendCaller_SearchRepositories_MissingQuery(t *testing.T) {
	caller, _ := newRestCallerWithMux(t, http.NewServeMux(), "tok", "")

	_, err := caller.CallTool(context.Background(), "search_repositories", map[string]interface{}{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "search_repositories")
	assert.ErrorContains(t, err, "missing query")
}

func TestRestBackendCaller_SearchRepositories_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search/repositories", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"message":"Service Unavailable"}`)
	})
	caller, _ := newRestCallerWithMux(t, mux, "tok", "")

	_, err := caller.CallTool(context.Background(), "search_repositories", map[string]interface{}{
		"query": "language:go",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "503")
}

// ---- unsupported tool ----

func TestRestBackendCaller_UnsupportedTool(t *testing.T) {
	caller, _ := newRestCallerWithMux(t, http.NewServeMux(), "tok", "")

	_, err := caller.CallTool(context.Background(), "nonexistent_tool", map[string]interface{}{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported tool")
	assert.ErrorContains(t, err, "nonexistent_tool")
}

// ---- enrichment auth fallback for non-collaborator tools ----

func TestRestBackendCaller_EnrichmentAuthForPRRead(t *testing.T) {
	var capturedAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"number":1}`)
	})

	t.Run("uses server token when available", func(t *testing.T) {
		capturedAuth = ""
		caller, _ := newRestCallerWithMux(t, mux, "server-token", "token client-token")

		_, err := caller.CallTool(context.Background(), "pull_request_read", map[string]interface{}{
			"owner": "owner", "repo": "repo", "pullNumber": "1",
		})
		require.NoError(t, err)
		assert.Equal(t, "token server-token", capturedAuth)
	})

	t.Run("falls back to client auth when server token is empty", func(t *testing.T) {
		capturedAuth = ""
		caller, _ := newRestCallerWithMux(t, mux, "", "token client-token")

		_, err := caller.CallTool(context.Background(), "pull_request_read", map[string]interface{}{
			"owner": "owner", "repo": "repo", "pullNumber": "1",
		})
		require.NoError(t, err)
		assert.Equal(t, "token client-token", capturedAuth)
	})

	t.Run("no auth when both tokens are empty", func(t *testing.T) {
		capturedAuth = "initial"
		caller, _ := newRestCallerWithMux(t, mux, "", "")

		_, err := caller.CallTool(context.Background(), "pull_request_read", map[string]interface{}{
			"owner": "owner", "repo": "repo", "pullNumber": "1",
		})
		require.NoError(t, err)
		assert.Empty(t, capturedAuth)
	})
}

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

func TestRestBackendCallerCollaboratorPermission(t *testing.T) {
	// Start a mock GitHub API server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/myorg/myrepo/collaborators/admin-user/permission", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"permission": "admin",
			"user": map[string]interface{}{
				"login": "admin-user",
				"id":    12345,
			},
		})
	})
	mux.HandleFunc("/repos/myorg/myrepo/collaborators/write-user/permission", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"permission": "write",
			"user": map[string]interface{}{
				"login": "write-user",
			},
		})
	})
	mux.HandleFunc("/repos/myorg/myrepo/collaborators/not-found-user/permission", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message": "Not Found"}`)
	})

	mockServer := httptest.NewServer(mux)
	defer mockServer.Close()

	proxyServer := &Server{
		githubAPIURL: mockServer.URL,
		githubToken:  "test-enrichment-token",
		httpClient:   http.DefaultClient,
	}

	caller := &restBackendCaller{
		server:     proxyServer,
		clientAuth: "token client-auth-token",
	}

	t.Run("admin permission", func(t *testing.T) {
		result, err := caller.CallTool(context.Background(), "get_collaborator_permission", map[string]interface{}{
			"owner":    "myorg",
			"repo":     "myrepo",
			"username": "admin-user",
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify MCP response format
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)

		content, ok := resultMap["content"].([]map[string]interface{})
		require.True(t, ok)
		require.Len(t, content, 1)

		text, ok := content[0]["text"].(string)
		require.True(t, ok)

		var parsed map[string]interface{}
		err = json.Unmarshal([]byte(text), &parsed)
		require.NoError(t, err)
		assert.Equal(t, "admin", parsed["permission"])
	})

	t.Run("write permission", func(t *testing.T) {
		result, err := caller.CallTool(context.Background(), "get_collaborator_permission", map[string]interface{}{
			"owner":    "myorg",
			"repo":     "myrepo",
			"username": "write-user",
		})
		require.NoError(t, err)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		content, ok := resultMap["content"].([]map[string]interface{})
		require.True(t, ok)
		text, ok := content[0]["text"].(string)
		require.True(t, ok)

		var parsed map[string]interface{}
		err = json.Unmarshal([]byte(text), &parsed)
		require.NoError(t, err)
		assert.Equal(t, "write", parsed["permission"])
	})

	t.Run("404 returns error", func(t *testing.T) {
		_, err := caller.CallTool(context.Background(), "get_collaborator_permission", map[string]interface{}{
			"owner":    "myorg",
			"repo":     "myrepo",
			"username": "not-found-user",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "404")
	})

	t.Run("missing owner", func(t *testing.T) {
		_, err := caller.CallTool(context.Background(), "get_collaborator_permission", map[string]interface{}{
			"repo":     "myrepo",
			"username": "admin-user",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "missing owner/repo/username")
	})

	t.Run("missing repo", func(t *testing.T) {
		_, err := caller.CallTool(context.Background(), "get_collaborator_permission", map[string]interface{}{
			"owner":    "myorg",
			"username": "admin-user",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "missing owner/repo/username")
	})

	t.Run("missing username", func(t *testing.T) {
		_, err := caller.CallTool(context.Background(), "get_collaborator_permission", map[string]interface{}{
			"owner": "myorg",
			"repo":  "myrepo",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "missing owner/repo/username")
	})

	t.Run("invalid args type", func(t *testing.T) {
		_, err := caller.CallTool(context.Background(), "get_collaborator_permission", "not-a-map")
		require.Error(t, err)
		assert.ErrorContains(t, err, "unexpected args type")
	})
}

func TestRestBackendCallerUsesEnrichmentToken(t *testing.T) {
	var capturedAuth string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"permission": "admin",
			"user":       map[string]interface{}{"login": "user"},
		})
	}))
	defer mockServer.Close()

	t.Run("prefers server token over client auth", func(t *testing.T) {
		proxyServer := &Server{
			githubAPIURL: mockServer.URL,
			githubToken:  "server-enrichment-token",
			httpClient:   http.DefaultClient,
		}
		caller := &restBackendCaller{
			server:     proxyServer,
			clientAuth: "token client-token",
		}

		_, err := caller.CallTool(context.Background(), "get_collaborator_permission", map[string]interface{}{
			"owner":    "org",
			"repo":     "repo",
			"username": "user",
		})
		require.NoError(t, err)
		assert.Equal(t, "token server-enrichment-token", capturedAuth)
	})

	t.Run("falls back to client auth", func(t *testing.T) {
		proxyServer := &Server{
			githubAPIURL: mockServer.URL,
			githubToken:  "",
			httpClient:   http.DefaultClient,
		}
		caller := &restBackendCaller{
			server:     proxyServer,
			clientAuth: "token client-token",
		}

		_, err := caller.CallTool(context.Background(), "get_collaborator_permission", map[string]interface{}{
			"owner":    "org",
			"repo":     "repo",
			"username": "user",
		})
		require.NoError(t, err)
		assert.Equal(t, "token client-token", capturedAuth)
	})
}

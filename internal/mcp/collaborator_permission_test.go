package mcp

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCollaboratorPermissionArgs(t *testing.T) {
	t.Run("returns all fields when present", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"owner":    "myorg",
			"repo":     "myrepo",
			"username": "alice",
		}
		owner, repo, username, err := ParseCollaboratorPermissionArgs(argsMap)
		require.NoError(t, err)
		assert.Equal(t, "myorg", owner)
		assert.Equal(t, "myrepo", repo)
		assert.Equal(t, "alice", username)
	})

	t.Run("error when owner missing", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"repo":     "myrepo",
			"username": "alice",
		}
		owner, repo, username, err := ParseCollaboratorPermissionArgs(argsMap)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing owner/repo/username")
		assert.Equal(t, "", owner)
		assert.Equal(t, "myrepo", repo)
		assert.Equal(t, "alice", username)
	})

	t.Run("error when repo missing", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"owner":    "myorg",
			"username": "alice",
		}
		owner, repo, username, err := ParseCollaboratorPermissionArgs(argsMap)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing owner/repo/username")
		assert.Equal(t, "myorg", owner)
		assert.Equal(t, "", repo)
		assert.Equal(t, "alice", username)
	})

	t.Run("error when username missing", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"owner": "myorg",
			"repo":  "myrepo",
		}
		owner, repo, username, err := ParseCollaboratorPermissionArgs(argsMap)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing owner/repo/username")
		assert.Equal(t, "myorg", owner)
		assert.Equal(t, "myrepo", repo)
		assert.Equal(t, "", username)
	})

	t.Run("returns partial values on error for logging", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"owner": "myorg",
			"repo":  "myrepo",
		}
		owner, repo, username, err := ParseCollaboratorPermissionArgs(argsMap)
		require.Error(t, err)
		assert.Equal(t, "myorg", owner)
		assert.Equal(t, "myrepo", repo)
		assert.Equal(t, "", username)
	})
}

func TestLogAndWrapCollaboratorPermission(t *testing.T) {
	t.Run("logs permission and wraps in MCP format", func(t *testing.T) {
		body := []byte(`{"permission":"admin","user":{"login":"alice"}}`)

		var logged []string
		result := LogAndWrapCollaboratorPermission(body, "myorg", "myrepo", "alice", 200, func(format string, args ...interface{}) {
			logged = append(logged, fmt.Sprintf(format, args...))
		})

		// Verify log message includes owner/repo/username context and permission
		require.Len(t, logged, 1)
		assert.Contains(t, logged[0], "myorg/myrepo")
		assert.Contains(t, logged[0], "alice")
		assert.Contains(t, logged[0], `"admin"`)
		assert.Contains(t, logged[0], "HTTP 200")

		// Verify MCP response format
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")

		content, ok := resultMap["content"].([]map[string]interface{})
		require.True(t, ok, "content should be array of maps")
		require.Len(t, content, 1)
		assert.Equal(t, "text", content[0]["type"])

		text, ok := content[0]["text"].(string)
		require.True(t, ok, "text should be a string")

		var parsed map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, "admin", parsed["permission"])
	})

	t.Run("logs missing permission field", func(t *testing.T) {
		body := []byte(`{"role":"something"}`)

		var logged []string
		result := LogAndWrapCollaboratorPermission(body, "org", "repo", "bob", 200, func(format string, args ...interface{}) {
			logged = append(logged, fmt.Sprintf(format, args...))
		})

		require.Len(t, logged, 1)
		assert.Contains(t, logged[0], "org/repo")
		assert.Contains(t, logged[0], "bob")
		assert.Contains(t, logged[0], "permission field missing")

		// MCP wrap still succeeds
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		content, ok := resultMap["content"].([]map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, string(body), content[0]["text"])
	})

	t.Run("logs JSON parse failure", func(t *testing.T) {
		body := []byte(`not valid json`)

		var logged []string
		result := LogAndWrapCollaboratorPermission(body, "org", "repo", "charlie", 200, func(format string, args ...interface{}) {
			logged = append(logged, fmt.Sprintf(format, args...))
		})

		require.Len(t, logged, 1)
		assert.Contains(t, logged[0], "org/repo")
		assert.Contains(t, logged[0], "charlie")
		assert.Contains(t, logged[0], "JSON parse failed")

		// Body is still wrapped even on parse failure
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		content, ok := resultMap["content"].([]map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, string(body), content[0]["text"])
	})

	t.Run("status code is included in log message", func(t *testing.T) {
		body := []byte(`{"permission":"write"}`)

		var logged []string
		LogAndWrapCollaboratorPermission(body, "org", "repo", "dave", 201, func(format string, args ...interface{}) {
			logged = append(logged, fmt.Sprintf(format, args...))
		})

		require.Len(t, logged, 1)
		assert.Contains(t, logged[0], "HTTP 201")
	})
}

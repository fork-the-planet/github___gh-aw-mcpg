package guard

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/difc"
)

const githubGuardWASMPath = "/Users/lpcox/Desktop/ai/github-guard/github-guard-rust.wasm"

// mockBackendCaller is a simple mock for testing
type mockBackendCaller struct{}

func (m *mockBackendCaller) CallTool(ctx context.Context, toolName string, args interface{}) (interface{}, error) {
	// Return mock data for common calls
	return map[string]interface{}{}, nil
}

func TestGitHubWASMGuard(t *testing.T) {
	// Skip if the WASM file doesn't exist
	if _, err := os.Stat(githubGuardWASMPath); os.IsNotExist(err) {
		t.Skipf("GitHub WASM guard not found at %s", githubGuardWASMPath)
	}

	ctx := context.Background()
	backend := &mockBackendCaller{}

	// Create the WASM guard
	guard, err := NewWasmGuard(ctx, "github", githubGuardWASMPath, backend)
	require.NoError(t, err, "Failed to create GitHub WASM guard")
	require.NotNil(t, guard)
	defer guard.Close(ctx)

	t.Run("Name returns github", func(t *testing.T) {
		assert.Equal(t, "github", guard.Name())
	})

	t.Run("LabelResource_search_repositories_is_read", func(t *testing.T) {
		caps := difc.NewCapabilities()

		args := map[string]interface{}{
			"query": "golang mcp",
		}

		resource, operation, err := guard.LabelResource(ctx, "search_repositories", args, backend, caps)
		require.NoError(t, err, "LabelResource failed")
		require.NotNil(t, resource, "Expected non-nil resource")

		// search_repositories is a read operation
		assert.Equal(t, difc.OperationRead, operation, "Expected search_repositories to be classified as READ")

		t.Logf("search_repositories: operation=%s, secrecy=%v, integrity=%v",
			operation, resource.Secrecy, resource.Integrity)
	})

	t.Run("LabelResource_get_issue_is_read", func(t *testing.T) {
		caps := difc.NewCapabilities()

		args := map[string]interface{}{
			"owner":        "github",
			"repo":         "gh-aw",
			"issue_number": 123,
		}

		resource, operation, err := guard.LabelResource(ctx, "get_issue", args, backend, caps)
		require.NoError(t, err, "LabelResource failed")
		require.NotNil(t, resource, "Expected non-nil resource")

		// get_issue is a read operation
		assert.Equal(t, difc.OperationRead, operation, "Expected get_issue to be classified as READ")

		t.Logf("get_issue: operation=%s, secrecy=%v, integrity=%v",
			operation, resource.Secrecy, resource.Integrity)
	})

	t.Run("LabelResource_list_commits_is_read", func(t *testing.T) {
		caps := difc.NewCapabilities()

		args := map[string]interface{}{
			"owner": "github",
			"repo":  "gh-aw",
		}

		resource, operation, err := guard.LabelResource(ctx, "list_commits", args, backend, caps)
		require.NoError(t, err, "LabelResource failed")
		require.NotNil(t, resource, "Expected non-nil resource")

		// list_commits is a read operation
		assert.Equal(t, difc.OperationRead, operation, "Expected list_commits to be classified as READ")

		t.Logf("list_commits: operation=%s, secrecy=%v, integrity=%v",
			operation, resource.Secrecy, resource.Integrity)
	})

	t.Run("LabelResource_create_issue_is_write", func(t *testing.T) {
		caps := difc.NewCapabilities()

		args := map[string]interface{}{
			"owner": "github",
			"repo":  "gh-aw",
			"title": "Test issue",
			"body":  "This is a test",
		}

		resource, operation, err := guard.LabelResource(ctx, "create_issue", args, backend, caps)
		require.NoError(t, err, "LabelResource failed")
		require.NotNil(t, resource, "Expected non-nil resource")

		// create_issue is a write operation
		assert.Equal(t, difc.OperationWrite, operation, "Expected create_issue to be classified as WRITE")

		t.Logf("create_issue: operation=%s, secrecy=%v, integrity=%v",
			operation, resource.Secrecy, resource.Integrity)
	})

	t.Run("LabelResource_push_files_is_write", func(t *testing.T) {
		caps := difc.NewCapabilities()

		args := map[string]interface{}{
			"owner":   "github",
			"repo":    "gh-aw",
			"branch":  "main",
			"message": "Test commit",
			"files": []map[string]interface{}{
				{"path": "test.txt", "content": "test"},
			},
		}

		resource, operation, err := guard.LabelResource(ctx, "push_files", args, backend, caps)
		require.NoError(t, err, "LabelResource failed")
		require.NotNil(t, resource, "Expected non-nil resource")

		// push_files is a write operation
		assert.Equal(t, difc.OperationWrite, operation, "Expected push_files to be classified as WRITE")

		t.Logf("push_files: operation=%s, secrecy=%v, integrity=%v",
			operation, resource.Secrecy, resource.Integrity)
	})

	t.Run("LabelResource_create_or_update_file_is_write", func(t *testing.T) {
		caps := difc.NewCapabilities()

		args := map[string]interface{}{
			"owner":   "github",
			"repo":    "gh-aw",
			"path":    "test.txt",
			"content": "test content",
			"message": "Test commit",
		}

		resource, operation, err := guard.LabelResource(ctx, "create_or_update_file", args, backend, caps)
		require.NoError(t, err, "LabelResource failed")
		require.NotNil(t, resource, "Expected non-nil resource")

		// create_or_update_file is a write operation
		assert.Equal(t, difc.OperationWrite, operation, "Expected create_or_update_file to be classified as WRITE")

		t.Logf("create_or_update_file: operation=%s, secrecy=%v, integrity=%v",
			operation, resource.Secrecy, resource.Integrity)
	})

	t.Run("LabelResource_get_file_contents_is_read", func(t *testing.T) {
		caps := difc.NewCapabilities()

		args := map[string]interface{}{
			"owner": "github",
			"repo":  "gh-aw",
			"path":  "README.md",
		}

		resource, operation, err := guard.LabelResource(ctx, "get_file_contents", args, backend, caps)
		require.NoError(t, err, "LabelResource failed")
		require.NotNil(t, resource, "Expected non-nil resource")

		// get_file_contents is a read operation
		assert.Equal(t, difc.OperationRead, operation, "Expected get_file_contents to be classified as READ")

		t.Logf("get_file_contents: operation=%s, secrecy=%v, integrity=%v",
			operation, resource.Secrecy, resource.Integrity)
	})

	t.Run("LabelResource_list_branches_is_read", func(t *testing.T) {
		caps := difc.NewCapabilities()

		args := map[string]interface{}{
			"owner": "github",
			"repo":  "gh-aw",
		}

		resource, operation, err := guard.LabelResource(ctx, "list_branches", args, backend, caps)
		require.NoError(t, err, "LabelResource failed")
		require.NotNil(t, resource, "Expected non-nil resource")

		// list_branches is a read operation
		assert.Equal(t, difc.OperationRead, operation, "Expected list_branches to be classified as READ")

		t.Logf("list_branches: operation=%s, secrecy=%v, integrity=%v",
			operation, resource.Secrecy, resource.Integrity)
	})

	t.Run("LabelResource_create_pull_request_is_write", func(t *testing.T) {
		caps := difc.NewCapabilities()

		args := map[string]interface{}{
			"owner": "github",
			"repo":  "gh-aw",
			"head":  "feature-branch",
			"base":  "main",
			"title": "Test PR",
		}

		resource, operation, err := guard.LabelResource(ctx, "create_pull_request", args, backend, caps)
		require.NoError(t, err, "LabelResource failed")
		require.NotNil(t, resource, "Expected non-nil resource")

		// create_pull_request is a write operation
		assert.Equal(t, difc.OperationWrite, operation, "Expected create_pull_request to be classified as WRITE")

		t.Logf("create_pull_request: operation=%s, secrecy=%v, integrity=%v",
			operation, resource.Secrecy, resource.Integrity)
	})

	t.Run("LabelResource_unknown_tool", func(t *testing.T) {
		caps := difc.NewCapabilities()

		args := map[string]interface{}{}

		resource, operation, err := guard.LabelResource(ctx, "unknown_tool_xyz", args, backend, caps)
		require.NoError(t, err, "LabelResource failed")
		require.NotNil(t, resource, "Expected non-nil resource")

		// Log what the guard returns for unknown tools
		// Note: The guard may return read or write depending on implementation
		t.Logf("unknown_tool_xyz: operation=%s, secrecy=%v, integrity=%v",
			operation, resource.Secrecy, resource.Integrity)

		// Just verify we got a valid operation type
		assert.True(t, operation == difc.OperationRead || operation == difc.OperationWrite || operation == difc.OperationReadWrite,
			"Expected valid operation type")
	})
}

func TestGitHubWASMGuard_LabelResponse(t *testing.T) {
	// Skip if the WASM file doesn't exist
	if _, err := os.Stat(githubGuardWASMPath); os.IsNotExist(err) {
		t.Skipf("GitHub WASM guard not found at %s", githubGuardWASMPath)
	}

	ctx := context.Background()
	backend := &mockBackendCaller{}

	guard, err := NewWasmGuard(ctx, "github", githubGuardWASMPath, backend)
	require.NoError(t, err, "Failed to create GitHub WASM guard")
	require.NotNil(t, guard)
	defer guard.Close(ctx)

	t.Run("LabelResponse_search_results", func(t *testing.T) {
		caps := difc.NewCapabilities()

		// Simulate a search response with multiple repositories
		result := map[string]interface{}{
			"total_count": 2,
			"items": []map[string]interface{}{
				{
					"id":        1,
					"name":      "repo1",
					"full_name": "org/repo1",
					"private":   false,
				},
				{
					"id":        2,
					"name":      "repo2",
					"full_name": "org/repo2",
					"private":   true,
				},
			},
		}

		labeledData, err := guard.LabelResponse(ctx, "search_repositories", result, backend, caps)
		require.NoError(t, err, "LabelResponse failed")

		// The guard may or may not provide fine-grained labeling
		t.Logf("search_repositories response labeledData: %+v", labeledData)
	})
}

func TestGitHubWASMGuard_ConcurrentAccess(t *testing.T) {
	// Skip if the WASM file doesn't exist
	if _, err := os.Stat(githubGuardWASMPath); os.IsNotExist(err) {
		t.Skipf("GitHub WASM guard not found at %s", githubGuardWASMPath)
	}

	ctx := context.Background()
	backend := &mockBackendCaller{}

	guard, err := NewWasmGuard(ctx, "github", githubGuardWASMPath, backend)
	require.NoError(t, err, "Failed to create GitHub WASM guard")
	require.NotNil(t, guard)
	defer guard.Close(ctx)

	caps := difc.NewCapabilities()

	// Test concurrent access (WASM guards must serialize calls)
	t.Run("ConcurrentLabelResource", func(t *testing.T) {
		const numGoroutines = 10
		done := make(chan bool, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(idx int) {
				defer func() { done <- true }()

				args := map[string]interface{}{
					"query": "test query",
				}

				resource, operation, err := guard.LabelResource(ctx, "search_repositories", args, backend, caps)
				assert.NoError(t, err, "Concurrent LabelResource failed")
				assert.NotNil(t, resource)
				assert.Equal(t, difc.OperationRead, operation)
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < numGoroutines; i++ {
			<-done
		}
	})
}

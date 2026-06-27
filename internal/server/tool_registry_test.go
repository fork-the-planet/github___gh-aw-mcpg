package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/launcher"
	"github.com/github/gh-aw-mcpg/internal/mcp"
	"github.com/github/gh-aw-mcpg/internal/sanitize"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMockBackend creates a mock HTTP backend server that returns a fixed set of tools.
// toolNames is a list of tool names to expose; if empty, the backend exposes no tools.
func newMockBackend(t *testing.T, serverName string, toolNames []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		method, _ := req["method"].(string)
		switch method {
		case "initialize":
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]interface{}{},
					"serverInfo":      map[string]interface{}{"name": serverName, "version": "1.0"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		case "tools/list":
			tools := make([]map[string]interface{}, 0, len(toolNames))
			for _, name := range toolNames {
				tools = append(tools, map[string]interface{}{
					"name":        name,
					"description": "Tool " + name,
					"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
				})
			}
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]interface{}{"tools": tools},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}
	}))
}

func newBackendListTestConnection(t *testing.T, serverID string, backend http.HandlerFunc) (func(), *mcp.Connection) {
	t.Helper()

	server := httptest.NewServer(backend)

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			serverID: {
				Type: "http",
				URL:  server.URL,
			},
		},
	}

	l := launcher.New(context.Background(), cfg)

	conn, err := launcher.GetOrLaunch(l, serverID)
	require.NoError(t, err)

	return func() {
		l.Close()
		server.Close()
	}, conn
}

func failOnUnexpectedRequestError(err error) error {
	return fmt.Errorf("unexpected request error: %w", err)
}

func failOnUnexpectedResponseError(code int, message string) error {
	return fmt.Errorf("unexpected response error: code=%d message=%s", code, message)
}

func failOnUnexpectedParseError(err error) error {
	return fmt.Errorf("unexpected parse error: %w", err)
}

func TestFetchBackendList_Success(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	cleanup, conn := newBackendListTestConnection(t, "test-backend", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch req["method"] {
		case "initialize":
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]interface{}{},
					"serverInfo": map[string]interface{}{
						"name":    "test-backend",
						"version": "1.0.0",
					},
				},
			}); err != nil {
				t.Errorf("encode initialize response: %v", err)
				http.Error(w, "encode initialize response: "+err.Error(), http.StatusInternalServerError)
				return
			}
		case "tools/list":
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"tools": []map[string]interface{}{
						{"name": "read_file"},
					},
				},
			}); err != nil {
				t.Errorf("encode tools response: %v", err)
				http.Error(w, "encode tools response: "+err.Error(), http.StatusInternalServerError)
				return
			}
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	defer cleanup()

	var listResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}

	err := fetchBackendList(
		context.Background(),
		conn,
		"test-backend",
		"tools/list",
		&listResult,
		failOnUnexpectedRequestError,
		failOnUnexpectedResponseError,
		failOnUnexpectedParseError,
	)
	require.NoError(err)
	require.Len(listResult.Tools, 1)
	assert.Equal("read_file", listResult.Tools[0].Name)
}

func TestFetchBackendList_BackendErrorCanGracefullySkip(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	cleanup, conn := newBackendListTestConnection(t, "prompt-backend", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch req["method"] {
		case "initialize":
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]interface{}{},
					"serverInfo": map[string]interface{}{
						"name":    "prompt-backend",
						"version": "1.0.0",
					},
				},
			}); err != nil {
				t.Errorf("encode initialize response: %v", err)
				http.Error(w, "encode initialize response: "+err.Error(), http.StatusInternalServerError)
				return
			}
		case "prompts/list":
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"error": map[string]interface{}{
					"code":    -32601,
					"message": "method not found",
				},
			}); err != nil {
				t.Fatalf("encode prompts response: %v", err)
			}
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	defer cleanup()

	var listResult struct {
		Prompts []struct {
			Name string `json:"name"`
		} `json:"prompts"`
	}

	var handledCode int
	var handledMessage string
	err := fetchBackendList(
		context.Background(),
		conn,
		"prompt-backend",
		"prompts/list",
		&listResult,
		failOnUnexpectedRequestError,
		func(code int, message string) error {
			handledCode = code
			handledMessage = message
			return nil
		},
		failOnUnexpectedParseError,
	)
	require.NoError(err)
	assert.Equal(-32601, handledCode)
	assert.Equal("method not found", handledMessage)
	assert.Empty(listResult.Prompts)
}

func TestFetchBackendList_RequestErrorCanGracefullySkip(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	serverID := "request-error-backend"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if req["method"] == "initialize" {
			require.NoError(json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]interface{}{},
					"serverInfo": map[string]interface{}{
						"name":    serverID,
						"version": "1.0.0",
					},
				},
			}))
			return
		}

		w.WriteHeader(http.StatusBadRequest)
	}))

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			serverID: {
				Type: "http",
				URL:  backend.URL,
			},
		},
	}

	l := launcher.New(context.Background(), cfg)
	conn, err := launcher.GetOrLaunch(l, serverID)
	require.NoError(err)

	backend.Close()
	defer l.Close()

	var listResult struct {
		Prompts []struct {
			Name string `json:"name"`
		} `json:"prompts"`
	}

	handledRequestError := false
	err = fetchBackendList(
		context.Background(),
		conn,
		serverID,
		"prompts/list",
		&listResult,
		func(err error) error {
			handledRequestError = true
			return nil
		},
		failOnUnexpectedResponseError,
		failOnUnexpectedParseError,
	)
	require.NoError(err)
	assert.True(handledRequestError)
	assert.Empty(listResult.Prompts)
}

func TestFetchBackendList_ParseError(t *testing.T) {
	require := require.New(t)

	cleanup, conn := newBackendListTestConnection(t, "invalid-backend", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch req["method"] {
		case "initialize":
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]interface{}{},
					"serverInfo": map[string]interface{}{
						"name":    "invalid-backend",
						"version": "1.0.0",
					},
				},
			}); err != nil {
				t.Fatalf("encode initialize response: %v", err)
			}
		case "tools/list":
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  "not a tool list",
			}); err != nil {
				t.Fatalf("encode tools response: %v", err)
			}
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	defer cleanup()

	var listResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}

	err := fetchBackendList(
		context.Background(),
		conn,
		"invalid-backend",
		"tools/list",
		&listResult,
		failOnUnexpectedRequestError,
		failOnUnexpectedResponseError,
		func(err error) error {
			return err
		},
	)
	require.Error(err)
	require.ErrorContains(err, "cannot unmarshal")
}

// TestRegisterAllTools_NoDIFC_Parallel verifies that with DIFC disabled and parallel mode,
// tools from all backends are registered and sys tools are NOT registered.
func TestRegisterAllTools_NoDIFC_Parallel(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	backend := newMockBackend(t, "alpha", []string{"do_thing"})
	defer backend.Close()

	cfg := &config.Config{
		SequentialLaunch: false, // parallel
		Servers: map[string]*config.ServerConfig{
			"alpha": {Type: "http", URL: backend.URL},
		},
		// No GuardPolicy → enableDIFC stays false
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	// Verify DIFC is disabled
	assert.False(us.enableDIFC, "DIFC should be disabled")
	assert.False(us.sequentialLaunch, "should use parallel mode")

	// Backend tool should be registered
	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()
	assert.Contains(us.tools, "alpha___do_thing", "backend tool should be registered")

	// Sys tools should NOT be registered when DIFC is disabled
	assert.NotContains(us.tools, "sys___init", "sys___init should not be registered without DIFC")
	assert.NotContains(us.tools, "sys___list_servers", "sys___list_servers should not be registered without DIFC")
}

// TestRegisterAllTools_DIFC_Parallel verifies that with DIFC enabled and parallel mode,
// both backend tools and sys tools are registered.
func TestRegisterAllTools_DIFC_Parallel(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	backend := newMockBackend(t, "beta", []string{"query"})
	defer backend.Close()

	cfg := &config.Config{
		SequentialLaunch: false,
		Servers: map[string]*config.ServerConfig{
			"beta": {Type: "http", URL: backend.URL},
		},
		// Setting GuardPolicy to non-nil causes enableDIFC = true in NewUnified
		GuardPolicy: &config.GuardPolicy{
			AllowOnly: &config.AllowOnlyPolicy{
				Repos:        "public",
				MinIntegrity: "none",
			},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	// Verify DIFC is enabled
	assert.True(us.enableDIFC, "DIFC should be enabled")

	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()

	// Sys tools SHOULD be registered when DIFC is enabled
	assert.Contains(us.tools, "sys___init", "sys___init should be registered with DIFC enabled")
	assert.Contains(us.tools, "sys___list_servers", "sys___list_servers should be registered with DIFC enabled")

	// Backend tool should also be registered
	assert.Contains(us.tools, "beta___query", "backend tool should be registered")
}

// TestRegisterAllTools_NoDIFC_Sequential verifies that sequential mode registers tools
// without sys tools when DIFC is disabled.
func TestRegisterAllTools_NoDIFC_Sequential(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	backend := newMockBackend(t, "gamma", []string{"run"})
	defer backend.Close()

	cfg := &config.Config{
		SequentialLaunch: true, // sequential
		Servers: map[string]*config.ServerConfig{
			"gamma": {Type: "http", URL: backend.URL},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	assert.True(us.sequentialLaunch, "should use sequential mode")
	assert.False(us.enableDIFC, "DIFC should be disabled")

	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()

	assert.Contains(us.tools, "gamma___run")
	assert.NotContains(us.tools, "sys___init")
}

// TestRegisterAllTools_DIFC_Sequential verifies that sequential mode with DIFC enabled
// registers both backend tools and sys tools.
func TestRegisterAllTools_DIFC_Sequential(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	backend := newMockBackend(t, "delta", []string{"fetch"})
	defer backend.Close()

	cfg := &config.Config{
		SequentialLaunch: true,
		Servers: map[string]*config.ServerConfig{
			"delta": {Type: "http", URL: backend.URL},
		},
		GuardPolicy: &config.GuardPolicy{
			AllowOnly: &config.AllowOnlyPolicy{
				Repos:        "public",
				MinIntegrity: "none",
			},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	assert.True(us.sequentialLaunch)
	assert.True(us.enableDIFC)

	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()

	assert.Contains(us.tools, "sys___init")
	assert.Contains(us.tools, "sys___list_servers")
	assert.Contains(us.tools, "delta___fetch")
}

// TestRegisterAllToolsSequential_MultipleBackends verifies sequential registration
// across multiple backends.
func TestRegisterAllToolsSequential_MultipleBackends(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	b1 := newMockBackend(t, "svc1", []string{"tool_a", "tool_b"})
	defer b1.Close()
	b2 := newMockBackend(t, "svc2", []string{"tool_c"})
	defer b2.Close()

	cfg := &config.Config{
		SequentialLaunch: true,
		Servers: map[string]*config.ServerConfig{
			"svc1": {Type: "http", URL: b1.URL},
			"svc2": {Type: "http", URL: b2.URL},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()

	assert.Contains(us.tools, "svc1___tool_a")
	assert.Contains(us.tools, "svc1___tool_b")
	assert.Contains(us.tools, "svc2___tool_c")
}

// TestRegisterAllToolsSequential_ContinuesOnFailure verifies that sequential registration
// continues processing backends even when one fails.
func TestRegisterAllToolsSequential_ContinuesOnFailure(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	goodBackend := newMockBackend(t, "good", []string{"tool_good"})
	defer goodBackend.Close()

	cfg := &config.Config{
		SequentialLaunch: true,
		Servers: map[string]*config.ServerConfig{
			"bad":  {Type: "http", URL: "http://127.0.0.1:1"}, // unreachable
			"good": {Type: "http", URL: goodBackend.URL},
		},
	}

	// NewUnified calls registerAllToolsSequential; it should not error even if one backend fails.
	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err, "registerAllToolsSequential must not return an error on partial failure")
	defer us.Close()

	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()

	// The good backend's tool should be registered despite the bad backend failing
	assert.Contains(us.tools, "good___tool_good", "good backend tools should still be registered")
}

// TestRegisterAllToolsParallel_MultipleBackends verifies parallel registration
// across multiple backends concurrently.
func TestRegisterAllToolsParallel_MultipleBackends(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	b1 := newMockBackend(t, "pa", []string{"pa_tool1", "pa_tool2"})
	defer b1.Close()
	b2 := newMockBackend(t, "pb", []string{"pb_tool1"})
	defer b2.Close()
	b3 := newMockBackend(t, "pc", []string{"pc_tool1", "pc_tool2", "pc_tool3"})
	defer b3.Close()

	cfg := &config.Config{
		SequentialLaunch: false, // parallel
		Servers: map[string]*config.ServerConfig{
			"pa": {Type: "http", URL: b1.URL},
			"pb": {Type: "http", URL: b2.URL},
			"pc": {Type: "http", URL: b3.URL},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()

	assert.Contains(us.tools, "pa___pa_tool1")
	assert.Contains(us.tools, "pa___pa_tool2")
	assert.Contains(us.tools, "pb___pb_tool1")
	assert.Contains(us.tools, "pc___pc_tool1")
	assert.Contains(us.tools, "pc___pc_tool2")
	assert.Contains(us.tools, "pc___pc_tool3")
}

// TestRegisterAllToolsParallel_ContinuesOnFailure verifies that parallel registration
// continues processing other backends when one fails.
func TestRegisterAllToolsParallel_ContinuesOnFailure(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	goodBackend := newMockBackend(t, "ok", []string{"ok_tool"})
	defer goodBackend.Close()

	cfg := &config.Config{
		SequentialLaunch: false,
		Servers: map[string]*config.ServerConfig{
			"broken": {Type: "http", URL: "http://127.0.0.1:1"},
			"ok":     {Type: "http", URL: goodBackend.URL},
		},
	}

	// NewUnified calls registerAllToolsParallel; it must succeed even if one backend fails.
	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err, "registerAllToolsParallel must not return an error on partial failure")
	defer us.Close()

	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()

	assert.Contains(us.tools, "ok___ok_tool", "successful backend tools should be registered")
}

// TestRegisterAllToolsParallel_AllFail verifies that parallel registration returns nil
// even when all backends fail.
func TestRegisterAllToolsParallel_AllFail(t *testing.T) {
	require := require.New(t)

	cfg := &config.Config{
		SequentialLaunch: false,
		Servers: map[string]*config.ServerConfig{
			"bad1": {Type: "http", URL: "http://127.0.0.1:1"},
			"bad2": {Type: "http", URL: "http://127.0.0.1:2"},
		},
	}

	// Should not return an error; failures are logged as warnings.
	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err, "registerAllToolsParallel should return nil even when all backends fail")
	if us != nil {
		us.Close()
	}
}

// TestRegisterAllToolsSequential_AllFail verifies that sequential registration returns
// nil even when all backends fail.
func TestRegisterAllToolsSequential_AllFail(t *testing.T) {
	require := require.New(t)

	cfg := &config.Config{
		SequentialLaunch: true,
		Servers: map[string]*config.ServerConfig{
			"bad1": {Type: "http", URL: "http://127.0.0.1:1"},
			"bad2": {Type: "http", URL: "http://127.0.0.1:2"},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err, "registerAllToolsSequential should return nil even when all backends fail")
	if us != nil {
		us.Close()
	}
}

// TestRegisterAllTools_EmptyServerList verifies that registration with no backends
// succeeds immediately without errors.
func TestRegisterAllTools_EmptyServerList(t *testing.T) {
	for _, sequential := range []bool{true, false} {
		name := "parallel"
		if sequential {
			name = "sequential"
		}
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			cfg := &config.Config{
				SequentialLaunch: sequential,
				Servers:          map[string]*config.ServerConfig{},
			}

			us, err := NewUnified(context.Background(), cfg)
			require.NoError(err)
			defer us.Close()

			us.toolsMu.RLock()
			toolCount := len(us.tools)
			us.toolsMu.RUnlock()

			assert.Equal(0, toolCount, "no tools should be registered with empty server list")
		})
	}
}

// TestRegisterSysTool_StoresInternallyOnly verifies that registerSysTool stores tool
// metadata internally but does NOT register the tool with the MCP SDK server
// (sys tools should not appear in tools/list).
func TestRegisterSysTool_StoresInternallyOnly(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{},
		GuardPolicy: &config.GuardPolicy{
			AllowOnly: &config.AllowOnlyPolicy{
				Repos:        "public",
				MinIntegrity: "none",
			},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	// Sys tools are stored in us.tools but NOT registered with the SDK server.
	// The registerSysTools function calls registerSysTool which only updates us.tools.
	us.toolsMu.RLock()
	sysInit := us.tools["sys___init"]
	sysList := us.tools["sys___list_servers"]
	us.toolsMu.RUnlock()

	require.NotNil(sysInit, "sys___init should be in internal tools map")
	assert.Equal("sys___init", sysInit.Name)
	assert.Equal("sys", sysInit.BackendID)
	assert.NotNil(sysInit.Handler, "sys___init should have a handler")

	require.NotNil(sysList, "sys___list_servers should be in internal tools map")
	assert.Equal("sys___list_servers", sysList.Name)
	assert.Equal("sys", sysList.BackendID)
	assert.NotNil(sysList.Handler, "sys___list_servers should have a handler")
}

// TestCallSysServer_SysInit verifies that callSysServer delegates correctly to the
// internal SysServer for the sys_init tool.
func TestCallSysServer_SysInit(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"alpha": {Type: "http", URL: "http://127.0.0.1:1"}, // unreachable is fine for this test
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	// callSysServer("sys_init") calls us.sysServer.HandleRequest
	result, err := us.callSysServer("sys_init")
	require.NoError(err, "callSysServer(sys_init) should succeed")
	assert.NotNil(result, "sys_init should return a non-nil result")
}

// TestCallSysServer_SysListServers verifies that callSysServer delegates correctly for
// the sys_list_servers tool.
func TestCallSysServer_SysListServers(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"server1": {Type: "http", URL: "http://127.0.0.1:1"},
			"server2": {Type: "http", URL: "http://127.0.0.1:1"},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	result, err := us.callSysServer("sys_list_servers")
	require.NoError(err, "callSysServer(sys_list_servers) should succeed")
	assert.NotNil(result, "sys_list_servers should return a non-nil result")
}

// TestCallSysServer_UnknownTool verifies that callSysServer returns an error for
// unknown tool names.
func TestCallSysServer_UnknownTool(t *testing.T) {
	require := require.New(t)

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	_, err = us.callSysServer("nonexistent_tool")
	require.Error(err, "callSysServer with unknown tool should return an error")
}

func TestSanitizeMarshalAndSanitize_RedactsSecrets(t *testing.T) {
	assert := assert.New(t)

	const secret = "ghp_1234567890123456789012345678901234567890"
	sanitized := sanitize.MarshalAndSanitize(map[string]interface{}{
		"token": secret,
	})

	assert.Contains(sanitized, "[REDACTED]")
	assert.NotContains(sanitized, secret)
}

func TestCallAndLogSysTool_UnknownToolReturnsErrorResult(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	result, data, callErr := us.callAndLogSysTool("session-id", "sys test", "nonexistent_tool")
	require.Error(callErr)
	require.NotNil(result)
	assert.Nil(data)
	assert.True(result.IsError)
}

// TestRegisterAllToolsParallel_EmptyList verifies that parallel registration with no
// servers does not block and returns immediately.
func TestRegisterAllToolsParallel_EmptyList(t *testing.T) {
	require := require.New(t)

	cfg := &config.Config{
		SequentialLaunch: false,
		Servers:          map[string]*config.ServerConfig{},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err, "parallel registration with no servers should succeed")
	defer us.Close()
}

// TestRegisterAllToolsSequential_SingleBackend verifies sequential registration
// with a single backend succeeds and registers tools.
func TestRegisterAllToolsSequential_SingleBackend(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	backend := newMockBackend(t, "solo", []string{"solo_tool"})
	defer backend.Close()

	cfg := &config.Config{
		SequentialLaunch: true,
		Servers: map[string]*config.ServerConfig{
			"solo": {Type: "http", URL: backend.URL},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()
	assert.Contains(us.tools, "solo___solo_tool")
}

// TestRegisterToolsFromBackend_FiltersAllowedTools verifies that when an allowed-tools
// list is configured, only those tools are registered (defense-in-depth for tools/list).
func TestRegisterToolsFromBackend_FiltersAllowedTools(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	backend := newMockBackend(t, "github", []string{"search_code", "get_file_contents", "delete_repo"})
	defer backend.Close()

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type:  "http",
				URL:   backend.URL,
				Tools: []string{"search_code", "get_file_contents"}, // delete_repo is not allowed
			},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()

	assert.Contains(us.tools, "github___search_code", "search_code should be registered")
	assert.Contains(us.tools, "github___get_file_contents", "get_file_contents should be registered")
	assert.NotContains(us.tools, "github___delete_repo", "delete_repo must NOT be registered when not in allowed list")
}

// TestRegisterToolsFromBackend_EmptyAllowedList registers all tools when no list is set.
func TestRegisterToolsFromBackend_EmptyAllowedList(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	backend := newMockBackend(t, "s", []string{"tool_a", "tool_b", "tool_c"})
	defer backend.Close()

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"s": {Type: "http", URL: backend.URL}, // no Tools filter
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()

	assert.Contains(us.tools, "s___tool_a")
	assert.Contains(us.tools, "s___tool_b")
	assert.Contains(us.tools, "s___tool_c")
}

// TestSchemaBypassCanary is a canary test for SDK upgrades.
//
// The gateway relies on Server.AddTool (instance method) to register backend tools
// WITHOUT full JSON Schema validation, because backends may emit schemas using
// draft-07 features (e.g., "$ref", "definitions", "if/then/else") that the SDK's
// stricter package-level AddTool function would reject.
//
// This test verifies that Server.AddTool still accepts such schemas. If it panics
// or rejects them after an SDK upgrade, the gateway's tool registration will break
// and this test serves as the early warning.
//
// See also: registerToolWithoutValidation in tool_registry.go.
func TestSchemaBypassCanary(t *testing.T) {
	assert := assert.New(t)

	server := sdk.NewServer(&sdk.Implementation{Name: "canary", Version: "1.0"}, &sdk.ServerOptions{})
	noop := func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{}, nil
	}

	// Draft-07 schema with $ref and definitions — valid JSON Schema but uses
	// features beyond what the SDK's stricter path validates.
	draft07Schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"repo": map[string]interface{}{
				"$ref": "#/definitions/repoName",
			},
		},
		"definitions": map[string]interface{}{
			"repoName": map[string]interface{}{
				"type":      "string",
				"minLength": 1,
			},
		},
	}

	// Server.AddTool (instance method) must not panic — this is the code path
	// that registerToolWithoutValidation uses.
	assert.NotPanics(func() {
		server.AddTool(&sdk.Tool{
			Name:        "draft07_tool",
			Description: "Tool with draft-07 schema features",
			InputSchema: draft07Schema,
		}, noop)
	}, "Server.AddTool must accept draft-07 schemas; if this fails after an SDK upgrade, "+
		"registerToolWithoutValidation needs to be updated")

	// Schema with additionalProperties and patternProperties — another draft-07
	// feature set that backends commonly use.
	extendedSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
		"additionalProperties": false,
		"patternProperties": map[string]interface{}{
			"^x-": map[string]interface{}{"type": "string"},
		},
	}

	assert.NotPanics(func() {
		server.AddTool(&sdk.Tool{
			Name:        "extended_schema_tool",
			Description: "Tool with extended schema features",
			InputSchema: extendedSchema,
		}, noop)
	}, "Server.AddTool must accept schemas with patternProperties/additionalProperties")
}

// TestArgumentValidationBypassCanary is a canary test for SDK upgrades.
//
// The gateway uses Server.AddTool (instance method) rather than the package-level
// sdk.AddTool[In,Out] function because the instance method does NOT validate tool
// argument values against the input schema at call time. This allows the gateway to
// forward arbitrary backend tool calls without the SDK rejecting them for schema
// violations.
//
// This test verifies that Server.AddTool still bypasses argument-value validation.
// If it starts failing after an SDK upgrade, registerToolWithoutValidation needs to
// be re-evaluated — it may need a different registration path or schema normalisation.
//
// See also: TestSchemaBypassCanary, registerToolWithoutValidation in tool_registry.go.
func TestArgumentValidationBypassCanary(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	server := sdk.NewServer(&sdk.Implementation{Name: "canary", Version: "1.0"}, &sdk.ServerOptions{})

	var handlerCalled atomic.Bool
	noop := func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		handlerCalled.Store(true)
		return &sdk.CallToolResult{}, nil
	}

	// Register a tool with a strict schema requiring "count" to be an integer.
	// The gateway relies on the fact that the SDK does NOT reject calls with
	// schema-invalid arguments on this code path.
	server.AddTool(&sdk.Tool{
		Name:        "strict_tool",
		Description: "Tool requiring integer count argument",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"count": map[string]interface{}{"type": "integer"},
			},
			"required": []interface{}{"count"},
		},
	}, noop)

	// Connect a client via in-memory transport and call the tool with an
	// intentionally invalid argument value (string instead of integer).
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = server.Run(ctx, serverTransport)
	}()

	client := sdk.NewClient(&sdk.Implementation{Name: "canary-client", Version: "1.0"}, &sdk.ClientOptions{})
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(err)
	defer clientSession.Close()

	// Pass "count" as a string instead of an integer — violates the declared schema.
	// If the SDK starts validating argument values on this code path after an upgrade,
	// this call will fail and registerToolWithoutValidation needs to be updated.
	result, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name:      "strict_tool",
		Arguments: map[string]interface{}{"count": "not-an-integer"},
	})
	require.NoError(err,
		"Server.AddTool must not validate argument values; if this fails after an SDK upgrade, "+
			"registerToolWithoutValidation needs to be updated")
	assert.False(result.IsError)
	assert.True(handlerCalled.Load(), "Handler must be called even when arguments violate the schema")
}

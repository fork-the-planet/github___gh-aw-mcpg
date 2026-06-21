package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newStreamableMCPTestServer creates a minimal httptest.Server that handles the MCP
// streamable protocol. It delegates each POST request to the provided handler,
// which receives the parsed request body map and should write a JSON-RPC response.
//
// The handler must write both the Content-Type header and body when responding with
// a JSON-RPC result. It may also write a session ID via w.Header().Set("Mcp-Session-Id", ...).
//
// Non-POST requests and empty-body POSTs are rejected so that SDK probe requests
// (GET for SSE stream) fail fast.
func newStreamableMCPTestServer(t *testing.T, handler func(w http.ResponseWriter, method string, req map[string]interface{})) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		method, _ := req["method"].(string)
		handler(w, method, req)
	}))
}

// writeInitializeResponse writes a successful MCP initialize JSON-RPC response,
// setting the Mcp-Session-Id header to the provided sessionID.
func writeInitializeResponse(w http.ResponseWriter, sessionID string, req map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Mcp-Session-Id", sessionID)
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"jsonrpc": "2.0",
		"id":      req["id"],
		"result": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]interface{}{"name": "test-mcp-server", "version": "1.0.0"},
			"capabilities":    map[string]interface{}{},
		},
	})
}

// newStreamableConn is a test helper that creates a real streamable HTTP Connection
// connected to the given server URL. It stores the connected connection in conn and
// will clean up the connection at the end of the test.
func newStreamableConn(t *testing.T, serverURL string) *Connection {
	t.Helper()
	conn, err := NewHTTPConnection(
		context.Background(),
		"test-server",
		serverURL,
		nil,
		nil,
		"",
		0,
		5*time.Second,
	)
	require.NoError(t, err)
	require.NotNil(t, conn)
	t.Cleanup(func() { conn.Close() })
	return conn
}

// TestReconnectSDKTransport_UnsupportedTransportType verifies that reconnectSDKTransport
// returns an immediate error when the connection has an unrecognised transport type,
// exercising the default branch without any network calls.
func TestReconnectSDKTransport_UnsupportedTransportType(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn := &Connection{
		ctx:               ctx,
		cancel:            cancel,
		serverID:          "test-server",
		isHTTP:            true,
		httpTransportType: HTTPTransportType("unsupported-type"),
		httpClient:        &http.Client{},
	}

	err := conn.reconnectSDKTransport()

	require.Error(t, err)
	assert.ErrorContains(t, err, "cannot reconnect")
	assert.ErrorContains(t, err, "unsupported transport type")
	assert.ErrorContains(t, err, "unsupported-type")
}

// TestReconnectSDKTransport_EmptyTransportType verifies the default branch with an
// empty transport type string (zero value for HTTPTransportType).
func TestReconnectSDKTransport_EmptyTransportType(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn := &Connection{
		ctx:               ctx,
		cancel:            cancel,
		serverID:          "test-server",
		isHTTP:            true,
		httpTransportType: HTTPTransportType(""),
		httpClient:        &http.Client{},
	}

	err := conn.reconnectSDKTransport()

	require.Error(t, err)
	assert.ErrorContains(t, err, "cannot reconnect: unsupported transport type")
}

// TestReconnectSDKTransport_StreamableConnectFail verifies that reconnectSDKTransport
// propagates the SDK connect error when the HTTP server is unreachable.
func TestReconnectSDKTransport_StreamableConnectFail(t *testing.T) {
	// Create and immediately close a server to get a URL that refuses connections.
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	badURL := closed.URL
	closed.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn := &Connection{
		ctx:               ctx,
		cancel:            cancel,
		serverID:          "test-server",
		isHTTP:            true,
		httpURL:           badURL,
		httpTransportType: HTTPTransportStreamable,
		httpClient:        &http.Client{Timeout: 2 * time.Second},
		connectTimeout:    2 * time.Second,
	}

	err := conn.reconnectSDKTransport()

	require.Error(t, err)
	assert.ErrorContains(t, err, "session reconnect failed")
}

// TestCallSDKMethodWithReconnect_NoReconnectOnNonSessionError verifies that when
// callSDKMethod returns an error that is not a session-not-found error,
// callSDKMethodWithReconnect returns it directly without attempting reconnection.
func TestCallSDKMethodWithReconnect_NoReconnectOnNonSessionError(t *testing.T) {
	var initCount atomic.Int32

	// Start a simple server to create a real streamable connection.
	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			initCount.Add(1)
			writeInitializeResponse(w, "session-1", req)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			// Return a non-session error for a supported SDK method call.
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      req["id"],
				"error":   map[string]interface{}{"code": -32603, "message": "internal server error"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)
	require.Equal(t, HTTPTransportStreamable, conn.httpTransportType)
	require.Equal(t, int32(1), initCount.Load(), "expected one initialize during initial connect")

	result, err := conn.callSDKMethodWithReconnect("tools/list", nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "internal server error")
	// Verify the error is NOT a session-not-found error (reconnect should not have been attempted).
	assert.False(t, isSessionNotFoundError(err))
	assert.Equal(t, int32(1), initCount.Load(), "reconnect should not have been attempted")
}

// TestCallSDKMethodWithReconnect_SessionNotFound_ReconnectFails verifies the full
// session-expiry + reconnect-failure code path:
//
//  1. First callSDKMethod → server returns 404 (session not found) → sdk.ErrSessionMissing
//  2. reconnectSDKTransport is called → immediately fails (unsupported transport type)
//  3. callSDKMethodWithReconnect returns the original session-not-found error
func TestCallSDKMethodWithReconnect_SessionNotFound_ReconnectFails(t *testing.T) {
	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		if method == "initialize" {
			writeInitializeResponse(w, "session-1", req)
			return
		}
		if method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// All other requests: simulate expired session.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"session not found"}`) //nolint:errcheck
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)
	require.Equal(t, HTTPTransportStreamable, conn.httpTransportType)

	// Override transport type to an unsupported value so that reconnectSDKTransport
	// returns an error immediately without making network calls.
	conn.httpTransportType = HTTPTransportType("none")

	result, err := conn.callSDKMethodWithReconnect("tools/list", nil)

	require.Error(t, err)
	assert.Nil(t, result)
	// The original session-not-found error should be returned (sdk.ErrSessionMissing).
	assert.True(t, isSessionNotFoundError(err),
		"expected session-not-found error but got: %v", err)
}

// TestCallSDKMethodWithReconnect_SessionNotFound_ReconnectSucceeds verifies the full
// happy reconnect path:
//
//  1. First callSDKMethod → server returns 404 (session not found)
//  2. reconnectSDKTransport → connects successfully with a new session
//  3. Retry callSDKMethod → succeeds, returning an empty tools list
func TestCallSDKMethodWithReconnect_SessionNotFound_ReconnectSucceeds(t *testing.T) {
	// State machine:
	//   initCount tracks how many initialize requests have been served.
	//   The first tools/list call with session-1 returns 404.
	//   After reconnect a new session-2 is issued.
	//   The second tools/list call with session-2 returns an empty tools list.
	var initCount atomic.Int32

	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			count := initCount.Add(1)
			sessionID := fmt.Sprintf("session-%d", count)
			writeInitializeResponse(w, sessionID, req)

		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)

		case "tools/list":
			// Determine which session this request belongs to.
			// initCount == 1 means only the first initialize has run → this is session-1 → expired.
			// initCount >= 2 means reconnect has happened → this is session-2 → return empty list.
			if initCount.Load() < 2 {
				// Session-1 has expired.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":"session not found"}`) //nolint:errcheck
			} else {
				// Session-2 is fresh.
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
					"jsonrpc": "2.0",
					"id":      req["id"],
					"result":  map[string]interface{}{"tools": []interface{}{}},
				})
			}

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)
	require.Equal(t, HTTPTransportStreamable, conn.httpTransportType)
	require.Equal(t, int32(1), initCount.Load(), "should have had exactly one initialize during NewHTTPConnection")

	result, err := conn.callSDKMethodWithReconnect("tools/list", nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int32(2), initCount.Load(), "should have reconnected with a second initialize")

	// Verify the response contains an empty tools list.
	var body struct {
		Tools []interface{} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &body))
	assert.Empty(t, body.Tools)
}

// TestCallSDKMethodWithReconnect_Success_NoReconnect verifies that when the first
// callSDKMethod call succeeds, callSDKMethodWithReconnect returns the result without
// attempting a reconnect (the reconnect branch is not taken).
func TestCallSDKMethodWithReconnect_Success_NoReconnect(t *testing.T) {
	var initCount atomic.Int32

	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			initCount.Add(1)
			writeInitializeResponse(w, "session-1", req)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]interface{}{"tools": []interface{}{}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)
	require.Equal(t, int32(1), initCount.Load(), "expected one initialize during initial connect")

	result, err := conn.callSDKMethodWithReconnect("tools/list", nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int32(1), initCount.Load(), "reconnect should not have been attempted")

	var body struct {
		Tools []interface{} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &body))
	assert.Empty(t, body.Tools)
}

// TestListResources_WithSession verifies that listResources succeeds when a real SDK
// session is available and the server returns a resources list.
func TestListResources_WithSession(t *testing.T) {
	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			writeInitializeResponse(w, "session-1", req)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "resources/list":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"resources": []interface{}{
						map[string]interface{}{
							"uri":      "file:///test.txt",
							"name":     "test.txt",
							"mimeType": "text/plain",
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)

	result, err := conn.callSDKMethod("resources/list", nil)

	require.NoError(t, err)
	require.NotNil(t, result)

	var body struct {
		Resources []struct {
			URI  string `json:"uri"`
			Name string `json:"name"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &body))
	require.Len(t, body.Resources, 1)
	assert.Equal(t, "file:///test.txt", body.Resources[0].URI)
	assert.Equal(t, "test.txt", body.Resources[0].Name)
}

// TestListPrompts_WithSession verifies that listPrompts succeeds when a real SDK
// session is available and the server returns a prompts list.
func TestListPrompts_WithSession(t *testing.T) {
	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			writeInitializeResponse(w, "session-1", req)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "prompts/list":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"prompts": []interface{}{
						map[string]interface{}{
							"name":        "summarise",
							"description": "Summarise a document",
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)

	result, err := conn.callSDKMethod("prompts/list", nil)

	require.NoError(t, err)
	require.NotNil(t, result)

	var body struct {
		Prompts []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"prompts"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &body))
	require.Len(t, body.Prompts, 1)
	assert.Equal(t, "summarise", body.Prompts[0].Name)
}

// TestReadResource_WithSession verifies that readResource propagates server errors
// through a real SDK session.
func TestReadResource_WithSession(t *testing.T) {
	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			writeInitializeResponse(w, "session-1", req)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "resources/read":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      req["id"],
				"error":   map[string]interface{}{"code": -32602, "message": "invalid resource uri"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)

	result, err := conn.callSDKMethod("resources/read", map[string]interface{}{"uri": "file:///test.txt"})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "invalid resource uri")
}

// TestGetPrompt_WithSession verifies that getPrompt propagates server errors
// through a real SDK session.
func TestGetPrompt_WithSession(t *testing.T) {
	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			writeInitializeResponse(w, "session-1", req)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "prompts/get":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      req["id"],
				"error":   map[string]interface{}{"code": -32602, "message": "prompt not found"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)

	result, err := conn.callSDKMethod("prompts/get", map[string]interface{}{
		"name":      "summarise",
		"arguments": map[string]string{},
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "prompt not found")
}

// TestServerInfo_WithSession verifies that ServerInfo returns the server name and version
// that were exchanged during the initialize handshake.
func TestServerInfo_WithSession(t *testing.T) {
	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "session-1")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{
						"name":    "my-mcp-backend",
						"version": "2.3.4",
					},
					"capabilities": map[string]interface{}{},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn, err := NewHTTPConnection(
		context.Background(),
		"test-server",
		srv.URL,
		nil, nil, "",
		0, 5*time.Second,
	)
	require.NoError(t, err)
	defer conn.Close()

	name, version := conn.ServerInfo()

	assert.Equal(t, "my-mcp-backend", name)
	assert.Equal(t, "2.3.4", version)
}

// TestServerInfo_NoSession verifies that ServerInfo returns empty strings when no SDK session
// is available (e.g. for a plain JSON-RPC connection that never established a session).
func TestServerInfo_NoSession(t *testing.T) {
	conn := newTestConnection(t)
	name, version := conn.ServerInfo()
	assert.Empty(t, name)
	assert.Empty(t, version)
}

// TestListResources_Empty verifies that listResources handles an empty resource list
// gracefully and returns a valid marshalled response.
func TestListResources_Empty(t *testing.T) {
	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			writeInitializeResponse(w, "session-1", req)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "resources/list":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]interface{}{"resources": []interface{}{}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)
	result, err := conn.callSDKMethod("resources/list", nil)

	require.NoError(t, err)
	require.NotNil(t, result)

	var body struct {
		Resources []interface{} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &body))
	assert.Empty(t, body.Resources)
}

// TestListPrompts_Empty verifies that listPrompts handles an empty prompt list.
func TestListPrompts_Empty(t *testing.T) {
	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			writeInitializeResponse(w, "session-1", req)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "prompts/list":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]interface{}{"prompts": []interface{}{}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)
	result, err := conn.callSDKMethod("prompts/list", nil)

	require.NoError(t, err)
	require.NotNil(t, result)

	var body struct {
		Prompts []interface{} `json:"prompts"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &body))
	assert.Empty(t, body.Prompts)
}

// TestReconnectSDKTransport_SSEConnectFail verifies that reconnectSDKTransport
// propagates a connect error for the SSE transport type.
func TestReconnectSDKTransport_SSEConnectFail(t *testing.T) {
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	badURL := closed.URL
	closed.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn := &Connection{
		ctx:               ctx,
		cancel:            cancel,
		serverID:          "test-server",
		isHTTP:            true,
		httpURL:           badURL,
		httpTransportType: HTTPTransportSSE,
		httpClient:        &http.Client{Timeout: 2 * time.Second},
		connectTimeout:    2 * time.Second,
	}

	err := conn.reconnectSDKTransport()

	require.Error(t, err)
	assert.ErrorContains(t, err, "session reconnect failed")
}

// TestNewHTTPConnection_ServerInfo_PopulatedAfterConnect verifies that the
// server name/version returned by ServerInfo is captured during initialization.
func TestNewHTTPConnection_ServerInfo_PopulatedAfterConnect(t *testing.T) {
	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "session-1")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{
						"name":    "backend-a",
						"version": "1.2.3",
					},
					"capabilities": map[string]interface{}{},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)

	name, version := conn.ServerInfo()
	assert.Equal(t, "backend-a", name)
	assert.Equal(t, "1.2.3", version)
}

// TestReconnectSDKTransport_ExistingSessionClosed verifies that reconnectSDKTransport
// gracefully closes an existing (non-nil) session before establishing a new one.
// This test targets the "if c.session != nil { _ = c.session.Close() }" branch.
func TestReconnectSDKTransport_ExistingSessionClosed(t *testing.T) {
	var initCount atomic.Int32
	srv := newStreamableMCPTestServer(t, func(w http.ResponseWriter, method string, req map[string]interface{}) {
		switch method {
		case "initialize":
			count := initCount.Add(1)
			writeInitializeResponse(w, fmt.Sprintf("session-%d", count), req)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	conn := newStreamableConn(t, srv.URL)
	require.Equal(t, int32(1), initCount.Load())
	require.NotNil(t, conn.session, "session should be set after NewHTTPConnection")

	// Directly call reconnectSDKTransport – it should close the existing session and
	// open a new one via the same server.
	err := conn.reconnectSDKTransport()

	require.NoError(t, err)
	assert.Equal(t, int32(2), initCount.Load(), "reconnect should have issued a second initialize")
	require.NotNil(t, conn.session, "session should be non-nil after successful reconnect")
}

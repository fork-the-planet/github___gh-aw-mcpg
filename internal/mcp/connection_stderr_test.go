package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConnection_SendRequest verifies that SendRequest delegates to SendRequestWithServerID
// using "unknown" as the serverID and context.Background() as the context.
// This tests the simplified API surface used by callers that don't need to specify a serverID.
func TestConnection_SendRequest(t *testing.T) {
	var receivedMethod string

	srv := newPlainJSONTestServer(t, func(w http.ResponseWriter, r *http.Request, method string, body []byte) {
		receivedMethod = method

		var req map[string]interface{}
		err := json.Unmarshal(body, &req)
		require.NoError(t, err)
		require.Contains(t, req, "id")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]interface{}{
				"tools": []interface{}{},
			},
		})
	})
	defer srv.Close()

	conn, err := NewHTTPConnection(context.Background(), "test-server", srv.URL, map[string]string{
		"Authorization": "test-token",
	}, nil, "", 0, 0)
	require.NoError(t, err)
	defer conn.Close()

	resp, err := conn.SendRequest("tools/list", nil)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, "tools/list", receivedMethod)
}

// TestConnection_SendRequest_UnsupportedMethod verifies that SendRequest surfaces errors
// from the underlying transport (e.g., unsupported method on a nil-session stdio connection).
func TestConnection_SendRequest_UnsupportedMethod(t *testing.T) {
	conn := newTestConnection(t)

	result, err := conn.SendRequest("unsupported/method", nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unsupported method")
}

// TestConnection_Close_NilSession verifies that Close does not panic and returns nil
// when the connection has no active SDK session (e.g., plain JSON-RPC or test connections).
func TestConnection_Close_NilSession(t *testing.T) {
	conn := newTestConnection(t)
	assert.Nil(t, conn.session, "test connection should have no session")
	err := conn.Close()
	assert.NoError(t, err)
}

// TestConnection_Close_CancelsContext verifies that Close cancels the connection context,
// making it unusable for further requests.
func TestConnection_Close_CancelsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &Connection{
		ctx:    ctx,
		cancel: cancel,
	}
	require.NoError(t, ctx.Err(), "context should be valid before Close")

	err := conn.Close()
	assert.NoError(t, err)
	assert.ErrorIs(t, ctx.Err(), context.Canceled, "context should be cancelled after Close")
}

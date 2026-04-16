package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMinimalTestServer returns an httptest server that responds with the
// minimal HTTP semantics needed for NewHTTPConnection to complete its SDK
// transport handshake reliably.
func newMinimalTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			return
		case http.MethodPost:
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo":      map[string]interface{}{"name": "test"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "test-session")
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
			return
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	}))
}

// TestNewHTTPConnection_DefaultConnectTimeout_ZeroInput verifies that a zero
// connectTimeout is replaced with defaultConnectTimeout (30 s).
func TestNewHTTPConnection_DefaultConnectTimeout_ZeroInput(t *testing.T) {
	srv := newMinimalTestServer(t)
	defer srv.Close()

	conn, err := NewHTTPConnection(context.Background(), "test", srv.URL,
		map[string]string{"Authorization": "test"}, nil, "", 0, 0)
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	assert.Equal(t, defaultConnectTimeout, conn.connectTimeout,
		"zero connectTimeout should be replaced with defaultConnectTimeout")
}

// TestNewHTTPConnection_DefaultConnectTimeout_NegativeInput verifies that a
// negative connectTimeout is also replaced with defaultConnectTimeout.
func TestNewHTTPConnection_DefaultConnectTimeout_NegativeInput(t *testing.T) {
	srv := newMinimalTestServer(t)
	defer srv.Close()

	conn, err := NewHTTPConnection(context.Background(), "test", srv.URL,
		map[string]string{"Authorization": "test"}, nil, "", 0, -1*time.Second)
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	assert.Equal(t, defaultConnectTimeout, conn.connectTimeout,
		"negative connectTimeout should be replaced with defaultConnectTimeout")
}

// TestNewHTTPConnection_DefaultConnectTimeout_CustomValue verifies that a
// positive connectTimeout is stored as-is without being replaced.
func TestNewHTTPConnection_DefaultConnectTimeout_CustomValue(t *testing.T) {
	srv := newMinimalTestServer(t)
	defer srv.Close()

	custom := 10 * time.Second
	conn, err := NewHTTPConnection(context.Background(), "test", srv.URL,
		map[string]string{"Authorization": "test"}, nil, "", 0, custom)
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	assert.Equal(t, custom, conn.connectTimeout,
		"a positive connectTimeout should be stored unchanged")
}

// TestDefaultConnectTimeout_Value guards against the constant value drifting
// away from config.DefaultConnectTimeout (30 s) unintentionally.
func TestDefaultConnectTimeout_Value(t *testing.T) {
	assert.Equal(t, 30*time.Second, defaultConnectTimeout,
		"defaultConnectTimeout must remain 30 s to stay in sync with config.DefaultConnectTimeout")
}

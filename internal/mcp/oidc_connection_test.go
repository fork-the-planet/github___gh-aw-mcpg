package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw-mcpg/internal/oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewHTTPConnection_WithOIDCProvider verifies that when an oidcProvider is
// supplied, NewHTTPConnection enables OIDC authentication by building the HTTP
// client with the OIDC round-tripper. The resulting connection should include an
// Authorization: Bearer <token> header derived from the OIDC provider on every
// outgoing request.
func TestNewHTTPConnection_WithOIDCProvider(t *testing.T) {
	// Build a minimal JWT that the OIDC provider can cache.
	jwtToken := makeTestJWT(time.Now().Add(10 * time.Minute).Unix())

	// Set up a mock OIDC token endpoint that returns our test JWT.
	oidcServer := newTestOIDCServer(t, jwtToken)
	defer oidcServer.Close()

	// Track what Authorization headers the MCP backend received.
	var receivedAuths []string
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuths = append(receivedAuths, r.Header.Get("Authorization"))

		switch r.Method {
		case http.MethodGet:
			// SSE transport probe — respond with event-stream to allow fallback to SSE.
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		case http.MethodPost:
			// Respond with a minimal valid MCP initialize response.
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{
						"name":    "oidc-test-server",
						"version": "1.0.0",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "oidc-session-123")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer mcpServer.Close()

	provider := oidc.NewProvider(oidcServer.URL, "request-token")

	conn, err := NewHTTPConnection(
		context.Background(),
		"oidc-test",
		mcpServer.URL,
		nil, // no static headers
		provider,
		"https://example.com/audience",
		0,
		0,
	)
	require.NoError(t, err, "NewHTTPConnection with OIDC provider should succeed")
	require.NotNil(t, conn, "Connection should not be nil")
	defer conn.Close()

	// At least one request should have carried an OIDC Bearer token.
	bearerFound := false
	for _, auth := range receivedAuths {
		if strings.HasPrefix(auth, "Bearer ") {
			bearerFound = true
			assert.Equal(t, "Bearer "+jwtToken, auth, "Authorization header should contain the OIDC JWT")
			break
		}
	}
	assert.True(t, bearerFound, "At least one request to the MCP server should carry a Bearer token from OIDC")
}

// TestNewHTTPConnection_WithOIDCAndStaticHeaders verifies that static headers
// are preserved while the OIDC token overrides the Authorization header — the
// same layering that NewHTTPConnection uses (static headers outer, OIDC inner).
func TestNewHTTPConnection_WithOIDCAndStaticHeaders(t *testing.T) {
	jwtToken := makeTestJWT(time.Now().Add(10 * time.Minute).Unix())

	oidcServer := newTestOIDCServer(t, jwtToken)
	defer oidcServer.Close()

	type capturedRequest struct {
		auth   string
		custom string
	}
	var captured []capturedRequest

	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = append(captured, capturedRequest{
			auth:   r.Header.Get("Authorization"),
			custom: r.Header.Get("X-Custom-Header"),
		})

		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
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
			w.Header().Set("Mcp-Session-Id", "session-oidc-static")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer mcpServer.Close()

	provider := oidc.NewProvider(oidcServer.URL, "request-token")

	// Pass a static Authorization header AND an OIDC provider; the OIDC token
	// must win because the OIDC round-tripper sits inside the static-header
	// round-tripper.
	conn, err := NewHTTPConnection(
		context.Background(),
		"oidc-static-test",
		mcpServer.URL,
		map[string]string{
			"Authorization":   "static-token-should-be-overridden",
			"X-Custom-Header": "my-custom-value",
		},
		provider,
		"https://example.com/audience",
		0,
		0,
	)
	require.NoError(t, err, "NewHTTPConnection with OIDC and static headers should succeed")
	require.NotNil(t, conn)
	defer conn.Close()

	// Find at least one POST request (the MCP initialize call).
	postFound := false
	for _, c := range captured {
		if c.auth != "" {
			// OIDC token overrides the static Authorization header.
			assert.Equal(t, "Bearer "+jwtToken, c.auth,
				"OIDC token must override static Authorization header")
			// Non-Authorization static headers must pass through unchanged.
			assert.Equal(t, "my-custom-value", c.custom,
				"X-Custom-Header should be preserved from static headers")
			postFound = true
			break
		}
	}
	assert.True(t, postFound, "At least one request with headers should have been captured")
}

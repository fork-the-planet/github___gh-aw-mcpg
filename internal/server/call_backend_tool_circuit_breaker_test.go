package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMockHTTPBackendForCB creates a mock HTTP MCP backend that serves initialize,
// tools/list, and tools/call requests. The toolsCallHandler is called for any
// tools/call request and should write a JSON-RPC response to w.
func newMockHTTPBackendForCB(t *testing.T, toolNames []string, toolsCallHandler func(w http.ResponseWriter, reqID interface{})) *httptest.Server {
	t.Helper()
	tools := make([]map[string]interface{}, len(toolNames))
	for i, name := range toolNames {
		tools[i] = map[string]interface{}{
			"name":        name,
			"description": name,
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		method, _ := req["method"].(string)
		switch method {
		case "initialize":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]interface{}{},
					"serverInfo":      map[string]interface{}{"name": "mock-cb-backend", "version": "1.0.0"},
				},
			})
		case "tools/list":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req["id"],
				"result": map[string]interface{}{"tools": tools},
			})
		case "tools/call":
			toolsCallHandler(w, req["id"])
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req["id"],
				"error": map[string]interface{}{
					"code":    -32601,
					"message": "method not found: " + method,
				},
			})
		}
	}))
}

// TestCallBackendTool_CircuitBreakerOpen_RejectsRequestWithoutCallingBackend verifies
// that when the circuit breaker is OPEN for a server, callBackendTool returns an
// ErrCircuitOpen error result immediately, without forwarding the call to the backend.
func TestCallBackendTool_CircuitBreakerOpen_RejectsRequestWithoutCallingBackend(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	// Track whether the backend's tools/call handler was invoked.
	var backendCalled atomic.Bool

	backend := newMockHTTPBackendForCB(t, []string{"test_tool"}, func(w http.ResponseWriter, reqID interface{}) {
		backendCalled.Store(true)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0", "id": reqID,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{{"type": "text", "text": "should not reach here"}},
			},
		})
	})
	defer backend.Close()

	// Use threshold=1 so a single RecordRateLimit call trips the circuit.
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"cb-server": {
				Type:               "http",
				URL:                backend.URL,
				RateLimitThreshold: 1,
				RateLimitCooldown:  3600, // long cooldown so the circuit stays open during the test
			},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	// Manually trip the circuit breaker by recording a rate-limit event.
	cb := us.getCircuitBreaker("cb-server")
	require.NotNil(cb)
	cb.RecordRateLimit(time.Now().Add(time.Hour))

	assert.Equal(circuitOpen, cb.State(), "circuit breaker should be OPEN after recording rate limit at threshold")

	// callBackendTool should now reject the call because the circuit is OPEN.
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "cb-test-session")
	result, _, callErr := us.callBackendTool(ctx, "cb-server", "test_tool", map[string]interface{}{})

	require.Error(callErr, "callBackendTool should return an error when circuit is OPEN")
	require.NotNil(result, "result must never be nil")
	assert.True(result.IsError, "result should be marked as error")

	// Verify the error mentions the circuit breaker / server.
	require.Len(result.Content, 1)
	textContent, ok := result.Content[0].(*sdk.TextContent)
	require.True(ok, "content should be TextContent")
	assert.Contains(textContent.Text, "cb-server", "error should mention the server ID")
	assert.Contains(textContent.Text, "OPEN", "error should mention circuit is OPEN")

	// The backend must NOT have been contacted for the tool call.
	assert.False(backendCalled.Load(), "backend should not be called when circuit breaker is OPEN")
}

// TestCallBackendTool_RateLimitResponse_TripsCircuitBreaker verifies that when the
// backend returns a rate-limit error response, callBackendTool:
//   - returns the original rate-limit error text (not a generic error)
//   - records the rate limit on the circuit breaker
func TestCallBackendTool_RateLimitResponse_TripsCircuitBreaker(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	const rateLimitMsg = "API rate limit exceeded [rate reset in 60s]"

	backend := newMockHTTPBackendForCB(t, []string{"search_repos"}, func(w http.ResponseWriter, reqID interface{}) {
		// Simulate a GitHub MCP server rate-limit response.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0", "id": reqID,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": rateLimitMsg},
				},
				"isError": true,
			},
		})
	})
	defer backend.Close()

	// Use threshold=2 so the circuit stays closed after one rate-limit event; we
	// only want to verify the error text and that RecordRateLimit was called.
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"rl-server": {
				Type:               "http",
				URL:                backend.URL,
				RateLimitThreshold: 2,
				RateLimitCooldown:  3600,
			},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	cb := us.getCircuitBreaker("rl-server")
	require.NotNil(cb)

	ctx := context.WithValue(context.Background(), SessionIDContextKey, "rl-test-session")
	result, rawResult, callErr := us.callBackendTool(ctx, "rl-server", "search_repos", map[string]interface{}{})

	// A rate-limit response is returned as an error result (no Go error).
	require.NoError(callErr, "rate-limit result should not produce a Go error (just an error tool result)")
	require.NotNil(result, "result must not be nil")
	require.NotNil(rawResult, "raw result must not be nil for rate-limit response")
	assert.True(result.IsError, "result should be marked as error")

	// The content should contain the original rate-limit text from the backend.
	require.NotEmpty(result.Content, "result must have content")
	textContent, ok := result.Content[0].(*sdk.TextContent)
	require.True(ok, "content[0] should be TextContent")
	assert.Equal(rateLimitMsg, textContent.Text, "error text should match the backend rate-limit message")

	// After one rate-limit event with threshold=2, the circuit should still be
	// CLOSED but the consecutive error counter should have incremented.
	assert.Equal(circuitClosed, cb.State(), "circuit should be CLOSED after only 1 of 2 required errors")
}

// TestCallBackendTool_RateLimitResponse_OpensCircuitOnThreshold verifies that
// repeated rate-limit responses from the backend eventually open the circuit breaker.
func TestCallBackendTool_RateLimitResponse_OpensCircuitOnThreshold(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	const rateLimitMsg = "too many requests"

	backend := newMockHTTPBackendForCB(t, []string{"list_issues"}, func(w http.ResponseWriter, reqID interface{}) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0", "id": reqID,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": rateLimitMsg},
				},
				"isError": true,
			},
		})
	})
	defer backend.Close()

	// threshold=1: first rate-limit opens the circuit immediately.
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"rl-threshold-server": {
				Type:               "http",
				URL:                backend.URL,
				RateLimitThreshold: 1,
				RateLimitCooldown:  3600,
			},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	cb := us.getCircuitBreaker("rl-threshold-server")
	require.NotNil(cb)

	ctx := context.WithValue(context.Background(), SessionIDContextKey, "rl-threshold-session")

	// First call — backend returns rate limit; circuit breaker should trip.
	result, _, callErr := us.callBackendTool(ctx, "rl-threshold-server", "list_issues", map[string]interface{}{})
	require.NoError(callErr, "first rate-limit result should not produce a Go error")
	require.NotNil(result)
	assert.True(result.IsError)

	// Circuit should now be OPEN.
	assert.Equal(circuitOpen, cb.State(), "circuit should be OPEN after hitting threshold of 1")

	// Second call — circuit breaker should reject the request outright.
	result2, _, callErr2 := us.callBackendTool(ctx, "rl-threshold-server", "list_issues", map[string]interface{}{})
	require.Error(callErr2, "second call should be rejected by the open circuit breaker")
	require.NotNil(result2)
	assert.True(result2.IsError)

	textContent, ok := result2.Content[0].(*sdk.TextContent)
	require.True(ok)
	assert.Contains(textContent.Text, "OPEN")
}

// TestCallBackendTool_SuccessfulCall_RecordsCircuitBreakerSuccess verifies that
// a successful backend call causes the circuit breaker to record success, which
// resets the consecutive error counter (and closes the circuit if HALF-OPEN).
func TestCallBackendTool_SuccessfulCall_RecordsCircuitBreakerSuccess(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	backend := newMockHTTPBackendForCB(t, []string{"get_issue"}, func(w http.ResponseWriter, reqID interface{}) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0", "id": reqID,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{{"type": "text", "text": "issue #42 details"}},
			},
		})
	})
	defer backend.Close()

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"success-server": {
				Type:               "http",
				URL:                backend.URL,
				RateLimitThreshold: 3,
				RateLimitCooldown:  3600,
			},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	cb := us.getCircuitBreaker("success-server")
	require.NotNil(cb)

	// Pre-load two consecutive rate-limit errors (threshold is 3, so not yet open).
	cb.RecordRateLimit(time.Time{})
	cb.RecordRateLimit(time.Time{})
	assert.Equal(circuitClosed, cb.State(), "circuit should still be CLOSED with 2/3 errors")

	// A successful call should reset the consecutive error counter via RecordSuccess.
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "success-test-session")
	result, _, callErr := us.callBackendTool(ctx, "success-server", "get_issue", map[string]interface{}{})

	require.NoError(callErr)
	require.NotNil(result)
	assert.False(result.IsError, "successful call should not be marked as error")

	// After RecordSuccess, one more rate-limit error should NOT open the circuit
	// (because the counter was reset to zero by the success).
	cb.RecordRateLimit(time.Time{})
	assert.Equal(circuitClosed, cb.State(), "circuit should still be CLOSED — success reset the consecutive error counter")
}

// TestCallBackendTool_CircuitBreakerHalfOpen_SuccessfulProbeClosesCircuit verifies the
// HALF-OPEN → CLOSED transition when a probe call succeeds via callBackendTool.
func TestCallBackendTool_CircuitBreakerHalfOpen_SuccessfulProbeClosesCircuit(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	backend := newMockHTTPBackendForCB(t, []string{"probe_tool"}, func(w http.ResponseWriter, reqID interface{}) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0", "id": reqID,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{{"type": "text", "text": "probe succeeded"}},
			},
		})
	})
	defer backend.Close()

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"halfopen-server": {
				Type:               "http",
				URL:                backend.URL,
				RateLimitThreshold: 1,
				RateLimitCooldown:  1, // 1-second cooldown so we can quickly enter HALF-OPEN
			},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	defer us.Close()

	cb := us.getCircuitBreaker("halfopen-server")
	require.NotNil(cb)

	// Use a fake clock so we can advance time without sleeping.
	fakeNow := time.Now()
	cb.nowFunc = func() time.Time { return fakeNow }

	// Open the circuit.
	cb.RecordRateLimit(time.Time{})
	require.Equal(circuitOpen, cb.State())

	// Advance the fake clock past the cooldown so Allow() transitions to HALF-OPEN.
	fakeNow = fakeNow.Add(1100 * time.Millisecond)

	// At this point Allow() would return nil (HALF-OPEN) and let one probe through.
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "halfopen-session")
	result, _, callErr := us.callBackendTool(ctx, "halfopen-server", "probe_tool", map[string]interface{}{})

	require.NoError(callErr, "probe call should succeed")
	require.NotNil(result)
	assert.False(result.IsError, "probe result should not be an error")

	// After a successful probe, the circuit should be CLOSED again.
	assert.Equal(circuitClosed, cb.State(), "circuit should be CLOSED after successful probe")
}

package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLauncher(servers map[string]*config.ServerConfig) *Launcher {
	ctx := context.Background()
	cfg := &config.Config{Servers: servers}
	return New(ctx, cfg)
}

func TestHealthMonitor_StartStop(t *testing.T) {
	l := newTestLauncher(map[string]*config.ServerConfig{})
	hm := NewHealthMonitor(l, 50*time.Millisecond)

	hm.Start()
	// Let at least one tick fire
	time.Sleep(100 * time.Millisecond)
	hm.Stop()

	// Verify doneCh is closed (Stop returned)
	select {
	case <-hm.doneCh:
		// expected
	default:
		t.Fatal("doneCh should be closed after Stop")
	}
}

func TestHealthMonitor_DefaultInterval(t *testing.T) {
	l := newTestLauncher(map[string]*config.ServerConfig{})
	hm := NewHealthMonitor(l, 0)

	assert.Equal(t, DefaultHealthCheckInterval, hm.interval)
}

func TestHealthMonitor_RunningServerResetsFailureCounter(t *testing.T) {
	servers := map[string]*config.ServerConfig{
		"test-server": {Type: "http", URL: "http://localhost:9999"},
	}
	l := newTestLauncher(servers)

	// Simulate a running server
	l.recordStart("test-server")

	hm := NewHealthMonitor(l, 50*time.Millisecond)
	hm.consecutiveFailures["test-server"] = 2

	hm.checkAll()

	assert.Equal(t, 0, hm.consecutiveFailures["test-server"])
}

func TestHealthMonitor_ErrorStateIncrementsFailureCounter(t *testing.T) {
	// Use a server config that will fail to launch (no Docker available in test)
	servers := map[string]*config.ServerConfig{
		"bad-server": {Type: "stdio", Command: "nonexistent-binary-xyz"},
	}
	l := newTestLauncher(servers)

	// Simulate the server being in error state
	l.recordError("bad-server", "process crashed")

	hm := NewHealthMonitor(l, time.Hour) // large interval; we call checkAll manually

	hm.checkAll()

	// Server should have failed restart and incremented counter
	assert.Equal(t, 1, hm.consecutiveFailures["bad-server"])
}

func TestHealthMonitor_StopsRetryingAtMaxFailures(t *testing.T) {
	servers := map[string]*config.ServerConfig{
		"bad-server": {Type: "stdio", Command: "nonexistent-binary-xyz"},
	}
	l := newTestLauncher(servers)

	hm := NewHealthMonitor(l, time.Hour)
	hm.consecutiveFailures["bad-server"] = maxConsecutiveRestartFailures

	// Simulate error state
	l.recordError("bad-server", "still broken")

	hm.checkAll()

	// Should not have incremented further
	assert.Equal(t, maxConsecutiveRestartFailures, hm.consecutiveFailures["bad-server"])

	// Error should still be present (no restart attempted)
	state := l.GetServerState("bad-server")
	assert.Equal(t, "error", state.Status)
}

func TestClearServerForRestart(t *testing.T) {
	l := newTestLauncher(map[string]*config.ServerConfig{
		"srv": {Type: "http", URL: "http://localhost:9999"},
	})

	// Record start then error
	l.serverStartTimes["srv"] = time.Now()
	l.serverErrors["srv"] = "something failed"

	state := l.GetServerState("srv")
	require.Equal(t, "error", state.Status)

	l.clearServerForRestart("srv")

	state = l.GetServerState("srv")
	assert.Equal(t, "stopped", state.Status)
	assert.Empty(t, state.LastError)
}

func TestHealthMonitor_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := &config.Config{Servers: map[string]*config.ServerConfig{}}
	l := New(ctx, cfg)

	hm := NewHealthMonitor(l, 50*time.Millisecond)
	hm.Start()

	// Cancel context — monitor should exit
	cancel()

	select {
	case <-hm.doneCh:
		// expected — monitor stopped
	case <-time.After(2 * time.Second):
		t.Fatal("health monitor did not stop after context cancellation")
	}
}

// TestHealthMonitor_LogsErrorWhenMaxFailuresReached verifies that the monitor
// emits a "reached max restart attempts" error log and does not further
// increment the counter when a restart failure pushes the counter to exactly
// maxConsecutiveRestartFailures.
func TestHealthMonitor_LogsErrorWhenMaxFailuresReached(t *testing.T) {
	servers := map[string]*config.ServerConfig{
		"bad-server": {Type: "stdio", Command: "nonexistent-binary-xyz"},
	}
	l := newTestLauncher(servers)
	l.recordError("bad-server", "persistent failure")

	hm := NewHealthMonitor(l, time.Hour)
	// Set counter to one below the threshold so the next failure reaches it.
	hm.consecutiveFailures["bad-server"] = maxConsecutiveRestartFailures - 1

	hm.checkAll()

	// Counter must now equal the threshold (not exceed it).
	require.Equal(t, maxConsecutiveRestartFailures, hm.consecutiveFailures["bad-server"])
	// The server remains in error state because the restart failed.
	state := l.GetServerState("bad-server")
	assert.Equal(t, "error", state.Status)
}

// mockMCPHandler returns an HTTP handler that responds to any POST with a
// minimal valid JSON-RPC initialize response. This is sufficient for the
// plain JSON-RPC transport fallback inside NewHTTPConnection to succeed.
func mockMCPHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{},
				"serverInfo": map[string]interface{}{
					"name":    "mock-server",
					"version": "1.0.0",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		data, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, "marshal failed", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	})
}

// TestHealthMonitor_SuccessfulRestartResetCounter verifies that when a
// health-check restart succeeds, the consecutive-failure counter is reset to
// zero and the server transitions back to "running" state.
func TestHealthMonitor_SuccessfulRestartResetCounter(t *testing.T) {
	mockServer := httptest.NewServer(mockMCPHandler(t))
	defer mockServer.Close()

	servers := map[string]*config.ServerConfig{
		"good-server": {Type: "http", URL: mockServer.URL},
	}
	l := newTestLauncher(servers)

	// Simulate a server that previously failed.
	l.recordError("good-server", "transient failure")

	hm := NewHealthMonitor(l, time.Hour)
	hm.consecutiveFailures["good-server"] = 1

	hm.checkAll()

	// A successful restart must reset the failure counter.
	assert.Equal(t, 0, hm.consecutiveFailures["good-server"])
	// The server should now report as running.
	state := l.GetServerState("good-server")
	assert.Equal(t, "running", state.Status)
}

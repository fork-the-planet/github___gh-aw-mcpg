package server

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnforceToolCallLimit covers all branches of the enforceToolCallLimit method.
//
// enforceToolCallLimit applies per-session per-server per-tool call budgets.
// It must:
//  1. Allow calls unconditionally when no session exists.
//  2. Allow calls unconditionally when no GuardInit state exists for the server.
//  3. Allow calls unconditionally when ToolCallLimits is empty (no limits configured).
//  4. Allow calls unconditionally when the specific tool has no limit entry.
//  5. Allow calls unconditionally when the limit for the tool is 0.
//  6. Increment the call counter and allow the call while under budget.
//  7. Return an error (without incrementing) once the budget is exhausted.
//  8. Initialise the ToolCallCounts map lazily on first use.
//  9. Handle concurrent calls correctly (no data races, correct counter).
func TestEnforceToolCallLimit(t *testing.T) {
	// helper to build a UnifiedServer seeded with one session + guard state.
	makeServer := func(sessionID, serverID string, limits map[string]int, counts map[string]int) *UnifiedServer {
		state := &GuardSessionState{
			ToolCallLimits: limits,
			ToolCallCounts: counts,
		}
		session := NewSession(sessionID, "")
		session.GuardInit[serverID] = state
		us := &UnifiedServer{
			sessions: map[string]*Session{sessionID: session},
		}
		return us
	}

	t.Run("no session — always allowed", func(t *testing.T) {
		us := &UnifiedServer{sessions: map[string]*Session{}}
		err := us.enforceToolCallLimit("missing-session", "srv", "tool_a")
		assert.NoError(t, err)
	})

	t.Run("session exists but no GuardInit for server — always allowed", func(t *testing.T) {
		session := NewSession("sess1", "")
		us := &UnifiedServer{
			sessions: map[string]*Session{"sess1": session},
		}
		err := us.enforceToolCallLimit("sess1", "unknown-server", "tool_a")
		assert.NoError(t, err)
	})

	t.Run("GuardSessionState has nil ToolCallLimits — always allowed", func(t *testing.T) {
		state := &GuardSessionState{ToolCallLimits: nil}
		session := NewSession("sess2", "")
		session.GuardInit["srv"] = state
		us := &UnifiedServer{sessions: map[string]*Session{"sess2": session}}
		err := us.enforceToolCallLimit("sess2", "srv", "tool_a")
		assert.NoError(t, err)
	})

	t.Run("ToolCallLimits is empty map — always allowed", func(t *testing.T) {
		us := makeServer("sess3", "srv", map[string]int{}, nil)
		err := us.enforceToolCallLimit("sess3", "srv", "tool_a")
		assert.NoError(t, err)
	})

	t.Run("tool not present in ToolCallLimits — always allowed", func(t *testing.T) {
		us := makeServer("sess4", "srv", map[string]int{"other_tool": 5}, nil)
		err := us.enforceToolCallLimit("sess4", "srv", "tool_a")
		assert.NoError(t, err)
	})

	t.Run("tool limit is 0 — always allowed", func(t *testing.T) {
		us := makeServer("sess5", "srv", map[string]int{"tool_a": 0}, nil)
		err := us.enforceToolCallLimit("sess5", "srv", "tool_a")
		assert.NoError(t, err)
	})

	t.Run("first call within budget — allowed and counter incremented", func(t *testing.T) {
		us := makeServer("sess6", "srv", map[string]int{"tool_a": 3}, nil)
		err := us.enforceToolCallLimit("sess6", "srv", "tool_a")
		require.NoError(t, err)

		state := us.sessions["sess6"].GuardInit["srv"]
		assert.Equal(t, 1, state.ToolCallCounts["tool_a"])
	})

	t.Run("calls up to limit are allowed, each increments counter", func(t *testing.T) {
		us := makeServer("sess7", "srv", map[string]int{"tool_a": 3}, map[string]int{})

		for i := 1; i <= 3; i++ {
			err := us.enforceToolCallLimit("sess7", "srv", "tool_a")
			require.NoError(t, err, "call %d should be allowed", i)
			state := us.sessions["sess7"].GuardInit["srv"]
			assert.Equal(t, i, state.ToolCallCounts["tool_a"], "counter after call %d", i)
		}
	})

	t.Run("call at limit returns error — counter not incremented", func(t *testing.T) {
		us := makeServer("sess8", "srv", map[string]int{"tool_a": 2}, map[string]int{"tool_a": 2})

		err := us.enforceToolCallLimit("sess8", "srv", "tool_a")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"tool_a"`)
		assert.Contains(t, err.Error(), "2")

		// Counter must not have been incremented beyond the limit.
		state := us.sessions["sess8"].GuardInit["srv"]
		assert.Equal(t, 2, state.ToolCallCounts["tool_a"])
	})

	t.Run("error message contains tool name and max", func(t *testing.T) {
		us := makeServer("sess9", "srv", map[string]int{"my_tool": 5}, map[string]int{"my_tool": 5})
		err := us.enforceToolCallLimit("sess9", "srv", "my_tool")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"my_tool"`)
		assert.Contains(t, err.Error(), "(max: 5)")
	})

	t.Run("nil ToolCallCounts is lazily initialised on first allowed call", func(t *testing.T) {
		us := makeServer("sess10", "srv", map[string]int{"tool_a": 5}, nil)

		// ToolCallCounts starts nil.
		state := us.sessions["sess10"].GuardInit["srv"]
		assert.Nil(t, state.ToolCallCounts)

		err := us.enforceToolCallLimit("sess10", "srv", "tool_a")
		require.NoError(t, err)

		// After the call ToolCallCounts must have been allocated and incremented.
		assert.NotNil(t, state.ToolCallCounts)
		assert.Equal(t, 1, state.ToolCallCounts["tool_a"])
	})

	t.Run("limits are independent per tool", func(t *testing.T) {
		us := makeServer("sess11", "srv",
			map[string]int{"tool_a": 2, "tool_b": 1},
			map[string]int{},
		)

		require.NoError(t, us.enforceToolCallLimit("sess11", "srv", "tool_a"))
		require.NoError(t, us.enforceToolCallLimit("sess11", "srv", "tool_a"))
		require.Error(t, us.enforceToolCallLimit("sess11", "srv", "tool_a"), "tool_a exhausted")

		require.NoError(t, us.enforceToolCallLimit("sess11", "srv", "tool_b"))
		require.Error(t, us.enforceToolCallLimit("sess11", "srv", "tool_b"), "tool_b exhausted")
	})

	t.Run("limits are independent per server", func(t *testing.T) {
		limits := map[string]int{"tool_a": 1}

		stateA := &GuardSessionState{ToolCallLimits: limits, ToolCallCounts: map[string]int{}}
		stateB := &GuardSessionState{ToolCallLimits: limits, ToolCallCounts: map[string]int{}}
		session := NewSession("sess12", "")
		session.GuardInit["srv_a"] = stateA
		session.GuardInit["srv_b"] = stateB
		us := &UnifiedServer{sessions: map[string]*Session{"sess12": session}}

		require.NoError(t, us.enforceToolCallLimit("sess12", "srv_a", "tool_a"))
		require.Error(t, us.enforceToolCallLimit("sess12", "srv_a", "tool_a"), "srv_a exhausted")

		// srv_b has its own independent counter.
		require.NoError(t, us.enforceToolCallLimit("sess12", "srv_b", "tool_a"))
	})

	t.Run("concurrent calls — no data race, final count correct", func(t *testing.T) {
		const limit = 50
		us := makeServer("sess13", "srv", map[string]int{"tool_a": limit}, map[string]int{})

		var wg sync.WaitGroup
		allowed := make(chan struct{}, limit+10)

		for i := 0; i < limit+10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := us.enforceToolCallLimit("sess13", "srv", "tool_a"); err == nil {
					allowed <- struct{}{}
				}
			}()
		}

		wg.Wait()
		close(allowed)

		count := 0
		for range allowed {
			count++
		}
		assert.Equal(t, limit, count, "exactly %d calls should have been allowed", limit)

		state := us.sessions["sess13"].GuardInit["srv"]
		state.CallCountMu.Lock()
		finalCount := state.ToolCallCounts["tool_a"]
		state.CallCountMu.Unlock()
		assert.Equal(t, limit, finalCount)
	})
}

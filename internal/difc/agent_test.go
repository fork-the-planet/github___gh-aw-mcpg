package difc

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentLabels_Clone tests the Clone method for deep copying agent labels
func TestAgentLabels_Clone(t *testing.T) {
	tests := []struct {
		name           string
		setupAgent     func() *AgentLabels
		modifyOriginal func(*AgentLabels)
		assertClone    func(*testing.T, *AgentLabels, *AgentLabels)
	}{
		{
			name: "clone empty agent labels",
			setupAgent: func() *AgentLabels {
				return NewAgentLabels("test-agent")
			},
			modifyOriginal: func(a *AgentLabels) {
				a.AddSecrecyTag("modified")
			},
			assertClone: func(t *testing.T, original, clone *AgentLabels) {
				assert.Equal(t, "test-agent", clone.AgentID)
				assert.Empty(t, clone.GetSecrecyTags(), "Clone should not reflect modifications to original")
				assert.NotEmpty(t, original.GetSecrecyTags(), "Original should be modified")
			},
		},
		{
			name: "clone agent with secrecy tags",
			setupAgent: func() *AgentLabels {
				agent := NewAgentLabels("secure-agent")
				agent.AddSecrecyTag("secret")
				agent.AddSecrecyTag("confidential")
				return agent
			},
			modifyOriginal: func(a *AgentLabels) {
				a.AddSecrecyTag("top-secret")
			},
			assertClone: func(t *testing.T, original, clone *AgentLabels) {
				assert.Equal(t, "secure-agent", clone.AgentID)
				cloneTags := clone.GetSecrecyTags()
				assert.Len(t, cloneTags, 2, "Clone should have original 2 tags")
				assert.Contains(t, cloneTags, Tag("secret"))
				assert.Contains(t, cloneTags, Tag("confidential"))
				assert.NotContains(t, cloneTags, Tag("top-secret"))
			},
		},
		{
			name: "clone agent with integrity tags",
			setupAgent: func() *AgentLabels {
				agent := NewAgentLabels("trusted-agent")
				agent.AddIntegrityTag("verified")
				agent.AddIntegrityTag("production")
				return agent
			},
			modifyOriginal: func(a *AgentLabels) {
				a.DropIntegrityTag("production")
			},
			assertClone: func(t *testing.T, original, clone *AgentLabels) {
				assert.Equal(t, "trusted-agent", clone.AgentID)
				cloneTags := clone.GetIntegrityTags()
				assert.Len(t, cloneTags, 2, "Clone should have original 2 tags")
				assert.Contains(t, cloneTags, Tag("verified"))
				assert.Contains(t, cloneTags, Tag("production"))
			},
		},
		{
			name: "clone agent with both secrecy and integrity tags",
			setupAgent: func() *AgentLabels {
				agent := NewAgentLabelsWithTags(
					"complex-agent",
					[]Tag{"private", "internal"},
					[]Tag{"trusted", "validated"},
				)
				return agent
			},
			modifyOriginal: func(a *AgentLabels) {
				a.AddSecrecyTag("extra-secret")
				a.AddIntegrityTag("extra-trust")
			},
			assertClone: func(t *testing.T, original, clone *AgentLabels) {
				assert.Equal(t, "complex-agent", clone.AgentID)
				secrecyTags := clone.GetSecrecyTags()
				integrityTags := clone.GetIntegrityTags()
				assert.Len(t, secrecyTags, 2)
				assert.Len(t, integrityTags, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := tt.setupAgent()
			clone := original.Clone()

			// Modify original after cloning
			tt.modifyOriginal(original)

			// Assert clone is independent
			tt.assertClone(t, original, clone)
		})
	}
}

// TestAgentLabels_GetSecrecyTags tests thread-safe retrieval of secrecy tags
func TestAgentLabels_GetSecrecyTags(t *testing.T) {
	tests := []struct {
		name     string
		tags     []Tag
		expected []Tag
	}{
		{
			name:     "empty secrecy tags",
			tags:     []Tag{},
			expected: []Tag{},
		},
		{
			name:     "single secrecy tag",
			tags:     []Tag{"confidential"},
			expected: []Tag{"confidential"},
		},
		{
			name:     "multiple secrecy tags",
			tags:     []Tag{"private", "secret", "confidential"},
			expected: []Tag{"private", "secret", "confidential"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewAgentLabels("test-agent")
			for _, tag := range tt.tags {
				agent.AddSecrecyTag(tag)
			}

			result := agent.GetSecrecyTags()
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

// TestAgentLabels_GetIntegrityTags tests thread-safe retrieval of integrity tags
func TestAgentLabels_GetIntegrityTags(t *testing.T) {
	tests := []struct {
		name     string
		tags     []Tag
		expected []Tag
	}{
		{
			name:     "empty integrity tags",
			tags:     []Tag{},
			expected: []Tag{},
		},
		{
			name:     "single integrity tag",
			tags:     []Tag{"trusted"},
			expected: []Tag{"trusted"},
		},
		{
			name:     "multiple integrity tags",
			tags:     []Tag{"verified", "production", "high-trust"},
			expected: []Tag{"verified", "production", "high-trust"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewAgentLabels("test-agent")
			for _, tag := range tt.tags {
				agent.AddIntegrityTag(tag)
			}

			result := agent.GetIntegrityTags()
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

// TestAgentLabels_DropIntegrityTag tests removal of integrity tags
func TestAgentLabels_DropIntegrityTag(t *testing.T) {
	tests := []struct {
		name     string
		initial  []Tag
		drop     Tag
		expected []Tag
	}{
		{
			name:     "drop from empty set",
			initial:  []Tag{},
			drop:     "nonexistent",
			expected: []Tag{},
		},
		{
			name:     "drop existing tag",
			initial:  []Tag{"trusted", "verified", "production"},
			drop:     "verified",
			expected: []Tag{"trusted", "production"},
		},
		{
			name:     "drop nonexistent tag",
			initial:  []Tag{"trusted", "verified"},
			drop:     "nonexistent",
			expected: []Tag{"trusted", "verified"},
		},
		{
			name:     "drop last tag",
			initial:  []Tag{"only-tag"},
			drop:     "only-tag",
			expected: []Tag{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewAgentLabels("test-agent")
			for _, tag := range tt.initial {
				agent.AddIntegrityTag(tag)
			}

			agent.DropIntegrityTag(tt.drop)
			result := agent.GetIntegrityTags()
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

// TestAgentLabels_ConcurrentAccess tests thread safety of agent label operations
func TestAgentLabels_ConcurrentAccess(t *testing.T) {
	agent := NewAgentLabels("concurrent-agent")
	var wg sync.WaitGroup
	iterations := 100

	// Concurrent writes
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			agent.AddSecrecyTag(Tag("secret"))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			agent.AddIntegrityTag(Tag("trusted"))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			agent.DropIntegrityTag(Tag("trusted"))
		}
	}()

	// Concurrent reads
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = agent.GetSecrecyTags()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = agent.GetIntegrityTags()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = agent.Clone()
		}
	}()

	wg.Wait()
	// If we get here without deadlock or race, test passes
	assert.NotNil(t, agent)
}

// TestAgentLabels_ConcurrentBulkMutations tests that AddSecrecyTags/DropIntegrityTags
// are safe to call concurrently with direct Label reads (GetTags/Contains).
// This specifically exercises the race fixed by switching from direct map mutation
// to Label.AddAll/RemoveAll which hold Label.mu.
func TestAgentLabels_ConcurrentBulkMutations(t *testing.T) {
	agent := NewAgentLabelsWithTags(
		"bulk-agent",
		[]Tag{"s1", "s2", "s3"},
		[]Tag{"i1", "i2", "i3"},
	)
	var wg sync.WaitGroup
	iterations := 200

	// Bulk mutations via the previously-racy methods
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			agent.AddSecrecyTags([]Tag{"s4", "s5"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			agent.DropIntegrityTags([]Tag{"i1", "i2"})
		}
	}()

	// Concurrent direct Label reads — these held Label.mu and could race with
	// the direct map writes that AddSecrecyTags/DropIntegrityTags previously did.
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = agent.Secrecy.Label.GetTags()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = agent.Integrity.Label.GetTags()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = agent.Secrecy.Label.Contains("s1")
			_ = agent.Integrity.Label.Contains("i3")
		}
	}()

	wg.Wait()
	assert.NotNil(t, agent)
}

// TestAgentRegistry_GetOrCreate tests the core registry functionality
func TestAgentRegistry_GetOrCreate(t *testing.T) {
	tests := []struct {
		name             string
		agentID          string
		defaultSecrecy   []Tag
		defaultIntegrity []Tag
		assertResult     func(*testing.T, *AgentRegistry, *AgentLabels)
	}{
		{
			name:    "create new agent with no defaults",
			agentID: "new-agent-1",
			assertResult: func(t *testing.T, registry *AgentRegistry, agent *AgentLabels) {
				assert.Equal(t, "new-agent-1", agent.AgentID)
				assert.Empty(t, agent.GetSecrecyTags())
				assert.Empty(t, agent.GetIntegrityTags())
				assert.Equal(t, 1, registry.Count())
			},
		},
		{
			name:             "create new agent with default secrecy",
			agentID:          "new-agent-2",
			defaultSecrecy:   []Tag{"default-secret"},
			defaultIntegrity: []Tag{},
			assertResult: func(t *testing.T, registry *AgentRegistry, agent *AgentLabels) {
				assert.Equal(t, "new-agent-2", agent.AgentID)
				assert.ElementsMatch(t, []Tag{"default-secret"}, agent.GetSecrecyTags())
				assert.Empty(t, agent.GetIntegrityTags())
			},
		},
		{
			name:             "create new agent with default integrity",
			agentID:          "new-agent-3",
			defaultSecrecy:   []Tag{},
			defaultIntegrity: []Tag{"default-trust"},
			assertResult: func(t *testing.T, registry *AgentRegistry, agent *AgentLabels) {
				assert.Equal(t, "new-agent-3", agent.AgentID)
				assert.Empty(t, agent.GetSecrecyTags())
				assert.ElementsMatch(t, []Tag{"default-trust"}, agent.GetIntegrityTags())
			},
		},
		{
			name:             "create new agent with both defaults",
			agentID:          "new-agent-4",
			defaultSecrecy:   []Tag{"default-secret", "default-private"},
			defaultIntegrity: []Tag{"default-trust", "default-verified"},
			assertResult: func(t *testing.T, registry *AgentRegistry, agent *AgentLabels) {
				assert.Equal(t, "new-agent-4", agent.AgentID)
				assert.ElementsMatch(t, []Tag{"default-secret", "default-private"}, agent.GetSecrecyTags())
				assert.ElementsMatch(t, []Tag{"default-trust", "default-verified"}, agent.GetIntegrityTags())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewAgentRegistryWithDefaults(tt.defaultSecrecy, tt.defaultIntegrity)
			agent := registry.GetOrCreate(tt.agentID)
			tt.assertResult(t, registry, agent)
		})
	}
}

// TestAgentRegistry_GetOrCreate_ReturnsExisting tests that existing agents are returned
func TestAgentRegistry_GetOrCreate_ReturnsExisting(t *testing.T) {
	registry := NewAgentRegistry()

	// Create agent
	agent1 := registry.GetOrCreate("existing-agent")
	agent1.AddSecrecyTag("secret")
	agent1.AddIntegrityTag("trusted")

	// Get same agent again
	agent2 := registry.GetOrCreate("existing-agent")

	// Should be the exact same instance
	assert.Equal(t, agent1, agent2)
	assert.Equal(t, "existing-agent", agent2.AgentID)
	assert.ElementsMatch(t, []Tag{"secret"}, agent2.GetSecrecyTags())
	assert.ElementsMatch(t, []Tag{"trusted"}, agent2.GetIntegrityTags())
	assert.Equal(t, 1, registry.Count(), "Should still have only 1 agent")
}

// TestAgentRegistry_GetOrCreate_Concurrent tests thread safety of GetOrCreate
func TestAgentRegistry_GetOrCreate_Concurrent(t *testing.T) {
	registry := NewAgentRegistryWithDefaults(
		[]Tag{"default-secret"},
		[]Tag{"default-trust"},
	)

	var wg sync.WaitGroup
	goroutines := 100
	agentID := "concurrent-agent"

	// Multiple goroutines trying to create the same agent
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			agent := registry.GetOrCreate(agentID)
			assert.NotNil(t, agent)
			assert.Equal(t, agentID, agent.AgentID)
		}()
	}

	wg.Wait()

	// Should have created exactly one agent despite concurrent access
	assert.Equal(t, 1, registry.Count())
	agent, ok := registry.Get(agentID)
	require.True(t, ok)
	assert.Equal(t, agentID, agent.AgentID)
}

// TestAgentRegistry_Get tests retrieving agents from registry
func TestAgentRegistry_Get(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*AgentRegistry) string
		agentID   string
		wantFound bool
	}{
		{
			name: "get existing agent",
			setup: func(r *AgentRegistry) string {
				agent := r.GetOrCreate("existing-agent")
				return agent.AgentID
			},
			agentID:   "existing-agent",
			wantFound: true,
		},
		{
			name: "get nonexistent agent",
			setup: func(r *AgentRegistry) string {
				return "nonexistent"
			},
			agentID:   "nonexistent",
			wantFound: false,
		},
		{
			name: "get agent after registration",
			setup: func(r *AgentRegistry) string {
				r.Register("registered-agent", []Tag{"secret"}, []Tag{"trust"})
				return "registered-agent"
			},
			agentID:   "registered-agent",
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewAgentRegistry()
			tt.setup(registry)

			agent, found := registry.Get(tt.agentID)
			assert.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				require.NotNil(t, agent)
				assert.Equal(t, tt.agentID, agent.AgentID)
			} else {
				assert.Nil(t, agent)
			}
		})
	}
}

// TestAgentRegistry_Register tests explicit agent registration
func TestAgentRegistry_Register(t *testing.T) {
	tests := []struct {
		name          string
		agentID       string
		secrecyTags   []Tag
		integrityTags []Tag
		assertAgent   func(*testing.T, *AgentLabels)
	}{
		{
			name:          "register with empty tags",
			agentID:       "agent-1",
			secrecyTags:   []Tag{},
			integrityTags: []Tag{},
			assertAgent: func(t *testing.T, agent *AgentLabels) {
				assert.Empty(t, agent.GetSecrecyTags())
				assert.Empty(t, agent.GetIntegrityTags())
			},
		},
		{
			name:          "register with secrecy tags only",
			agentID:       "agent-2",
			secrecyTags:   []Tag{"private", "confidential"},
			integrityTags: []Tag{},
			assertAgent: func(t *testing.T, agent *AgentLabels) {
				assert.ElementsMatch(t, []Tag{"private", "confidential"}, agent.GetSecrecyTags())
				assert.Empty(t, agent.GetIntegrityTags())
			},
		},
		{
			name:          "register with integrity tags only",
			agentID:       "agent-3",
			secrecyTags:   []Tag{},
			integrityTags: []Tag{"trusted", "verified"},
			assertAgent: func(t *testing.T, agent *AgentLabels) {
				assert.Empty(t, agent.GetSecrecyTags())
				assert.ElementsMatch(t, []Tag{"trusted", "verified"}, agent.GetIntegrityTags())
			},
		},
		{
			name:          "register with both tag types",
			agentID:       "agent-4",
			secrecyTags:   []Tag{"secret"},
			integrityTags: []Tag{"production"},
			assertAgent: func(t *testing.T, agent *AgentLabels) {
				assert.ElementsMatch(t, []Tag{"secret"}, agent.GetSecrecyTags())
				assert.ElementsMatch(t, []Tag{"production"}, agent.GetIntegrityTags())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewAgentRegistry()
			agent := registry.Register(tt.agentID, tt.secrecyTags, tt.integrityTags)

			assert.Equal(t, tt.agentID, agent.AgentID)
			tt.assertAgent(t, agent)

			// Verify agent is in registry
			retrievedAgent, found := registry.Get(tt.agentID)
			require.True(t, found)
			assert.Equal(t, agent, retrievedAgent)
		})
	}
}

// TestAgentRegistry_Register_Overwrites tests that Register replaces existing agents
func TestAgentRegistry_Register_Overwrites(t *testing.T) {
	registry := NewAgentRegistry()

	// Create initial agent
	agent1 := registry.Register("agent", []Tag{"initial-secret"}, []Tag{"initial-trust"})
	assert.ElementsMatch(t, []Tag{"initial-secret"}, agent1.GetSecrecyTags())

	// Register again with different tags
	agent2 := registry.Register("agent", []Tag{"new-secret"}, []Tag{"new-trust"})
	assert.ElementsMatch(t, []Tag{"new-secret"}, agent2.GetSecrecyTags())

	// Should have replaced the agent
	assert.NotEqual(t, agent1, agent2)
	assert.Equal(t, 1, registry.Count())

	// Retrieved agent should be the new one
	retrieved, found := registry.Get("agent")
	require.True(t, found)
	assert.Equal(t, agent2, retrieved)
}

// TestAgentRegistry_Remove tests agent removal from registry
func TestAgentRegistry_Remove(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*AgentRegistry) []string
		removeID      string
		expectedCount int
		assertRemoved func(*testing.T, *AgentRegistry)
	}{
		{
			name: "remove existing agent",
			setup: func(r *AgentRegistry) []string {
				r.GetOrCreate("agent-1")
				r.GetOrCreate("agent-2")
				return []string{"agent-1", "agent-2"}
			},
			removeID:      "agent-1",
			expectedCount: 1,
			assertRemoved: func(t *testing.T, r *AgentRegistry) {
				_, found := r.Get("agent-1")
				assert.False(t, found)
				_, found = r.Get("agent-2")
				assert.True(t, found)
			},
		},
		{
			name: "remove nonexistent agent",
			setup: func(r *AgentRegistry) []string {
				r.GetOrCreate("agent-1")
				return []string{"agent-1"}
			},
			removeID:      "nonexistent",
			expectedCount: 1,
			assertRemoved: func(t *testing.T, r *AgentRegistry) {
				_, found := r.Get("agent-1")
				assert.True(t, found)
			},
		},
		{
			name: "remove from empty registry",
			setup: func(r *AgentRegistry) []string {
				return []string{}
			},
			removeID:      "any-agent",
			expectedCount: 0,
			assertRemoved: func(t *testing.T, r *AgentRegistry) {
				_, found := r.Get("any-agent")
				assert.False(t, found)
			},
		},
		{
			name: "remove last agent",
			setup: func(r *AgentRegistry) []string {
				r.GetOrCreate("only-agent")
				return []string{"only-agent"}
			},
			removeID:      "only-agent",
			expectedCount: 0,
			assertRemoved: func(t *testing.T, r *AgentRegistry) {
				_, found := r.Get("only-agent")
				assert.False(t, found)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewAgentRegistry()
			tt.setup(registry)

			registry.Remove(tt.removeID)

			assert.Equal(t, tt.expectedCount, registry.Count())
			tt.assertRemoved(t, registry)
		})
	}
}

// TestAgentRegistry_Count tests counting registered agents
func TestAgentRegistry_Count(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*AgentRegistry)
		expectedCount int
	}{
		{
			name:          "empty registry",
			setup:         func(r *AgentRegistry) {},
			expectedCount: 0,
		},
		{
			name: "single agent",
			setup: func(r *AgentRegistry) {
				r.GetOrCreate("agent-1")
			},
			expectedCount: 1,
		},
		{
			name: "multiple agents",
			setup: func(r *AgentRegistry) {
				r.GetOrCreate("agent-1")
				r.GetOrCreate("agent-2")
				r.GetOrCreate("agent-3")
			},
			expectedCount: 3,
		},
		{
			name: "after removal",
			setup: func(r *AgentRegistry) {
				r.GetOrCreate("agent-1")
				r.GetOrCreate("agent-2")
				r.Remove("agent-1")
			},
			expectedCount: 1,
		},
		{
			name: "duplicate GetOrCreate",
			setup: func(r *AgentRegistry) {
				r.GetOrCreate("agent-1")
				r.GetOrCreate("agent-1")
				r.GetOrCreate("agent-1")
			},
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewAgentRegistry()
			tt.setup(registry)
			assert.Equal(t, tt.expectedCount, registry.Count())
		})
	}
}

// TestAgentRegistry_GetAllAgentIDs tests retrieving all agent IDs
func TestAgentRegistry_GetAllAgentIDs(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*AgentRegistry)
		expectedIDs []string
	}{
		{
			name:        "empty registry",
			setup:       func(r *AgentRegistry) {},
			expectedIDs: []string{},
		},
		{
			name: "single agent",
			setup: func(r *AgentRegistry) {
				r.GetOrCreate("agent-1")
			},
			expectedIDs: []string{"agent-1"},
		},
		{
			name: "multiple agents",
			setup: func(r *AgentRegistry) {
				r.GetOrCreate("agent-1")
				r.GetOrCreate("agent-2")
				r.GetOrCreate("agent-3")
			},
			expectedIDs: []string{"agent-1", "agent-2", "agent-3"},
		},
		{
			name: "after mixed operations",
			setup: func(r *AgentRegistry) {
				r.GetOrCreate("agent-1")
				r.Register("agent-2", []Tag{"secret"}, []Tag{"trust"})
				r.GetOrCreate("agent-3")
				r.Remove("agent-2")
			},
			expectedIDs: []string{"agent-1", "agent-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewAgentRegistry()
			tt.setup(registry)
			ids := registry.GetAllAgentIDs()
			assert.ElementsMatch(t, tt.expectedIDs, ids)
		})
	}
}

// TestAgentRegistry_SetDefaultLabels tests updating default labels
func TestAgentRegistry_SetDefaultLabels(t *testing.T) {
	tests := []struct {
		name             string
		initialSecrecy   []Tag
		initialIntegrity []Tag
		newSecrecy       []Tag
		newIntegrity     []Tag
		assertDefaults   func(*testing.T, *AgentRegistry)
	}{
		{
			name:             "set defaults on empty registry",
			initialSecrecy:   []Tag{},
			initialIntegrity: []Tag{},
			newSecrecy:       []Tag{"new-secret"},
			newIntegrity:     []Tag{"new-trust"},
			assertDefaults: func(t *testing.T, r *AgentRegistry) {
				// Create new agent to verify defaults applied
				agent := r.GetOrCreate("test-agent")
				assert.ElementsMatch(t, []Tag{"new-secret"}, agent.GetSecrecyTags())
				assert.ElementsMatch(t, []Tag{"new-trust"}, agent.GetIntegrityTags())
			},
		},
		{
			name:             "update existing defaults",
			initialSecrecy:   []Tag{"old-secret"},
			initialIntegrity: []Tag{"old-trust"},
			newSecrecy:       []Tag{"updated-secret"},
			newIntegrity:     []Tag{"updated-trust"},
			assertDefaults: func(t *testing.T, r *AgentRegistry) {
				// Create new agent to verify new defaults applied
				agent := r.GetOrCreate("test-agent")
				assert.ElementsMatch(t, []Tag{"updated-secret"}, agent.GetSecrecyTags())
				assert.ElementsMatch(t, []Tag{"updated-trust"}, agent.GetIntegrityTags())
			},
		},
		{
			name:             "clear defaults",
			initialSecrecy:   []Tag{"secret"},
			initialIntegrity: []Tag{"trust"},
			newSecrecy:       []Tag{},
			newIntegrity:     []Tag{},
			assertDefaults: func(t *testing.T, r *AgentRegistry) {
				agent := r.GetOrCreate("test-agent")
				assert.Empty(t, agent.GetSecrecyTags())
				assert.Empty(t, agent.GetIntegrityTags())
			},
		},
		{
			name:             "set multiple default tags",
			initialSecrecy:   []Tag{},
			initialIntegrity: []Tag{},
			newSecrecy:       []Tag{"secret-1", "secret-2", "secret-3"},
			newIntegrity:     []Tag{"trust-1", "trust-2"},
			assertDefaults: func(t *testing.T, r *AgentRegistry) {
				agent := r.GetOrCreate("test-agent")
				assert.ElementsMatch(t, []Tag{"secret-1", "secret-2", "secret-3"}, agent.GetSecrecyTags())
				assert.ElementsMatch(t, []Tag{"trust-1", "trust-2"}, agent.GetIntegrityTags())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewAgentRegistryWithDefaults(tt.initialSecrecy, tt.initialIntegrity)
			registry.SetDefaultLabels(tt.newSecrecy, tt.newIntegrity)
			tt.assertDefaults(t, registry)
		})
	}
}

// TestAgentRegistry_SetDefaultLabels_DoesNotAffectExisting tests that changing
// defaults doesn't affect already registered agents
func TestAgentRegistry_SetDefaultLabels_DoesNotAffectExisting(t *testing.T) {
	registry := NewAgentRegistryWithDefaults(
		[]Tag{"initial-secret"},
		[]Tag{"initial-trust"},
	)

	// Create an agent with initial defaults
	existingAgent := registry.GetOrCreate("existing-agent")
	assert.ElementsMatch(t, []Tag{"initial-secret"}, existingAgent.GetSecrecyTags())
	assert.ElementsMatch(t, []Tag{"initial-trust"}, existingAgent.GetIntegrityTags())

	// Change defaults
	registry.SetDefaultLabels([]Tag{"new-secret"}, []Tag{"new-trust"})

	// Existing agent should not be affected
	assert.ElementsMatch(t, []Tag{"initial-secret"}, existingAgent.GetSecrecyTags())
	assert.ElementsMatch(t, []Tag{"initial-trust"}, existingAgent.GetIntegrityTags())

	// But new agents should get new defaults
	newAgent := registry.GetOrCreate("new-agent")
	assert.ElementsMatch(t, []Tag{"new-secret"}, newAgent.GetSecrecyTags())
	assert.ElementsMatch(t, []Tag{"new-trust"}, newAgent.GetIntegrityTags())
}

// TestAgentLabels_AddSecrecyTags tests adding multiple secrecy tags at once
func TestAgentLabels_AddSecrecyTags(t *testing.T) {
	t.Run("adds multiple tags", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")
		agent.AddSecrecyTags([]Tag{"secret", "classified", "confidential"})

		tags := agent.GetSecrecyTags()
		assert.Len(t, tags, 3)
		assert.Contains(t, tags, Tag("secret"))
		assert.Contains(t, tags, Tag("classified"))
		assert.Contains(t, tags, Tag("confidential"))
	})

	t.Run("adding empty slice does nothing", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")
		agent.AddSecrecyTags([]Tag{})
		assert.Empty(t, agent.GetSecrecyTags())
	})

	t.Run("adding duplicate tags is idempotent", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")
		agent.AddSecrecyTag("secret")
		agent.AddSecrecyTags([]Tag{"secret", "secret"})
		assert.Len(t, agent.GetSecrecyTags(), 1)
	})
}

// TestAgentLabels_DropIntegrityTags tests dropping multiple integrity tags at once
func TestAgentLabels_DropIntegrityTags(t *testing.T) {
	t.Run("drops multiple tags", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")
		agent.AddIntegrityTag("trusted")
		agent.AddIntegrityTag("verified")
		agent.AddIntegrityTag("production")

		agent.DropIntegrityTags([]Tag{"trusted", "verified"})

		tags := agent.GetIntegrityTags()
		assert.Len(t, tags, 1)
		assert.Contains(t, tags, Tag("production"))
	})

	t.Run("dropping non-existent tags is safe", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")
		agent.AddIntegrityTag("trusted")

		agent.DropIntegrityTags([]Tag{"nonexistent", "alsononexistent"})

		tags := agent.GetIntegrityTags()
		assert.Len(t, tags, 1)
		assert.Contains(t, tags, Tag("trusted"))
	})

	t.Run("dropping empty slice does nothing", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")
		agent.AddIntegrityTag("trusted")
		agent.DropIntegrityTags([]Tag{})
		assert.Len(t, agent.GetIntegrityTags(), 1)
	})
}

// TestAgentLabels_ApplyPropagation tests the ApplyPropagation method
func TestAgentLabels_ApplyPropagation(t *testing.T) {
	t.Run("applies secrecy propagation", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")

		result := &EvaluationResult{
			Decision:        AccessAllowWithPropagate,
			SecrecyToAdd:    []Tag{"secret", "classified"},
			IntegrityToDrop: []Tag{},
		}

		changed := agent.ApplyPropagation(result)

		assert.True(t, changed, "Labels should have changed")
		tags := agent.GetSecrecyTags()
		assert.Contains(t, tags, Tag("secret"))
		assert.Contains(t, tags, Tag("classified"))
	})

	t.Run("applies integrity propagation", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")
		agent.AddIntegrityTag("trusted")
		agent.AddIntegrityTag("verified")
		agent.AddIntegrityTag("production")

		result := &EvaluationResult{
			Decision:        AccessAllowWithPropagate,
			SecrecyToAdd:    []Tag{},
			IntegrityToDrop: []Tag{"trusted", "verified"},
		}

		changed := agent.ApplyPropagation(result)

		assert.True(t, changed, "Labels should have changed")
		tags := agent.GetIntegrityTags()
		assert.Len(t, tags, 1)
		assert.Contains(t, tags, Tag("production"))
	})

	t.Run("applies both secrecy and integrity propagation", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")
		agent.AddIntegrityTag("trusted")

		result := &EvaluationResult{
			Decision:        AccessAllowWithPropagate,
			SecrecyToAdd:    []Tag{"secret"},
			IntegrityToDrop: []Tag{"trusted"},
		}

		changed := agent.ApplyPropagation(result)

		assert.True(t, changed, "Labels should have changed")
		assert.Contains(t, agent.GetSecrecyTags(), Tag("secret"))
		assert.Empty(t, agent.GetIntegrityTags())
	})

	t.Run("returns false for nil result", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")
		changed := agent.ApplyPropagation(nil)
		assert.False(t, changed)
	})

	t.Run("returns false for non-propagate decision", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")

		result := &EvaluationResult{
			Decision:        AccessAllow,
			SecrecyToAdd:    []Tag{"should-not-be-added"},
			IntegrityToDrop: []Tag{"should-not-be-dropped"},
		}

		changed := agent.ApplyPropagation(result)

		assert.False(t, changed, "Should not apply for non-propagate decisions")
		assert.Empty(t, agent.GetSecrecyTags())
	})

	t.Run("returns false when no changes needed", func(t *testing.T) {
		agent := NewAgentLabels("test-agent")

		result := &EvaluationResult{
			Decision:        AccessAllowWithPropagate,
			SecrecyToAdd:    []Tag{},
			IntegrityToDrop: []Tag{},
		}

		changed := agent.ApplyPropagation(result)

		assert.False(t, changed, "Should return false when no changes needed")
	})
}

// TestPropagateMode_EndToEnd tests the complete propagation workflow
func TestPropagateMode_EndToEnd(t *testing.T) {
	t.Run("reading secret data taints agent and blocks public writes", func(t *testing.T) {
		// Create propagate mode evaluator
		eval := NewEvaluatorWithMode(EnforcementPropagate)

		// Agent starts with no labels
		agent := NewAgentLabels("demo-agent")

		// First, agent reads a secret resource
		secretResource := NewLabeledResource("secret-document")
		secretResource.Secrecy.Label.Add("secret")

		readResult := eval.Evaluate(agent.Secrecy, agent.Integrity, secretResource, OperationRead)
		assert.True(t, readResult.IsAllowed(), "Read should be allowed in propagate mode")

		// Apply propagation
		agent.ApplyPropagation(readResult)
		assert.Contains(t, agent.GetSecrecyTags(), Tag("secret"), "Agent should now have secret tag")

		// Now try to write to public resource
		publicResource := NewLabeledResource("public-internet")

		writeResult := eval.Evaluate(agent.Secrecy, agent.Integrity, publicResource, OperationWrite)
		assert.False(t, writeResult.IsAllowed(), "Write to public should be blocked after reading secret")
	})

	t.Run("reading untrusted data drops integrity and blocks high-integrity writes", func(t *testing.T) {
		// Create propagate mode evaluator
		eval := NewEvaluatorWithMode(EnforcementPropagate)

		// Agent starts with high integrity
		agent := NewAgentLabels("demo-agent")
		agent.AddIntegrityTag("trusted")
		agent.AddIntegrityTag("verified")

		// First, agent reads an untrusted resource
		untrustedResource := NewLabeledResource("random-internet-page")

		readResult := eval.Evaluate(agent.Secrecy, agent.Integrity, untrustedResource, OperationRead)
		assert.True(t, readResult.IsAllowed(), "Read should be allowed in propagate mode")

		// Apply propagation
		agent.ApplyPropagation(readResult)
		assert.Empty(t, agent.GetIntegrityTags(), "Agent should have lost all integrity tags")

		// Now try to write to high-integrity resource
		productionDB := NewLabeledResource("production-database")
		productionDB.Integrity.Label.Add("trusted")

		writeResult := eval.Evaluate(agent.Secrecy, agent.Integrity, productionDB, OperationWrite)
		assert.False(t, writeResult.IsAllowed(), "Write to production should be blocked after reading untrusted")
	})
}

// TestAgentLabels_AddIntegrityTags tests the AddIntegrityTags method which was
// previously at 0% coverage.
func TestAgentLabels_AddIntegrityTags(t *testing.T) {
	tests := []struct {
		name     string
		initial  []Tag
		add      []Tag
		wantTags []Tag
	}{
		{
			name:     "add tags to empty integrity label",
			initial:  nil,
			add:      []Tag{"verified", "trusted"},
			wantTags: []Tag{"verified", "trusted"},
		},
		{
			name:     "add tags to non-empty integrity label",
			initial:  []Tag{"existing"},
			add:      []Tag{"new-tag"},
			wantTags: []Tag{"existing", "new-tag"},
		},
		{
			name:     "add empty slice is a no-op",
			initial:  []Tag{"keep"},
			add:      []Tag{},
			wantTags: []Tag{"keep"},
		},
		{
			name:     "add nil slice is a no-op",
			initial:  []Tag{"keep"},
			add:      nil,
			wantTags: []Tag{"keep"},
		},
		{
			name:     "add duplicate tags does not create duplicates",
			initial:  []Tag{"trusted"},
			add:      []Tag{"trusted", "verified"},
			wantTags: []Tag{"trusted", "verified"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewAgentLabels("test-agent")
			for _, tag := range tt.initial {
				agent.AddIntegrityTag(tag)
			}

			agent.AddIntegrityTags(tt.add)

			got := agent.GetIntegrityTags()
			assert.ElementsMatch(t, tt.wantTags, got)
		})
	}
}

// TestAgentLabels_AddIntegrityTags_IsIndependentOfSecrecy verifies that
// AddIntegrityTags only modifies integrity labels and leaves secrecy unchanged.
func TestAgentLabels_AddIntegrityTags_IsIndependentOfSecrecy(t *testing.T) {
	agent := NewAgentLabels("agent")
	agent.AddSecrecyTag("confidential")

	agent.AddIntegrityTags([]Tag{"verified", "trusted"})

	// Integrity should have new tags
	assert.ElementsMatch(t, []Tag{"verified", "trusted"}, agent.GetIntegrityTags())
	// Secrecy should be unchanged
	assert.ElementsMatch(t, []Tag{"confidential"}, agent.GetSecrecyTags())
}

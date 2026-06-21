package difc

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildReflectResponse_EmptyRegistry(t *testing.T) {
	resp := BuildReflectResponse(DIFCComponents{
		Mode:          EnforcementFilter,
		AgentRegistry: NewAgentRegistry(),
	})

	require.NotNil(t, resp.Agents)
	assert.Empty(t, resp.Agents)
	assert.Equal(t, "filter", resp.Mode)
	_, err := time.Parse(time.RFC3339, resp.Timestamp)
	assert.NoError(t, err)
}

func TestBuildReflectResponse_SkipsNilAgentEntries(t *testing.T) {
	reg := NewAgentRegistry()
	reg.agents["broken"] = nil

	resp := BuildReflectResponse(DIFCComponents{
		Mode:          EnforcementStrict,
		AgentRegistry: reg,
	})

	assert.NotContains(t, resp.Agents, "broken")
}

// TestBuildReflectResponse_NilRegistry verifies that a nil AgentRegistry
// returns a valid response with an empty agents map rather than panicking.
func TestBuildReflectResponse_NilRegistry(t *testing.T) {
	resp := BuildReflectResponse(DIFCComponents{
		Mode:          EnforcementStrict,
		AgentRegistry: nil,
	})

	require.NotNil(t, resp.Agents)
	assert.Empty(t, resp.Agents)
	assert.Equal(t, "strict", resp.Mode)
	_, err := time.Parse(time.RFC3339, resp.Timestamp)
	assert.NoError(t, err)
}

// TestBuildReflectResponse_AgentWithSecrecyTags verifies that an agent with
// secrecy tags is reflected correctly, exercising the tagsToStrings code path.
func TestBuildReflectResponse_AgentWithSecrecyTags(t *testing.T) {
	reg := NewAgentRegistry()
	reg.Register("agent-1", []Tag{"private:owner/repo", "secret"}, nil)

	resp := BuildReflectResponse(DIFCComponents{
		Mode:          EnforcementStrict,
		AgentRegistry: reg,
	})

	require.Contains(t, resp.Agents, "agent-1")
	labels := resp.Agents["agent-1"]
	assert.Len(t, labels.Secrecy, 2)
	assert.Empty(t, labels.Integrity)
	// Tags must be sorted alphabetically.
	assert.True(t, sort.StringsAreSorted(labels.Secrecy),
		"secrecy tags should be sorted; got %v", labels.Secrecy)
	assert.Contains(t, labels.Secrecy, "private:owner/repo")
	assert.Contains(t, labels.Secrecy, "secret")
}

// TestBuildReflectResponse_AgentWithIntegrityTags verifies that an agent with
// integrity tags is reflected correctly, exercising the tagsToStrings code path.
func TestBuildReflectResponse_AgentWithIntegrityTags(t *testing.T) {
	reg := NewAgentRegistry()
	reg.Register("agent-2", nil, []Tag{"merged:all", "approved:all"})

	resp := BuildReflectResponse(DIFCComponents{
		Mode:          EnforcementFilter,
		AgentRegistry: reg,
	})

	require.Contains(t, resp.Agents, "agent-2")
	labels := resp.Agents["agent-2"]
	assert.Empty(t, labels.Secrecy)
	assert.Len(t, labels.Integrity, 2)
	assert.True(t, sort.StringsAreSorted(labels.Integrity),
		"integrity tags should be sorted; got %v", labels.Integrity)
	assert.Contains(t, labels.Integrity, "merged:all")
	assert.Contains(t, labels.Integrity, "approved:all")
}

// TestBuildReflectResponse_AgentWithBothTagTypes verifies that an agent with
// both secrecy and integrity tags has them reflected and sorted independently.
func TestBuildReflectResponse_AgentWithBothTagTypes(t *testing.T) {
	reg := NewAgentRegistry()
	reg.Register("agent-3",
		[]Tag{"repo:org/b", "repo:org/a"},
		[]Tag{"merged:all", "approved:all", "unapproved:all"},
	)

	resp := BuildReflectResponse(DIFCComponents{
		Mode:          EnforcementPropagate,
		AgentRegistry: reg,
	})

	require.Contains(t, resp.Agents, "agent-3")
	labels := resp.Agents["agent-3"]
	assert.Len(t, labels.Secrecy, 2)
	assert.Len(t, labels.Integrity, 3)
	// Both slices must be independently sorted.
	assert.True(t, sort.StringsAreSorted(labels.Secrecy),
		"secrecy tags should be sorted; got %v", labels.Secrecy)
	assert.True(t, sort.StringsAreSorted(labels.Integrity),
		"integrity tags should be sorted; got %v", labels.Integrity)
	assert.Equal(t, "propagate", resp.Mode)
}

// TestBuildReflectResponse_MultipleAgents verifies that all registered agents
// appear in the reflect response with their correct labels.
func TestBuildReflectResponse_MultipleAgents(t *testing.T) {
	reg := NewAgentRegistry()
	reg.Register("alice", []Tag{"private:org/secret"}, []Tag{"merged:all"})
	reg.Register("bob", nil, []Tag{"approved:all"})
	reg.Register("charlie", []Tag{"secret"}, nil)

	resp := BuildReflectResponse(DIFCComponents{
		Mode:          EnforcementStrict,
		AgentRegistry: reg,
	})

	assert.Len(t, resp.Agents, 3)
	require.Contains(t, resp.Agents, "alice")
	require.Contains(t, resp.Agents, "bob")
	require.Contains(t, resp.Agents, "charlie")

	alice := resp.Agents["alice"]
	assert.Equal(t, []string{"private:org/secret"}, alice.Secrecy)
	assert.Equal(t, []string{"merged:all"}, alice.Integrity)

	bob := resp.Agents["bob"]
	assert.Empty(t, bob.Secrecy)
	assert.Equal(t, []string{"approved:all"}, bob.Integrity)

	charlie := resp.Agents["charlie"]
	assert.Equal(t, []string{"secret"}, charlie.Secrecy)
	assert.Empty(t, charlie.Integrity)
}

// TestBuildReflectResponse_AllEnforcementModes verifies that all three
// enforcement modes are reflected correctly in the Mode field.
func TestBuildReflectResponse_AllEnforcementModes(t *testing.T) {
	modes := []struct {
		mode EnforcementMode
		want string
	}{
		{EnforcementStrict, "strict"},
		{EnforcementFilter, "filter"},
		{EnforcementPropagate, "propagate"},
	}

	for _, tc := range modes {
		t.Run(tc.want, func(t *testing.T) {
			resp := BuildReflectResponse(DIFCComponents{
				Mode:          tc.mode,
				AgentRegistry: NewAgentRegistry(),
			})
			assert.Equal(t, tc.want, resp.Mode)
		})
	}
}

// TestBuildReflectResponse_TimestampIsRFC3339 verifies that the Timestamp
// field in the response is always a valid RFC3339 string.
func TestBuildReflectResponse_TimestampIsRFC3339(t *testing.T) {
	before := time.Now().UTC().Truncate(time.Second)
	resp := BuildReflectResponse(DIFCComponents{
		Mode:          EnforcementStrict,
		AgentRegistry: NewAgentRegistry(),
	})
	after := time.Now().UTC()

	ts, err := time.Parse(time.RFC3339, resp.Timestamp)
	require.NoError(t, err, "Timestamp should be valid RFC3339")
	assert.False(t, ts.Before(before), "Timestamp should not be before test start")
	assert.False(t, ts.After(after), "Timestamp should not be after test end")
}

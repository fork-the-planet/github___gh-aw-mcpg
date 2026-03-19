package difc

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetSinkServerIDs(t *testing.T) {
	// Reset state after each test
	t.Cleanup(func() {
		SetSinkServerIDs(nil)
	})

	t.Run("empty slice clears configuration", func(t *testing.T) {
		SetSinkServerIDs([]string{"server1"})
		SetSinkServerIDs([]string{})

		assert.False(t, IsSinkServerID("server1"))
	})

	t.Run("nil slice clears configuration", func(t *testing.T) {
		SetSinkServerIDs([]string{"server1"})
		SetSinkServerIDs(nil)

		assert.False(t, IsSinkServerID("server1"))
	})

	t.Run("configured server IDs match", func(t *testing.T) {
		SetSinkServerIDs([]string{"github", "slack"})

		assert.True(t, IsSinkServerID("github"))
		assert.True(t, IsSinkServerID("slack"))
	})

	t.Run("unconfigured server IDs do not match", func(t *testing.T) {
		SetSinkServerIDs([]string{"github"})

		assert.False(t, IsSinkServerID("slack"))
		assert.False(t, IsSinkServerID("unknown"))
	})

	t.Run("duplicate server IDs are deduplicated", func(t *testing.T) {
		SetSinkServerIDs([]string{"github", "github", "slack", "github"})

		assert.True(t, IsSinkServerID("github"))
		assert.True(t, IsSinkServerID("slack"))
	})

	t.Run("empty strings in input are skipped", func(t *testing.T) {
		SetSinkServerIDs([]string{"github", "", "slack", ""})

		assert.True(t, IsSinkServerID("github"))
		assert.True(t, IsSinkServerID("slack"))
		assert.False(t, IsSinkServerID(""))
	})

	t.Run("replaces previous configuration", func(t *testing.T) {
		SetSinkServerIDs([]string{"server1", "server2"})
		SetSinkServerIDs([]string{"server3"})

		assert.False(t, IsSinkServerID("server1"))
		assert.False(t, IsSinkServerID("server2"))
		assert.True(t, IsSinkServerID("server3"))
	})
}

func TestIsSinkServerID(t *testing.T) {
	t.Cleanup(func() {
		SetSinkServerIDs(nil)
	})

	t.Run("returns false when no sink IDs configured", func(t *testing.T) {
		SetSinkServerIDs(nil)

		assert.False(t, IsSinkServerID("any-server"))
	})

	t.Run("returns true for exact match", func(t *testing.T) {
		SetSinkServerIDs([]string{"my-server"})

		assert.True(t, IsSinkServerID("my-server"))
	})

	t.Run("returns false for partial match", func(t *testing.T) {
		SetSinkServerIDs([]string{"my-server"})

		assert.False(t, IsSinkServerID("my"))
		assert.False(t, IsSinkServerID("server"))
		assert.False(t, IsSinkServerID("my-server-extra"))
	})

	t.Run("is case sensitive", func(t *testing.T) {
		SetSinkServerIDs([]string{"GitHub"})

		assert.False(t, IsSinkServerID("github"))
		assert.False(t, IsSinkServerID("GITHUB"))
		assert.True(t, IsSinkServerID("GitHub"))
	})

	t.Run("concurrent access is safe", func(t *testing.T) {
		SetSinkServerIDs([]string{"concurrent-server"})

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = IsSinkServerID("concurrent-server")
			}()
		}
		wg.Wait()
	})
}

package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNonceCache_EvictExpired_EmptyCache verifies that calling evictExpired on an
// empty cache is a no-op and does not panic.
func TestNonceCache_EvictExpired_EmptyCache(t *testing.T) {
	c := newNonceCache()
	assert.Empty(t, c.entries, "cache should start empty")

	// Must not panic on empty cache
	c.evictExpired(time.Now())
	assert.Empty(t, c.entries, "cache should still be empty after eviction")
}

// TestNonceCache_EvictExpired_RemovesExpiredEntries verifies that entries whose
// deadline has passed are deleted.
func TestNonceCache_EvictExpired_RemovesExpiredEntries(t *testing.T) {
	c := newNonceCache()
	past := time.Now().Add(-1 * time.Second)

	// Insert entries with past deadlines directly so they are immediately expired.
	c.entries["expired-1"] = past
	c.entries["expired-2"] = past.Add(-5 * time.Second)

	require.Len(t, c.entries, 2, "should have 2 entries before eviction")

	c.evictExpired(time.Now())

	assert.Empty(t, c.entries, "all expired entries should be removed")
}

// TestNonceCache_EvictExpired_KeepsFreshEntries verifies that entries whose
// deadline is still in the future are retained.
func TestNonceCache_EvictExpired_KeepsFreshEntries(t *testing.T) {
	c := newNonceCache()
	future := time.Now().Add(60 * time.Second)

	c.entries["fresh-1"] = future
	c.entries["fresh-2"] = future.Add(10 * time.Second)

	require.Len(t, c.entries, 2, "should have 2 entries before eviction")

	c.evictExpired(time.Now())

	assert.Len(t, c.entries, 2, "fresh entries should not be removed")
	assert.Contains(t, c.entries, "fresh-1")
	assert.Contains(t, c.entries, "fresh-2")
}

// TestNonceCache_EvictExpired_MixedEntries verifies that only expired entries are
// removed and fresh entries remain when both exist in the cache.
func TestNonceCache_EvictExpired_MixedEntries(t *testing.T) {
	c := newNonceCache()
	now := time.Now()
	past := now.Add(-1 * time.Second)
	future := now.Add(60 * time.Second)

	c.entries["expired-a"] = past
	c.entries["expired-b"] = past.Add(-10 * time.Second)
	c.entries["fresh-a"] = future
	c.entries["fresh-b"] = future.Add(30 * time.Second)

	require.Len(t, c.entries, 4)

	c.evictExpired(now)

	assert.Len(t, c.entries, 2, "only fresh entries should remain")
	assert.NotContains(t, c.entries, "expired-a")
	assert.NotContains(t, c.entries, "expired-b")
	assert.Contains(t, c.entries, "fresh-a")
	assert.Contains(t, c.entries, "fresh-b")
}

// TestNonceCache_EvictExpired_ExactDeadline verifies the boundary condition where
// the deadline equals the current time. Because evictExpired uses now.After(deadline),
// an entry whose deadline equals `now` (i.e. now.After(deadline) == false) should be
// retained.
func TestNonceCache_EvictExpired_ExactDeadline(t *testing.T) {
	c := newNonceCache()
	now := time.Now()

	// Deadline exactly equals now — now.After(now) is false, so the entry should be kept.
	c.entries["exact-deadline"] = now

	c.evictExpired(now)

	assert.Contains(t, c.entries, "exact-deadline",
		"entry at exact deadline should not be evicted (now.After(deadline) is false when equal)")
}

// TestNonceCache_EvictExpired_JustExpired verifies that an entry one nanosecond in the
// past is evicted.
func TestNonceCache_EvictExpired_JustExpired(t *testing.T) {
	c := newNonceCache()
	now := time.Now()
	justBefore := now.Add(-1) // one nanosecond before now

	c.entries["just-expired"] = justBefore

	c.evictExpired(now)

	assert.NotContains(t, c.entries, "just-expired",
		"entry one nanosecond in the past should be evicted")
}

// TestNonceCache_EvictExpired_AllExpiredThenFreshAdded verifies that after evicting
// all expired entries, new entries can be added normally.
func TestNonceCache_EvictExpired_AllExpiredThenFreshAdded(t *testing.T) {
	c := newNonceCache()
	past := time.Now().Add(-1 * time.Second)

	c.entries["old-1"] = past
	c.entries["old-2"] = past

	c.evictExpired(time.Now())
	require.Empty(t, c.entries, "all entries should be evicted")

	// Now add a fresh entry via the public API
	assert.True(t, c.checkAndSet("new-nonce"), "should accept new nonce after full eviction")
	assert.Len(t, c.entries, 1)
}

// TestNonceCache_EvictExpired_ManyEntries verifies correctness with a large number
// of entries — half expired, half fresh.
func TestNonceCache_EvictExpired_ManyEntries(t *testing.T) {
	c := newNonceCache()
	now := time.Now()
	past := now.Add(-1 * time.Second)
	future := now.Add(60 * time.Second)

	const count = 100
	for i := range count {
		if i%2 == 0 {
			c.entries[fmt.Sprintf("expired-%d", i)] = past
		} else {
			c.entries[fmt.Sprintf("fresh-%d", i)] = future
		}
	}

	require.Len(t, c.entries, count)

	c.evictExpired(now)

	assert.Len(t, c.entries, count/2, "exactly half the entries (fresh) should remain")
	for i := range count {
		key := fmt.Sprintf("expired-%d", i)
		freshKey := fmt.Sprintf("fresh-%d", i)
		if i%2 == 0 {
			assert.NotContains(t, c.entries, key, "expired entry should be removed")
		} else {
			assert.Contains(t, c.entries, freshKey, "fresh entry should remain")
		}
	}
}

// TestNonceCache_EvictExpired_CalledByCheckAndSet verifies that evictExpired is
// triggered by checkAndSet: entries added with a past deadline are cleaned up on
// the next checkAndSet call.
func TestNonceCache_EvictExpired_CalledByCheckAndSet(t *testing.T) {
	c := newNonceCache()
	past := time.Now().Add(-1 * time.Second)

	// Directly plant an expired entry.
	c.entries["stale-nonce"] = past
	require.Len(t, c.entries, 1)

	// checkAndSet internally calls evictExpired — "stale-nonce" should be cleaned up.
	ok := c.checkAndSet("fresh-nonce")
	assert.True(t, ok, "fresh nonce should be accepted")

	assert.NotContains(t, c.entries, "stale-nonce", "expired entry should be evicted by checkAndSet")
	assert.Contains(t, c.entries, "fresh-nonce", "new entry should be present")
}

// TestNonceCache_EvictExpired_CalledBySeenNonce verifies that evictExpired is also
// triggered by seenNonce: expired entries are removed on every read path as well.
func TestNonceCache_EvictExpired_CalledBySeenNonce(t *testing.T) {
	c := newNonceCache()
	past := time.Now().Add(-1 * time.Second)
	future := time.Now().Add(60 * time.Second)

	c.entries["stale"] = past
	c.entries["live"] = future

	require.Len(t, c.entries, 2)

	// seenNonce on a non-existent nonce still triggers eviction.
	result := c.seenNonce("unknown-nonce")
	assert.False(t, result, "unseen nonce should return false")

	assert.NotContains(t, c.entries, "stale", "expired entry should be evicted by seenNonce")
	assert.Contains(t, c.entries, "live", "live entry should still be present")
}

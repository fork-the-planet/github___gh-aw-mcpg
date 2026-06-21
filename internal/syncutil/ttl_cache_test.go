// Package syncutil tests – internal test file to allow access to
// newTTLCacheWithClock for deterministic time injection.
package syncutil

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock is a monotonically-advancing fake clock for deterministic testing.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// TestTTLCache_GetOrCreate_ReturnsCachedValue verifies that a cached entry is
// returned without invoking create a second time.
func TestTTLCache_GetOrCreate_ReturnsCachedValue(t *testing.T) {
	cache := NewTTLCache[string, int](time.Hour, 100)

	createCount := 0
	first := cache.GetOrCreate("key", func() int { createCount++; return 42 })
	second := cache.GetOrCreate("key", func() int { createCount++; return 99 })

	assert.Equal(t, 42, first)
	assert.Equal(t, 42, second, "cached value should be returned on second call")
	assert.Equal(t, 1, createCount, "create should be called exactly once")
}

// TestTTLCache_GetOrCreate_MissingKey verifies that create is called for a new
// key and the result is stored.
func TestTTLCache_GetOrCreate_MissingKey(t *testing.T) {
	cache := NewTTLCache[string, int](time.Hour, 100)

	v := cache.GetOrCreate("key", func() int { return 7 })

	assert.Equal(t, 7, v)
	assert.Equal(t, 1, cache.Len())
}

// TestTTLCache_Len_MultipleKeys verifies that each unique key occupies a
// separate entry.
func TestTTLCache_Len_MultipleKeys(t *testing.T) {
	cache := NewTTLCache[string, int](time.Hour, 100)

	for _, k := range []string{"a", "b", "c"} {
		key := k
		cache.GetOrCreate(key, func() int { return len(key) })
	}

	assert.Equal(t, 3, cache.Len())
}

// TestTTLCache_TTLEviction verifies that entries older than the TTL are lazily
// evicted on the next GetOrCreate call.
func TestTTLCache_TTLEviction(t *testing.T) {
	clk := newFakeClock()
	cache := newTTLCacheWithClock[string, int](100*time.Millisecond, 100, clk.Now)

	createCount := 0
	cache.GetOrCreate("key", func() int { createCount++; return 1 })
	require.Equal(t, 1, createCount)

	// Advance past TTL.
	clk.Advance(200 * time.Millisecond)

	cache.GetOrCreate("other", func() int { return 99 }) // triggers lazy eviction
	assert.Equal(t, 1, cache.Len(), "evicted entry should be removed; only 'other' remains")

	// Accessing the evicted key again must create a new entry.
	cache.GetOrCreate("key", func() int { createCount++; return 2 })
	assert.Equal(t, 2, createCount, "create must be called again for the evicted key")
}

// TestTTLCache_TTLEviction_RealTime mirrors the existing TTL eviction test that
// used time.Sleep to ensure the behaviour is preserved.
func TestTTLCache_TTLEviction_RealTime(t *testing.T) {
	ttl := 100 * time.Millisecond
	cache := NewTTLCache[string, int](ttl, 100)

	createCount := 0
	cache.GetOrCreate("session1", func() int { createCount++; return 1 })
	assert.Equal(t, 1, createCount)
	assert.Equal(t, 1, cache.Len())

	// Wait for TTL to expire (use generous margin to avoid CI flakiness).
	time.Sleep(200 * time.Millisecond)

	// Accessing a different key should trigger eviction of session1.
	cache.GetOrCreate("session2", func() int { createCount++; return 2 })
	assert.Equal(t, 2, createCount, "create should be called for the new key")

	// After eviction the only remaining entry is session2.
	assert.Equal(t, 1, cache.Len(), "expired session1 should have been evicted")
}

// TestTTLCache_LRUEviction verifies that the least-recently-used entry is
// evicted when the cache reaches its maximum size.
func TestTTLCache_LRUEviction(t *testing.T) {
	clk := newFakeClock()
	cache := newTTLCacheWithClock[string, int](time.Hour, 3, clk.Now)

	createCount := 0
	creator := func(v int) func() int { return func() int { createCount++; return v } }

	// Insert 3 entries with deterministic timestamps.
	cache.GetOrCreate("key1", creator(1)) // lastUsed = T+0
	clk.Advance(time.Millisecond)
	cache.GetOrCreate("key2", creator(2)) // lastUsed = T+1ms
	clk.Advance(time.Millisecond)
	cache.GetOrCreate("key3", creator(3)) // lastUsed = T+2ms

	require.Equal(t, 3, createCount)
	require.Equal(t, 3, cache.Len())

	// Adding a fourth entry must evict key1 (LRU).
	clk.Advance(time.Millisecond)
	v4 := cache.GetOrCreate("key4", creator(4))
	assert.Equal(t, 4, createCount)
	assert.Equal(t, 4, v4)
	assert.Equal(t, 3, cache.Len(), "cache should remain at maxSize after LRU eviction")

	// key2, key3, key4 should still be present – verify no extra creates.
	cache.GetOrCreate("key2", creator(20))
	cache.GetOrCreate("key3", creator(30))
	cache.GetOrCreate("key4", creator(40))
	assert.Equal(t, 4, createCount, "key2, key3, key4 should still be cached")

	// key1 was evicted; fetching it again must call create.
	clk.Advance(time.Millisecond)
	cache.GetOrCreate("key1", creator(10))
	assert.Equal(t, 5, createCount, "key1 must be re-created after eviction")
}

// TestTTLCache_LRU_UpdatesLastUsed verifies that a cache hit refreshes the
// entry's lastUsed timestamp, protecting it from LRU eviction.
func TestTTLCache_LRU_UpdatesLastUsed(t *testing.T) {
	clk := newFakeClock()
	cache := newTTLCacheWithClock[string, int](time.Hour, 2, clk.Now)

	creator := func(v int) func() int { return func() int { return v } }

	cache.GetOrCreate("key1", creator(1)) // T+0
	clk.Advance(time.Millisecond)
	cache.GetOrCreate("key2", creator(2)) // T+1ms

	// Re-access key1 to refresh its lastUsed; now key2 is the LRU.
	clk.Advance(time.Millisecond)
	v1 := cache.GetOrCreate("key1", creator(99)) // hit – updates lastUsed of key1
	assert.Equal(t, 1, v1, "should return cached value, not 99")

	// Adding key3 should evict key2 (now the LRU).
	clk.Advance(time.Millisecond)
	createCount := 0
	cache.GetOrCreate("key3", func() int { createCount++; return 3 })
	assert.Equal(t, 1, createCount)

	// key1 must still be cached.
	cache.GetOrCreate("key1", func() int { createCount++; return 99 })
	assert.Equal(t, 1, createCount, "key1 should still be cached after its lastUsed was refreshed")

	// key2 must have been evicted.
	cache.GetOrCreate("key2", func() int { createCount++; return 99 })
	assert.Equal(t, 2, createCount, "key2 should have been evicted (was LRU)")
}

// TestTTLCache_ConcurrentAccess verifies that concurrent GetOrCreate calls are
// race-free (run with -race).
func TestTTLCache_ConcurrentAccess(t *testing.T) {
	cache := NewTTLCache[int, int](time.Hour, 50)

	var wg sync.WaitGroup
	var createCount atomic.Int32
	const goroutines = 100

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		key := i % 10 // intentional key collisions
		go func(k int) {
			defer wg.Done()
			cache.GetOrCreate(k, func() int { createCount.Add(1); return k * 10 })
		}(key)
	}
	wg.Wait()

	assert.LessOrEqual(t, createCount.Load(), int32(10), "at most 10 unique keys")
}

// TestTTLCache_GetOrCreate_MaxSizeZeroOrNegativeBypassesCache verifies that
// when maxSize is zero or negative every GetOrCreate call invokes create()
// directly without caching the result, and Len() always reports zero entries.
//
// A maxSize <= 0 cache is intentionally a passthrough: useful for testing code
// that accepts a *TTLCache or for runtime-disabling the cache without changing
// call-sites.
func TestTTLCache_GetOrCreate_MaxSizeZeroOrNegativeBypassesCache(t *testing.T) {
	tests := []struct {
		name    string
		maxSize int
	}{
		{name: "zero disables cache", maxSize: 0},
		{name: "negative one disables cache", maxSize: -1},
		{name: "large negative disables cache", maxSize: -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewTTLCache[string, int](time.Hour, tt.maxSize)

			createCount := 0

			// First call: create must be invoked.
			first := cache.GetOrCreate("key", func() int { createCount++; return 42 })
			assert.Equal(t, 42, first)
			assert.Equal(t, 1, createCount, "create should be called on first access")
			assert.Equal(t, 0, cache.Len(), "nothing should be stored when maxSize <= 0")

			// Second call with the same key: create must be invoked again (no caching).
			second := cache.GetOrCreate("key", func() int { createCount++; return 99 })
			assert.Equal(t, 99, second, "second call should return the new value (not a cached result)")
			assert.Equal(t, 2, createCount, "create should be called on every access when maxSize <= 0")
			assert.Equal(t, 0, cache.Len(), "cache must remain empty after second access")
		})
	}
}

// TestTTLCache_MaxSizeOne verifies edge behaviour when maxSize is 1.
func TestTTLCache_MaxSizeOne(t *testing.T) {
	clk := newFakeClock()
	cache := newTTLCacheWithClock[string, int](time.Hour, 1, clk.Now)

	createCount := 0
	cache.GetOrCreate("a", func() int { createCount++; return 1 })
	assert.Equal(t, 1, cache.Len())

	clk.Advance(time.Millisecond)
	cache.GetOrCreate("b", func() int { createCount++; return 2 })
	assert.Equal(t, 1, cache.Len(), "cache should still hold only 1 entry")
	assert.Equal(t, 2, createCount)
}

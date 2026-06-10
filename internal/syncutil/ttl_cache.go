package syncutil

import (
	"sync"
	"time"
)

// ttlEntry holds a cached value together with its last-used timestamp for LRU
// tracking.
type ttlEntry[V any] struct {
	value    V
	lastUsed time.Time
}

// TTLCache is a thread-safe generic get-or-create cache that combines lazy TTL
// eviction with an LRU size cap.
//
// On every [TTLCache.GetOrCreate] call, all entries whose idle time exceeds the
// configured TTL are evicted first. If the cache is still at capacity after TTL
// eviction, the least-recently-used entry is evicted to make room.
type TTLCache[K comparable, V any] struct {
	mu      sync.Mutex
	entries map[K]*ttlEntry[V]
	ttl     time.Duration
	maxSize int
	nowFn   func() time.Time
}

// NewTTLCache creates a new TTLCache with the given entry TTL and maximum size.
// Entries idle longer than ttl are evicted lazily; when the cache reaches
// maxSize the least-recently-used entry is evicted on the next GetOrCreate call.
func NewTTLCache[K comparable, V any](ttl time.Duration, maxSize int) *TTLCache[K, V] {
	return newTTLCacheWithClock[K, V](ttl, maxSize, time.Now)
}

// newTTLCacheWithClock creates a TTLCache with an injectable clock function.
// Intended for use in unit tests only.
func newTTLCacheWithClock[K comparable, V any](ttl time.Duration, maxSize int, nowFn func() time.Time) *TTLCache[K, V] {
	return &TTLCache[K, V]{
		entries: make(map[K]*ttlEntry[V]),
		ttl:     ttl,
		maxSize: maxSize,
		nowFn:   nowFn,
	}
}

// GetOrCreate returns the cached value for key. If the key is not present, or
// has been evicted, create is called to produce a new value which is then
// stored and returned.
//
// On each call, all expired entries are lazily evicted before the lookup. If
// the cache has reached its capacity after TTL eviction, the LRU entry is
// removed to make room.
//
// create is called while the cache lock is held.
func (c *TTLCache[K, V]) GetOrCreate(key K, create func() V) V {
	if c.maxSize <= 0 {
		return create()
	}
	now := c.nowFn()
	c.mu.Lock()
	defer c.mu.Unlock()
	// Lazy eviction of expired entries.
	if c.ttl > 0 {
		for k, entry := range c.entries {
			if now.Sub(entry.lastUsed) > c.ttl {
				delete(c.entries, k)
			}
		}
	}

	if entry, ok := c.entries[key]; ok {
		entry.lastUsed = now
		return entry.value
	}

	// LRU eviction when still at capacity after TTL sweep.
	if len(c.entries) >= c.maxSize {
		var lruKey K
		var lruTime time.Time
		first := true
		for k, entry := range c.entries {
			if first || entry.lastUsed.Before(lruTime) {
				lruKey = k
				lruTime = entry.lastUsed
				first = false
			}
		}
		if !first {
			delete(c.entries, lruKey)
		}
	}

	v := create()
	c.entries[key] = &ttlEntry[V]{value: v, lastUsed: now}
	return v
}

// Len returns the current number of entries held in the cache.
func (c *TTLCache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

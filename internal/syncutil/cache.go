// Package syncutil provides concurrency utilities.
package syncutil

import "sync"

// MapGetOrCreate looks up key in cache under a read lock; if missing, acquires a
// write lock, double-checks, and calls create to populate the entry.
// The entry is stored in cache only when create returns a nil error.
// create is called while the write lock is held, so it is safe for create to
// read or write other fields protected by the same mu.
func MapGetOrCreate[K comparable, V any](
	mu *sync.RWMutex,
	cache map[K]V,
	key K,
	create func() (V, error),
) (V, error) {
	mu.RLock()
	if v, ok := cache[key]; ok {
		mu.RUnlock()
		return v, nil
	}
	mu.RUnlock()

	mu.Lock()
	defer mu.Unlock()

	if v, ok := cache[key]; ok {
		return v, nil
	}

	v, err := create()
	if err == nil {
		cache[key] = v
	}
	return v, err
}

// Package ttlcache provides a generic, mutex-protected, TTL-based in-memory
// cache backed entirely by stdlib (sync, time). The zero value is ready to use
// with caching disabled (TTL = 0).
//
// Semantics (matching the four hand-written copies this package replaces):
//   - TTL <= 0: Get always returns (zero, false); Set is a no-op.
//   - TTL > 0: Set stores the value; Get returns it only if the entry's age
//     does not exceed the TTL (checked on read, no background eviction).
//   - Set stores whatever value it is given, including nil pointers and nil
//     slices: Get then reports (nil, true), which callers use for negative
//     caching (e.g. the PyPI wheel import-name cache stores nil on miss).
//
// DDD Layer: common (shared infrastructure utility; no domain logic).
package ttlcache

import (
	"sync"
	"time"
)

type entry[V any] struct {
	value V
	ts    time.Time
}

// Cache is a generic TTL cache. The zero value is usable with caching disabled.
// Set SetTTL to a positive duration to enable caching.
//
// Cache is safe for concurrent use by multiple goroutines.
type Cache[V any] struct {
	mu      sync.RWMutex
	entries map[string]entry[V]
	ttl     time.Duration
}

// SetTTL configures the time-to-live for cached entries. A non-positive
// value disables caching: Get always misses and Set becomes a no-op.
// Existing entries are not evicted; they expire naturally on the next Get.
func (c *Cache[V]) SetTTL(d time.Duration) {
	c.mu.Lock()
	c.ttl = d
	c.mu.Unlock()
}

// Get returns the cached value for key if it exists and has not expired.
// Returns (zero, false) when the TTL is <= 0, the key is absent, or the
// entry has exceeded its TTL.
func (c *Cache[V]) Get(key string) (V, bool) {
	c.mu.RLock()
	ttl := c.ttl
	ent, ok := c.entries[key]
	c.mu.RUnlock()

	if ttl <= 0 || !ok {
		var zero V
		return zero, false
	}
	if time.Since(ent.ts) > ttl {
		var zero V
		return zero, false
	}
	return ent.value, true
}

// Set stores v under key with the current timestamp. It is a no-op when
// the TTL is <= 0.
func (c *Cache[V]) Set(key string, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ttl <= 0 {
		return
	}
	if c.entries == nil {
		c.entries = make(map[string]entry[V])
	}
	c.entries[key] = entry[V]{value: v, ts: time.Now()}
}

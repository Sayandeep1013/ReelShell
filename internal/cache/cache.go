// Package cache is a small in-memory TTL cache for discovery-layer
// responses (SPEC.md v2: cuts redundant TMDB/AniList calls). Deliberately
// not a size-bounded LRU — for one user browsing a handful of tabs, the
// entry count never grows large enough to need eviction by size, only by
// staleness.
package cache

import (
	"sync"
	"time"
)

type entry struct {
	value   any
	expires time.Time
}

type Cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]entry
}

func New(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl, entries: make(map[string]entry)}
}

// Get returns (value, true) if key exists and hasn't expired.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.value, true
}

func (c *Cache) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry{value: value, expires: time.Now().Add(c.ttl)}
}

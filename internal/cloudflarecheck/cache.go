package cloudflarecheck

import (
	"sync"
	"time"
)

type cacheEntry struct {
	result    Result
	expiresAt time.Time
}

type Cache struct {
	ttl   time.Duration
	now   func() time.Time
	mu    sync.RWMutex
	items map[string]cacheEntry
}

func NewCache(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	return &Cache{ttl: ttl, now: time.Now, items: make(map[string]cacheEntry)}
}

func (c *Cache) Get(key string) (Result, bool) {
	if c == nil || key == "" {
		return Result{}, false
	}
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return Result{}, false
	}
	if !entry.expiresAt.After(c.now()) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return Result{}, false
	}
	result := entry.result
	result.Cached = true
	return result, true
}

func (c *Cache) Set(key string, result Result) {
	if c == nil || key == "" {
		return
	}
	result.Cached = false
	c.mu.Lock()
	c.items[key] = cacheEntry{result: result, expiresAt: c.now().Add(c.ttl)}
	c.mu.Unlock()
}

func (c *Cache) List() []Result {
	if c == nil {
		return nil
	}
	now := c.now()
	out := make([]Result, 0)
	c.mu.RLock()
	for key, entry := range c.items {
		if !entry.expiresAt.After(now) {
			continue
		}
		result := entry.result
		if result.NodeTag == "" {
			result.NodeTag = key
		}
		result.Cached = true
		out = append(out, result)
	}
	c.mu.RUnlock()
	return out
}

func (c *Cache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = make(map[string]cacheEntry)
	c.mu.Unlock()
}

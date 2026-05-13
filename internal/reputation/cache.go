package reputation

import (
	"sync"
	"time"
)

type cacheEntry struct {
	result    Result
	expiresAt time.Time
}

// MemoryCache is a small concurrency-safe in-memory TTL cache keyed by IP.
type MemoryCache struct {
	ttl   time.Duration
	now   func() time.Time
	mu    sync.RWMutex
	items map[string]cacheEntry
}

func NewMemoryCache(ttl time.Duration) *MemoryCache {
	return &MemoryCache{ttl: ttl, now: time.Now, items: make(map[string]cacheEntry)}
}

func (c *MemoryCache) Get(ip string) (Result, bool) {
	if c == nil || c.ttl <= 0 {
		return Result{}, false
	}
	c.mu.RLock()
	entry, ok := c.items[ip]
	c.mu.RUnlock()
	if !ok {
		return Result{}, false
	}
	if !entry.expiresAt.After(c.now()) {
		c.mu.Lock()
		delete(c.items, ip)
		c.mu.Unlock()
		return Result{}, false
	}
	res := entry.result
	res.Cached = true
	return res, true
}

func (c *MemoryCache) Set(ip string, result Result) {
	if c == nil || c.ttl <= 0 {
		return
	}
	result.Cached = false
	result.NormalizeAliases()
	c.mu.Lock()
	c.items[ip] = cacheEntry{result: result, expiresAt: c.now().Add(c.ttl)}
	c.mu.Unlock()
}

// Cache is a pointer-oriented compatibility wrapper around MemoryCache.
type Cache struct{ mem *MemoryCache }

func NewCache(ttl time.Duration) *Cache { return &Cache{mem: NewMemoryCache(ttl)} }

func (c *Cache) Get(ip string) (*Result, bool) {
	if c == nil {
		return nil, false
	}
	res, ok := c.mem.Get(ip)
	if !ok {
		return nil, false
	}
	return &res, true
}

func (c *Cache) Set(ip string, result *Result) {
	if c == nil || result == nil {
		return
	}
	c.mem.Set(ip, *result)
}

func (c *Cache) List() []Result {
	if c == nil || c.mem == nil {
		return nil
	}
	now := c.mem.now()
	out := make([]Result, 0)
	c.mem.mu.RLock()
	for ip, entry := range c.mem.items {
		if !entry.expiresAt.After(now) {
			continue
		}
		res := entry.result
		res.IP = ip
		res.Cached = true
		out = append(out, res)
	}
	c.mem.mu.RUnlock()
	return out
}

func (c *Cache) Clear() {
	if c == nil || c.mem == nil {
		return
	}
	c.mem.mu.Lock()
	c.mem.items = make(map[string]cacheEntry)
	c.mem.mu.Unlock()
}

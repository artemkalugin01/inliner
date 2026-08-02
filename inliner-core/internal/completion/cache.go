package completion

import (
	"sync"
	"time"
)

const (
	DefaultCacheMaxEntries = 128
	DefaultCacheTTL        = 30 * time.Minute
	cacheContextBytes      = 200
)

type CacheOptions struct {
	MaxEntries int
	TTL        time.Duration
	Now        func() time.Time
}

type AcceptanceCache struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	now        func() time.Time
	entries    []cacheEntry
}

type DismissalCache struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	now        func() time.Time
	entries    []cacheEntry
}

type cacheEntry struct {
	key       cacheKey
	text      string
	createdAt time.Time
}

type cacheKey struct {
	language string
	filePath string
	prefix   string
	suffix   string
}

func NewAcceptanceCache(options CacheOptions) *AcceptanceCache {
	maxEntries, ttl, now := resolveCacheOptions(options)
	return &AcceptanceCache{maxEntries: maxEntries, ttl: ttl, now: now}
}

func NewDismissalCache(options CacheOptions) *DismissalCache {
	maxEntries, ttl, now := resolveCacheOptions(options)
	return &DismissalCache{maxEntries: maxEntries, ttl: ttl, now: now}
}

func resolveCacheOptions(options CacheOptions) (int, time.Duration, func() time.Time) {
	maxEntries := options.MaxEntries
	if maxEntries <= 0 {
		maxEntries = DefaultCacheMaxEntries
	}

	ttl := options.TTL
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	return maxEntries, ttl, now
}

func (c *AcceptanceCache) Store(request Request, text string) {
	if text == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	key := newCacheKey(request)
	c.removeExpiredLocked(now)

	for i := range c.entries {
		if c.entries[i].key == key {
			c.entries[i].text = text
			c.entries[i].createdAt = now
			return
		}
	}

	c.entries = append(c.entries, cacheEntry{key: key, text: text, createdAt: now})
	if len(c.entries) > c.maxEntries {
		c.entries = c.entries[len(c.entries)-c.maxEntries:]
	}
}

func (c *AcceptanceCache) Lookup(request Request) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	key := newCacheKey(request)
	c.removeExpiredLocked(now)

	for i := len(c.entries) - 1; i >= 0; i-- {
		if c.entries[i].key == key {
			return c.entries[i].text, true
		}
	}

	return "", false
}

func (c *DismissalCache) Store(request Request, text string) {
	if text == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	key := newCacheKey(request)
	c.removeExpiredLocked(now)

	for i := range c.entries {
		if c.entries[i].key == key && c.entries[i].text == text {
			c.entries[i].createdAt = now
			return
		}
	}

	c.entries = append(c.entries, cacheEntry{key: key, text: text, createdAt: now})
	if len(c.entries) > c.maxEntries {
		c.entries = c.entries[len(c.entries)-c.maxEntries:]
	}
}

func (c *DismissalCache) IsDismissed(request Request, text string) bool {
	if text == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	key := newCacheKey(request)
	c.removeExpiredLocked(now)

	for i := len(c.entries) - 1; i >= 0; i-- {
		if c.entries[i].key == key && c.entries[i].text == text {
			return true
		}
	}

	return false
}

func (c *AcceptanceCache) removeExpiredLocked(now time.Time) {
	if len(c.entries) == 0 {
		return
	}

	kept := c.entries[:0]
	for _, entry := range c.entries {
		if now.Sub(entry.createdAt) <= c.ttl {
			kept = append(kept, entry)
		}
	}
	c.entries = kept
}

func (c *DismissalCache) removeExpiredLocked(now time.Time) {
	if len(c.entries) == 0 {
		return
	}

	kept := c.entries[:0]
	for _, entry := range c.entries {
		if now.Sub(entry.createdAt) <= c.ttl {
			kept = append(kept, entry)
		}
	}
	c.entries = kept
}

func newCacheKey(request Request) cacheKey {
	return cacheKey{
		language: request.Language,
		filePath: request.FilePath,
		prefix:   tail(request.Prefix, cacheContextBytes),
		suffix:   head(request.Suffix, cacheContextBytes),
	}
}

func tail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}

func head(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

package main

import (
	lru "github.com/hashicorp/golang-lru/v2"
)

// Cache interface to allow dependency injection.
type Cache interface {
	Get(key string) (string, bool)
	Add(key, value string)
	Expire(key string)
	Length() int
	Clear()
}

// MemoryCache implements Cache using a map with a mutex.
type (
	MemoryCache struct {
		cache *lru.Cache[string, cacheRecord]
	}

	cacheRecord struct {
		// Just only a simple 'A' record for now
		A string
	}
)

// NewMemoryCache creates and returns a new MemoryCache instance.
func NewMemoryCache() (*MemoryCache, error) {
	cache, err := lru.New[string, cacheRecord](100)
	if err != nil {
		return nil, err
	}
	return &MemoryCache{
		cache: cache,
	}, nil
}

// Get retrieves a value from the cache.
func (c *MemoryCache) Get(key string) (string, bool) {
	value, ok := c.cache.Get(key)
	switch {
	case !ok:
		return "", false
	default:
		return value.A, true
	}
}

// Set stores a value in the cache.
func (c *MemoryCache) Add(key, value string) {
	c.cache.Add(key, cacheRecord{A: value})
}

// Expire cache item by key.
func (c *MemoryCache) Expire(key string) {
	c.cache.Remove(key)
}

// Len returns the number of items in the cache.
func (c *MemoryCache) Length() int {
	return c.cache.Len()
}

// Clear removes all items from the cache.
func (c *MemoryCache) Clear() {
	c.cache.Purge()
}

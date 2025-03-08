package main

import "sync"

// Cache interface to allow dependency injection.
type Cache interface {
	Get(key string) (string, bool)
	Set(key, value string)
}

// MemoryCache implements Cache using a map with a mutex.
type MemoryCache struct {
	mu    sync.RWMutex
	store map[string]string
}

// NewMemoryCache creates and returns a new MemoryCache instance.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		store: make(map[string]string),
	}
}

// Get retrieves a value from the cache.
func (c *MemoryCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, found := c.store[key]
	return val, found
}

// Set stores a value in the cache.
func (c *MemoryCache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = value
}

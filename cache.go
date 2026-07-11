package main

import (
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// Cache interface to allow dependency injection.
type Cache interface {
	// Get returns the cached resource records for a full name, or false if the
	// entry is missing or has expired.
	Get(name string) ([]RR, bool)
	// Add caches the resource records for a name. The entry expires after the
	// smallest record TTL (or a default if none is set), but never past the
	// record's signed end-of-life (eol, unix seconds; 0 means no EOL cap).
	Add(name string, records []RR, eol int64)
	// Expire removes a single entry by name.
	Expire(name string)
	Length() int
	Clear()
}

// MemoryCache implements Cache using an LRU with per-entry expiry.
type (
	MemoryCache struct {
		cache *lru.Cache[string, cacheRecord]
	}

	cacheRecord struct {
		Records   []RR
		ExpiresAt time.Time
	}
)

// defaultCacheTTL is used when a record set carries no usable TTL.
const defaultCacheTTL = 5 * time.Minute

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

// Get retrieves the resource records for a name, treating expired entries as a
// miss (and evicting them).
func (c *MemoryCache) Get(name string) ([]RR, bool) {
	value, ok := c.cache.Get(name)
	if !ok {
		return nil, false
	}
	if time.Now().After(value.ExpiresAt) {
		c.cache.Remove(name)
		return nil, false
	}
	return value.Records, true
}

// Add caches resource records, computing expiry from the smallest TTL in the
// set, capped at the record's signed EOL so expired records are never served
// from cache.
func (c *MemoryCache) Add(name string, records []RR, eol int64) {
	expiresAt := time.Now().Add(cacheTTL(records))
	if eol > 0 {
		if eolTime := time.Unix(eol, 0); eolTime.Before(expiresAt) {
			expiresAt = eolTime
		}
	}
	c.cache.Add(name, cacheRecord{
		Records:   records,
		ExpiresAt: expiresAt,
	})
}

// Expire removes a single cache entry by name.
func (c *MemoryCache) Expire(name string) {
	c.cache.Remove(name)
}

// Length returns the number of items in the cache.
func (c *MemoryCache) Length() int {
	return c.cache.Len()
}

// Clear removes all items from the cache.
func (c *MemoryCache) Clear() {
	c.cache.Purge()
}

// cacheTTL returns the smallest positive TTL across the records, or a default.
func cacheTTL(records []RR) time.Duration {
	min := uint32(0)
	for _, rr := range records {
		if rr.TTL > 0 && (min == 0 || rr.TTL < min) {
			min = rr.TTL
		}
	}
	if min == 0 {
		return defaultCacheTTL
	}
	return time.Duration(min) * time.Second
}

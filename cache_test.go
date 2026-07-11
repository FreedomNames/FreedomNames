package main

import (
	"testing"
	"time"
)

func TestCacheExpiry(t *testing.T) {
	c, err := NewMemoryCache()
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}

	// Force a manual short expiry by writing a cacheRecord directly.
	c.cache.Add("mysite.k.fn", cacheRecord{
		Records:   []RR{{Type: "A", Value: "10.0.0.5", TTL: 1}},
		ExpiresAt: time.Now().Add(-time.Second), // already expired
	})

	if _, ok := c.Get("mysite.k.fn"); ok {
		t.Fatal("expected expired entry to be treated as a miss")
	}
	if c.Length() != 0 {
		t.Fatalf("expected expired entry to be evicted, len=%d", c.Length())
	}
}

func TestCacheHitReturnsRecords(t *testing.T) {
	c, _ := NewMemoryCache()
	want := []RR{{Type: "A", Value: "10.0.0.5", TTL: 300}}
	c.Add("mysite.k.fn", want)

	got, ok := c.Get("mysite.k.fn")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != 1 || got[0].Value != "10.0.0.5" {
		t.Fatalf("unexpected records: %+v", got)
	}
}

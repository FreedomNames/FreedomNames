package resolver

import (
	"testing"
	"time"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
)

func TestCacheExpiry(t *testing.T) {
	c, err := NewMemoryCache()
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}

	// Force a manual short expiry by writing a cacheRecord directly.
	c.cache.Add("mysite.k.fn", cacheRecord{
		Records:   []record.RR{{Type: "A", Value: "10.0.0.5", TTL: 1}},
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
	want := []record.RR{{Type: "A", Value: "10.0.0.5", TTL: 300}}
	c.Add("mysite.k.fn", want, 0)

	got, ok := c.Get("mysite.k.fn")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != 1 || got[0].Value != "10.0.0.5" {
		t.Fatalf("unexpected records: %+v", got)
	}
}

func TestCacheExpiryCappedByEOL(t *testing.T) {
	c, _ := NewMemoryCache()
	records := []record.RR{{Type: "A", Value: "10.0.0.5", TTL: 300}}

	// EOL already in the past: the entry must be an immediate miss even though
	// the record.RR TTL alone would cache it for 300s.
	c.Add("expired.k.fn", records, time.Now().Add(-time.Minute).Unix())
	if _, ok := c.Get("expired.k.fn"); ok {
		t.Fatal("expected record with past EOL to be treated as a miss")
	}

	// EOL far in the future: normal record.RR-TTL caching applies.
	c.Add("live.k.fn", records, time.Now().Add(24*time.Hour).Unix())
	if _, ok := c.Get("live.k.fn"); !ok {
		t.Fatal("expected record with future EOL to be cached")
	}
}

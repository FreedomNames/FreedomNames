package main

// RecordStore is the minimal record-lookup surface a Resolver needs. Both the
// real node and test fakes satisfy it.
type RecordStore interface {
	ResolveRecord(key string) (*FNRecord, error)
}

// Resolver resolves Freedom Names ("label.<pubKeyID>.fn") to their resource
// records, using a local cache in front of the DHT. It is shared by every
// resolution surface (DNS server, HTTP API, CLI).
//
// Self-certifying names resolve directly: the pubKeyID suffix yields the DHT key.
// Bare names ("mysite.fn") are routed through the optional NameRegistry (Layer 2)
// to find the controlling owner's public key first.
type Resolver struct {
	store    RecordStore
	cache    Cache
	registry NameRegistry // optional; nil means bare names are unsupported
}

// NewResolver builds a Resolver over the given record store and cache. The
// registry may be nil, in which case only self-certifying names resolve.
func NewResolver(store RecordStore, cache Cache) *Resolver {
	return &Resolver{store: store, cache: cache}
}

// WithRegistry attaches a Layer 2 name registry for resolving bare names.
func (r *Resolver) WithRegistry(registry NameRegistry) *Resolver {
	r.registry = registry
	return r
}

// Resolve returns the resource records for a full "label.<pubKeyID>.fn" name.
// It checks the cache first, then the DHT, caching any DHT hit.
func (r *Resolver) Resolve(name string) ([]RR, error) {
	if records, ok := r.cache.Get(name); ok {
		return records, nil
	}

	key, err := r.dhtKeyForName(name)
	if err != nil {
		return nil, err
	}
	rec, err := r.store.ResolveRecord(key)
	if err != nil {
		return nil, err
	}

	r.cache.Add(name, rec.Records)
	return rec.Records, nil
}

// dhtKeyForName derives the DHT key for a name. Self-certifying names use their
// pubKeyID suffix directly; bare names are resolved to an owner pubkey via the
// Layer 2 registry.
func (r *Resolver) dhtKeyForName(name string) (string, error) {
	if !isBareName(name) {
		return DHTKeyForName(name)
	}
	if r.registry == nil {
		return "", ErrRegistryNotFound
	}
	pubKey, err := r.registry.ResolveOwner(name)
	if err != nil {
		return "", err
	}
	return DHTKeyForPubKey(pubKey)
}

// ResolveType returns only the records of the requested type for a name.
func (r *Resolver) ResolveType(name, recordType string) ([]RR, error) {
	records, err := r.Resolve(name)
	if err != nil {
		return nil, err
	}
	filtered := make([]RR, 0, len(records))
	for _, rr := range records {
		if rr.Type == recordType {
			filtered = append(filtered, rr)
		}
	}
	return filtered, nil
}

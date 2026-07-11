package main

// bchRegistry is the (not-yet-implemented) Bitcoin Cash registry resolver. When
// built, it will map a normalized bare name to the controlling CashTokens NFT
// whose commitment carries the owner's public key, using LBRY-style claim
// semantics.
//
// It is wired in behind the NameRegistry interface so Layer 1 does not depend on
// it; today it returns ErrRegistryNotImplemented and callers fall back to
// self-certifying resolution.
type bchRegistry struct {
	// indexerURL string // BCH indexer / node endpoint (Phase 2)
}

// NewBCHRegistry returns the BCH registry resolver stub.
func NewBCHRegistry() NameRegistry {
	return &bchRegistry{}
}

// ResolveOwner is not yet implemented. Phase 2 will:
//  1. normalize the name,
//  2. look up the controlling claim (the CashTokens NFT with the highest
//     effective stake for that name) via a BCH indexer,
//  3. extract and return the owner public key from the NFT commitment.
func (r *bchRegistry) ResolveOwner(name string) ([]byte, error) {
	return nil, ErrRegistryNotImplemented
}

package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
)

// defaultRecordTTL is how long a signed FNRecord stays valid (its EOL horizon).
// The node can only re-put the original signed bytes — it never holds the
// owner's private key, so it cannot extend the EOL. The owner must re-publish
// (re-sign) before this expires; the CLI surfaces the expiry date at publish.
const defaultRecordTTL = 7 * 24 * time.Hour

// republishInterval is how often the node re-puts owned records into the DHT.
// libp2p DHT records expire in ~36h, so this must stay comfortably below that.
const republishInterval = 8 * time.Hour

// BuildAndSignRecord constructs an FNRecord for the given label and resource
// records, sets its expiry to now+defaultRecordTTL, and signs it with the owner
// key. seq should be higher than any previously published record for this key.
func BuildAndSignRecord(priv crypto.PrivKey, label string, records []RR, seq uint64) (*FNRecord, error) {
	rec := &FNRecord{
		Label:   label,
		Records: records,
		Seq:     seq,
		EOL:     time.Now().Add(defaultRecordTTL).Unix(),
	}
	if err := rec.Sign(priv); err != nil {
		return nil, err
	}
	return rec, nil
}

// PublishRecord signs (if needed) and stores an FNRecord in the DHT, and tracks it
// for periodic republishing. The record must already be signed.
func (freedomName *FreedomNameNode) PublishRecord(rec *FNRecord) error {
	key, err := rec.DHTKey()
	if err != nil {
		return err
	}
	value, err := rec.Marshal()
	if err != nil {
		return err
	}
	if err := freedomName.PutValue(key, value); err != nil {
		return err
	}

	freedomName.ownedMu.Lock()
	freedomName.owned[key] = rec
	freedomName.ownedMu.Unlock()
	log.Printf("Published record for %s (seq %d)", key, rec.Seq)
	return nil
}

// ResolveRecord fetches and returns the current FNRecord for a DHT key. The
// caller's context bounds the lookup (so e.g. the DNS path can use a short,
// client-appropriate budget), additionally capped at dhtOpTimeout.
func (freedomName *FreedomNameNode) ResolveRecord(ctx context.Context, key string) (*FNRecord, error) {
	if freedomName.kadDHT == nil {
		return nil, errors.New("DHT not initialized")
	}
	ctx, cancel := context.WithTimeout(ctx, dhtOpTimeout)
	defer cancel()

	value, err := freedomName.kadDHT.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return UnmarshalFNRecord(value)
}

// republishLoop periodically re-publishes owned records so they never expire from
// the DHT while this node is up.
func (freedomName *FreedomNameNode) republishLoop() {
	ticker := time.NewTicker(republishInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			freedomName.republishOwned()
		case <-freedomName.ctx.Done():
			log.Println("Stopping republish service.")
			return
		}
	}
}

// republishOwned re-puts each still-valid owned record into the DHT so it does
// not fall out at the DHT's ~36h record expiry. It cannot extend a record's
// signed EOL (the node has no owner keys): records whose EOL has passed are
// pruned from the owned set with a warning telling the owner to re-publish.
func (freedomName *FreedomNameNode) republishOwned() {
	now := time.Now().Unix()

	freedomName.ownedMu.Lock()
	live := make(map[string]*FNRecord, len(freedomName.owned))
	for key, rec := range freedomName.owned {
		if rec.EOL != 0 && now > rec.EOL {
			log.Printf("WARNING: record %s (label %q) passed its signed EOL and was dropped from republishing; the owner must re-publish (re-sign) it", key, rec.Label)
			delete(freedomName.owned, key)
			continue
		}
		live[key] = rec
	}
	freedomName.ownedMu.Unlock()

	for key, rec := range live {
		value, err := rec.Marshal()
		if err != nil {
			log.Printf("republish: marshal %s: %v", key, err)
			continue
		}
		if err := freedomName.PutValue(key, value); err != nil {
			log.Printf("republish: put %s: %v", key, err)
			continue
		}
		log.Printf("Republished record for %s (seq %d)", key, rec.Seq)
	}
}

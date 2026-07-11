package main

import (
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
)

// defaultRecordTTL is how long a published FNRecord stays valid before it must be
// republished. libp2p DHT records expire in ~36h, so we refresh well before that.
const defaultRecordTTL = 24 * time.Hour

// republishInterval is how often the node re-publishes owned records.
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

// ResolveRecord fetches and returns the current FNRecord for a DHT key.
func (freedomName *FreedomNameNode) ResolveRecord(key string) (*FNRecord, error) {
	value, err := freedomName.GetValue(key)
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

// republishOwned bumps the expiry of each owned record and re-stores it.
func (freedomName *FreedomNameNode) republishOwned() {
	freedomName.ownedMu.Lock()
	records := make([]*FNRecord, 0, len(freedomName.owned))
	for _, rec := range freedomName.owned {
		records = append(records, rec)
	}
	freedomName.ownedMu.Unlock()

	for _, rec := range records {
		key, err := rec.DHTKey()
		if err != nil {
			log.Printf("republish: bad key: %v", err)
			continue
		}
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

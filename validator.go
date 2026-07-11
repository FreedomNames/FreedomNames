package main

import (
	"bytes"
	"errors"
	"fmt"

	record "github.com/libp2p/go-libp2p-record"
)

// FreedomNameValidator validates records in the "fn" namespace. A record is only
// accepted if it is a well-formed, signed FNRecord whose public key hashes to the
// DHT key it is stored under, and whose signature verifies. This is what prevents
// anyone from overwriting a name they do not own.
type FreedomNameValidator struct{}

// Validate validates a freedom name (FN) record.
func (v FreedomNameValidator) Validate(key string, value []byte) error {
	ns, keyID, err := record.SplitKey(key)
	if err != nil {
		return err
	}
	if ns != dhtNamespace {
		return fmt.Errorf("namespace not %q", dhtNamespace)
	}

	rec, err := UnmarshalFNRecord(value)
	if err != nil {
		return err
	}

	// The public key must hash to the DHT key suffix (self-certifying binding).
	wantID, err := pubKeyID(rec.PubKey)
	if err != nil {
		return err
	}
	if wantID != keyID {
		return errors.New("record public key does not match DHT key")
	}

	// Signature, expiry and record sanity.
	return rec.Verify()
}

// Select conforms to the Validator interface: it picks the best of several
// competing values for the same key. Records are ordered by highest Seq, then
// latest EOL, then raw byte comparison — the same rule IPNS uses. Callers are
// expected to have Validated the values first, but we defensively skip any that
// fail to unmarshal.
func (v FreedomNameValidator) Select(k string, vals [][]byte) (int, error) {
	if len(vals) == 0 {
		return 0, errors.New("no values to select from")
	}

	best := -1
	var bestRec *FNRecord
	for i, val := range vals {
		rec, err := UnmarshalFNRecord(val)
		if err != nil {
			continue
		}
		if best == -1 || betterRecord(rec, bestRec, vals[i], vals[best]) {
			best = i
			bestRec = rec
		}
	}
	if best == -1 {
		return 0, errors.New("no valid values to select from")
	}
	return best, nil
}

// betterRecord reports whether candidate should win over current.
func betterRecord(candidate, current *FNRecord, candidateRaw, currentRaw []byte) bool {
	if candidate.Seq != current.Seq {
		return candidate.Seq > current.Seq
	}
	if candidate.EOL != current.EOL {
		return candidate.EOL > current.EOL
	}
	return bytes.Compare(candidateRaw, currentRaw) > 0
}

package main

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
)

func newTestKey(t *testing.T) crypto.PrivKey {
	t.Helper()
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, -1, rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return priv
}

func TestSignVerifyRoundTrip(t *testing.T) {
	priv := newTestKey(t)
	rec, err := BuildAndSignRecord(priv, "mysite", []RR{{Type: "A", Value: "10.0.0.5", TTL: 300}}, 1)
	if err != nil {
		t.Fatalf("build/sign: %v", err)
	}
	if err := rec.Verify(); err != nil {
		t.Fatalf("verify freshly signed record: %v", err)
	}

	// Marshal -> Unmarshal must preserve validity.
	data, err := rec.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalFNRecord(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := got.Verify(); err != nil {
		t.Fatalf("verify after round-trip: %v", err)
	}
}

func TestTamperRejected(t *testing.T) {
	priv := newTestKey(t)
	rec, _ := BuildAndSignRecord(priv, "mysite", []RR{{Type: "A", Value: "10.0.0.5", TTL: 300}}, 1)

	// Mutate a record value after signing.
	rec.Records[0].Value = "6.6.6.6"
	if err := rec.Verify(); err == nil {
		t.Fatal("expected verify to fail on tampered record, got nil")
	}
}

func TestExpiredRejected(t *testing.T) {
	priv := newTestKey(t)
	rec := &FNRecord{
		Label:   "mysite",
		Records: []RR{{Type: "A", Value: "10.0.0.5", TTL: 300}},
		Seq:     1,
		EOL:     time.Now().Add(-time.Hour).Unix(),
	}
	if err := rec.Sign(priv); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := rec.Verify(); err == nil {
		t.Fatal("expected verify to fail on expired record, got nil")
	}
}

func TestValidatorRejectsWrongKeyBinding(t *testing.T) {
	priv := newTestKey(t)
	rec, _ := BuildAndSignRecord(priv, "mysite", []RR{{Type: "A", Value: "10.0.0.5", TTL: 300}}, 1)
	value, _ := rec.Marshal()

	v := FreedomNameValidator{}

	// Correct key: derived from the record's own pubkey.
	goodKey, err := rec.DHTKey()
	if err != nil {
		t.Fatalf("dht key: %v", err)
	}
	if err := v.Validate(goodKey, value); err != nil {
		t.Fatalf("validate under correct key: %v", err)
	}

	// Wrong key: someone tries to store this record under a different name's key.
	other := newTestKey(t)
	otherPub, _ := crypto.MarshalPublicKey(other.GetPublic())
	badKey, _ := DHTKeyForPubKey(otherPub)
	if err := v.Validate(badKey, value); err == nil {
		t.Fatal("expected validate to reject record stored under wrong key, got nil")
	}
}

func TestSelectRejectsForgedHigherSeq(t *testing.T) {
	// The rightful owner publishes seq 1. An attacker crafts a seq-2 record with
	// their OWN key but the victim's DHT key. The validator must reject the forged
	// record when stored under the victim's key, so Select never even sees it as a
	// valid competitor for that key.
	owner := newTestKey(t)
	attacker := newTestKey(t)

	ownerRec, _ := BuildAndSignRecord(owner, "mysite", []RR{{Type: "A", Value: "10.0.0.5", TTL: 300}}, 1)
	victimKey, _ := ownerRec.DHTKey()

	forged, _ := BuildAndSignRecord(attacker, "mysite", []RR{{Type: "A", Value: "6.6.6.6", TTL: 300}}, 2)
	forgedBytes, _ := forged.Marshal()

	v := FreedomNameValidator{}
	if err := v.Validate(victimKey, forgedBytes); err == nil {
		t.Fatal("expected validator to reject attacker's record under victim's key")
	}
}

func TestSelectHighestSeqWins(t *testing.T) {
	priv := newTestKey(t)
	old, _ := BuildAndSignRecord(priv, "mysite", []RR{{Type: "A", Value: "10.0.0.5", TTL: 300}}, 1)
	newer, _ := BuildAndSignRecord(priv, "mysite", []RR{{Type: "A", Value: "10.0.0.6", TTL: 300}}, 2)

	oldBytes, _ := old.Marshal()
	newBytes, _ := newer.Marshal()

	v := FreedomNameValidator{}
	idx, err := v.Select("/fn/whatever", [][]byte{oldBytes, newBytes})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if idx != 1 {
		t.Fatalf("expected index 1 (seq 2) to win, got %d", idx)
	}
}

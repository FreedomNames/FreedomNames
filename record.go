package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/multiformats/go-base36"
	mh "github.com/multiformats/go-multihash"
)

// Supported resource-record types. Kept small on purpose for the PoC.
const (
	RecordTypeA     = "A"
	RecordTypeAAAA  = "AAAA"
	RecordTypeTXT   = "TXT"
	RecordTypeCNAME = "CNAME"
)

// dhtNamespace is the DHT key namespace, matching the NamespacedValidator
// registered in node.go ("fn").
const dhtNamespace = "fn"

// tld is the human-facing top-level domain for Freedom Names.
const tld = "fn"

// RR is a single DNS-style resource record.
type RR struct {
	Type  string `json:"type"`  // A | AAAA | TXT | CNAME
	Value string `json:"value"` // IP, hostname or text depending on Type
	TTL   uint32 `json:"ttl"`   // seconds
}

// FNRecord is a self-sovereign, signed Freedom Names record. Ownership is proven
// by the Ed25519 keypair whose public key hashes to the record's DHT key. Records
// are ordered by (Seq, EOL) so the newest signed update wins.
type FNRecord struct {
	Label   string `json:"label"`   // human label, e.g. "mysite"
	Records []RR   `json:"records"` // the resource records for this name
	Seq     uint64 `json:"seq"`     // monotonic per-name; higher wins
	EOL     int64  `json:"eol"`     // unix seconds; record invalid after this
	PubKey  []byte `json:"pubKey"`  // marshaled Ed25519 owner public key
	Sig     []byte `json:"sig"`     // Ed25519 signature over canonicalBytes()
}

// canonicalBytes returns the deterministic serialization signed over. It excludes
// Sig and uses a fixed field order so signing/verification is stable regardless of
// map/JSON ordering.
func (r *FNRecord) canonicalBytes() []byte {
	var b bytes.Buffer
	b.WriteString(r.Label)
	b.WriteByte(0)

	// Records in a stable order (Type, then Value) so signatures are reproducible.
	recs := make([]RR, len(r.Records))
	copy(recs, r.Records)
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].Type != recs[j].Type {
			return recs[i].Type < recs[j].Type
		}
		return recs[i].Value < recs[j].Value
	})
	for _, rr := range recs {
		b.WriteString(rr.Type)
		b.WriteByte(0)
		b.WriteString(rr.Value)
		b.WriteByte(0)
		var ttl [4]byte
		binary.BigEndian.PutUint32(ttl[:], rr.TTL)
		b.Write(ttl[:])
	}

	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], r.Seq)
	b.Write(scratch[:])
	binary.BigEndian.PutUint64(scratch[:], uint64(r.EOL))
	b.Write(scratch[:])
	b.Write(r.PubKey)
	return b.Bytes()
}

// Sign fills PubKey and Sig using the owner's private key.
func (r *FNRecord) Sign(priv crypto.PrivKey) error {
	pubBytes, err := crypto.MarshalPublicKey(priv.GetPublic())
	if err != nil {
		return fmt.Errorf("marshal public key: %w", err)
	}
	r.PubKey = pubBytes
	sig, err := priv.Sign(r.canonicalBytes())
	if err != nil {
		return fmt.Errorf("sign record: %w", err)
	}
	r.Sig = sig
	return nil
}

// Verify checks the signature, the pubkey binding, expiry and record sanity.
// It does NOT check that PubKey hashes to a particular DHT key; that binding is
// enforced by the validator, which knows the key.
func (r *FNRecord) Verify() error {
	if len(r.PubKey) == 0 {
		return errors.New("record has no public key")
	}
	if len(r.Sig) == 0 {
		return errors.New("record has no signature")
	}
	pub, err := crypto.UnmarshalPublicKey(r.PubKey)
	if err != nil {
		return fmt.Errorf("unmarshal public key: %w", err)
	}
	ok, err := pub.Verify(r.canonicalBytes(), r.Sig)
	if err != nil {
		return fmt.Errorf("verify signature: %w", err)
	}
	if !ok {
		return errors.New("invalid signature")
	}
	if r.EOL != 0 && time.Now().Unix() > r.EOL {
		return errors.New("record expired")
	}
	return r.validateRecords()
}

// validateRecords sanity-checks each RR's type and value.
func (r *FNRecord) validateRecords() error {
	if len(r.Records) == 0 {
		return errors.New("record has no resource records")
	}
	for _, rr := range r.Records {
		switch rr.Type {
		case RecordTypeA:
			ip := net.ParseIP(rr.Value)
			if ip == nil || ip.To4() == nil {
				return fmt.Errorf("A record has invalid IPv4: %q", rr.Value)
			}
		case RecordTypeAAAA:
			ip := net.ParseIP(rr.Value)
			if ip == nil || ip.To4() != nil {
				return fmt.Errorf("AAAA record has invalid IPv6: %q", rr.Value)
			}
		case RecordTypeCNAME:
			if rr.Value == "" {
				return errors.New("CNAME record has empty target")
			}
		case RecordTypeTXT:
			// Any UTF-8 string is acceptable.
		default:
			return fmt.Errorf("unsupported record type: %q", rr.Type)
		}
	}
	return nil
}

// Marshal serializes the record for DHT storage.
func (r *FNRecord) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

// UnmarshalFNRecord parses a record from DHT bytes.
func UnmarshalFNRecord(data []byte) (*FNRecord, error) {
	var r FNRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("unmarshal FNRecord: %w", err)
	}
	return &r, nil
}

// unmarshalOwnerPubKey parses a marshaled libp2p public key (as carried in an
// FN01/FN02 OP_RETURN), returning an error if it is not a valid key.
func unmarshalOwnerPubKey(marshaled []byte) (crypto.PubKey, error) {
	return crypto.UnmarshalPublicKey(marshaled)
}

// pubKeyID returns the self-certifying suffix for a marshaled public key: the
// lowercase base36 encoding of its sha2-256 multihash. This is what appears in
// the human name "label.<pubKeyID>.fn" and what the DHT key is derived from.
func pubKeyID(marshaledPub []byte) (string, error) {
	h, err := mh.Sum(marshaledPub, mh.SHA2_256, -1)
	if err != nil {
		return "", fmt.Errorf("hash public key: %w", err)
	}
	return base36.EncodeToStringLc(h), nil
}

// DHTKey returns the DHT key this record must be stored under, derived from its
// own public key. A record can therefore only live under the key its pubkey
// hashes to, which is the root of the ownership guarantee.
func (r *FNRecord) DHTKey() (string, error) {
	id, err := pubKeyID(r.PubKey)
	if err != nil {
		return "", err
	}
	return "/" + dhtNamespace + "/" + id, nil
}

// DHTKeyForPubKey builds the DHT key for a marshaled public key.
func DHTKeyForPubKey(marshaledPub []byte) (string, error) {
	id, err := pubKeyID(marshaledPub)
	if err != nil {
		return "", err
	}
	return "/" + dhtNamespace + "/" + id, nil
}

// FullName returns the human-facing name "label.<pubKeyID>.fn".
func (r *FNRecord) FullName() (string, error) {
	id, err := pubKeyID(r.PubKey)
	if err != nil {
		return "", err
	}
	return r.Label + "." + id + "." + tld, nil
}

// CanonicalName normalizes a name for use as a cache or comparison key:
// lowercase with any trailing dot (DNS FQDN form) stripped. Every surface (DNS,
// HTTP, CLI) must key caches on this form so "MySite.<id>.fn." and
// "mysite.<id>.fn" share one entry.
func CanonicalName(name string) string {
	return strings.TrimSuffix(strings.ToLower(name), ".")
}

// IsPubKeyID reports whether s is a well-formed self-certifying pubkey id:
// the base36 encoding of a sha2-256 multihash. This is the authoritative test
// for Layer 1 vs Layer 2 routing — no length heuristics.
func IsPubKeyID(s string) bool {
	raw, err := base36.DecodeString(s)
	if err != nil {
		return false
	}
	decoded, err := mh.Decode(raw)
	if err != nil {
		return false
	}
	return decoded.Code == mh.SHA2_256
}

// ErrNotFNName marks a name that is not a well-formed "label.<pubKeyID>.fn"
// name. Callers classify errors with errors.Is (e.g. to map them to HTTP 400)
// instead of matching message text.
var ErrNotFNName = errors.New("not a valid fn name")

// ParseName splits a "label.<pubKeyID>.fn" name into its label and pubkey-id.
// It tolerates a trailing dot (as DNS fully-qualified names carry).
func ParseName(name string) (label, keyID string, err error) {
	trimmed := CanonicalName(name)
	parts := strings.Split(trimmed, ".")
	if len(parts) < 3 || parts[len(parts)-1] != tld {
		return "", "", fmt.Errorf("%w: %q", ErrNotFNName, name)
	}
	keyID = parts[len(parts)-2]
	label = strings.Join(parts[:len(parts)-2], ".")
	if label == "" || keyID == "" {
		return "", "", fmt.Errorf("%w (malformed): %q", ErrNotFNName, name)
	}
	return label, keyID, nil
}

// DHTKeyForName derives the DHT key from a "label.<pubKeyID>.fn" name.
func DHTKeyForName(name string) (string, error) {
	_, keyID, err := ParseName(name)
	if err != nil {
		return "", err
	}
	return "/" + dhtNamespace + "/" + keyID, nil
}

package record

import (
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
)

// defaultRecordTTL is how long a signed FNRecord stays valid (its EOL horizon).
// The node can only re-put the original signed bytes — it never holds the
// owner's private key, so it cannot extend the EOL. The owner must re-publish
// (re-sign) before this expires; the CLI surfaces the expiry date at publish.
const defaultRecordTTL = 7 * 24 * time.Hour

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

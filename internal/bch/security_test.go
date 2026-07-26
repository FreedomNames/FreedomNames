package bch

import (
	"errors"
	"fmt"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/registry"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/testsupport"
)

// --- Registry history scanning: never answer from a partial history ---

// TestNewestHistoryScansTheRecentEnd pins the direction of the custody scan. A
// transaction that spends an output is mined at or after the one that created
// it, so a truncated custody scan has to keep the recent entries: keeping the
// oldest would search precisely the half that cannot contain the spend, report
// the outpoint as unspent, and freeze a transferred name on its previous owner.
func TestNewestHistoryScansTheRecentEnd(t *testing.T) {
	history := make([]electrumHistoryItem, maxHistoryScan+10)
	for i := range history {
		history[i] = electrumHistoryItem{Height: int64(i + 1), TxHash: fmt.Sprintf("tx%d", i)}
	}

	scan, truncated := newestHistory(history, "test")
	if !truncated {
		t.Fatal("truncated = false for an over-long history")
	}
	if len(scan) != maxHistoryScan {
		t.Fatalf("scanned %d entries, want %d", len(scan), maxHistoryScan)
	}
	// Newest first, and the newest entry must be present at all.
	if got, want := scan[0].Height, history[len(history)-1].Height; got != want {
		t.Fatalf("first scanned height = %d, want the newest (%d)", got, want)
	}
	if got, want := scan[len(scan)-1].Height, history[10].Height; got != want {
		t.Fatalf("last scanned height = %d, want %d", got, want)
	}

	short := history[:5]
	if scan, truncated := newestHistory(short, "test"); truncated || len(scan) != 5 {
		t.Fatalf("short history: truncated=%v len=%d, want false/5", truncated, len(scan))
	}

	// The claim scan keeps the other end: the earliest confirmed claim wins.
	scan, truncated = oldestHistory(history, "test")
	if !truncated || len(scan) != maxHistoryScan {
		t.Fatalf("oldestHistory: truncated=%v len=%d", truncated, len(scan))
	}
	if got, want := scan[0].Height, history[0].Height; got != want {
		t.Fatalf("oldestHistory first height = %d, want %d", got, want)
	}
}

// TestTruncatedHistoryIsNotReportedAsUnclaimed covers the denial of service a
// silent truncation would open. The marker address is derived from the label
// alone, so anyone can compute it and pad a name's history with dust until the
// real claim falls outside the scan window. The lookup must then fail as
// inconclusive: reporting "unclaimed" would be wrong AND would be negative
// cached, keeping a perfectly valid name dark long after the flood.
func TestTruncatedHistoryIsNotReportedAsUnclaimed(t *testing.T) {
	m := newMockElectrum(t)
	ownerKey := testsupport.NewTestKey(t)
	ownerPub := ownerPubBytes(t, ownerKey)

	fundingKey, _ := secp256k1.GeneratePrivateKey()
	holderScript := p2pkhScript(hash160(fundingKey.PubKey().SerializeCompressed()))

	// Dust padding: cheap for an attacker, and it all lands on the marker
	// address ahead of the genuine claim.
	dust := makeClaimTx(t, "unrelated", ownerPub, holderScript, fundingKey, mustHex(t, repeat("cd", 32)))
	dustID := m.addTx(dust, 1, markerScript("unrelated"))
	marker := scriptHash(markerScript("padded"))
	for i := range maxHistoryScan + 1 {
		m.history[marker] = append(m.history[marker], electrumHistoryItem{Height: int64(i + 1), TxHash: dustID})
	}

	claim := makeClaimTx(t, "padded", ownerPub, holderScript, fundingKey, mustHex(t, repeat("ab", 32)))
	m.addTx(claim, 10000, markerScript("padded"), holderScript)

	client := NewElectrumClient(m.endpoint())
	defer client.Close()
	reg := NewBCHRegistry(client, 1)

	_, err := reg.ResolveOwner("padded.fn")
	if err == nil {
		t.Fatal("expected the truncated lookup to fail")
	}
	if !errors.Is(err, errHistoryTruncated) {
		t.Fatalf("err = %v, want it to wrap errHistoryTruncated", err)
	}
	// The important half: registry.ErrRegistryNotFound is the only error that gets
	// negative cached, so an inconclusive scan must not masquerade as one.
	if errors.Is(err, registry.ErrRegistryNotFound) {
		t.Fatal("an incomplete scan was reported as a definitive not-found, which gets negative cached")
	}
}

// --- hosting budget ---

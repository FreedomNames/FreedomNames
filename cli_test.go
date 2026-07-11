package main

import (
	"path/filepath"
	"testing"
)

// withTempHome points ~/.freedom at a temp dir for the duration of a test.
func withTempHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestCliSetDedupes(t *testing.T) {
	withTempHome(t)

	// Setting the same type+value twice must not create a duplicate.
	if err := cliSet([]string{"mysite", "A", "42.42.42.42", "300"}); err != nil {
		t.Fatalf("first set: %v", err)
	}
	if err := cliSet([]string{"mysite", "A", "42.42.42.42", "600"}); err != nil {
		t.Fatalf("second set: %v", err)
	}

	records, err := loadStaged("mysite")
	if err != nil {
		t.Fatalf("load staged: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 deduped record, got %d: %+v", len(records), records)
	}
	// The repeat set should have updated the TTL in place.
	if records[0].TTL != 600 {
		t.Fatalf("expected TTL updated to 600, got %d", records[0].TTL)
	}
}

func TestCliSetKeepsDistinctRecords(t *testing.T) {
	withTempHome(t)

	if err := cliSet([]string{"mysite", "A", "10.0.0.1", "300"}); err != nil {
		t.Fatalf("set A: %v", err)
	}
	if err := cliSet([]string{"mysite", "A", "10.0.0.2", "300"}); err != nil {
		t.Fatalf("set second A: %v", err)
	}
	if err := cliSet([]string{"mysite", "TXT", "hello", "300"}); err != nil {
		t.Fatalf("set TXT: %v", err)
	}

	records, err := loadStaged("mysite")
	if err != nil {
		t.Fatalf("load staged: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 distinct records, got %d: %+v", len(records), records)
	}
}

func TestNextSeq(t *testing.T) {
	// First publish: no current record, use wall-clock.
	if got := nextSeq(1000, nil); got != 1000 {
		t.Fatalf("nil current: want 1000, got %d", got)
	}
	// Normal update: current record older than the clock, use wall-clock.
	if got := nextSeq(1000, &FNRecord{Seq: 500}); got != 1000 {
		t.Fatalf("older current: want 1000, got %d", got)
	}
	// Same-second double publish: must go strictly above the current record.
	if got := nextSeq(1000, &FNRecord{Seq: 1000}); got != 1001 {
		t.Fatalf("same-second: want 1001, got %d", got)
	}
	// Clock stepped backwards: must still go above the current record.
	if got := nextSeq(900, &FNRecord{Seq: 1000}); got != 1001 {
		t.Fatalf("clock back: want 1001, got %d", got)
	}
}

func TestStagedRoundTripPath(t *testing.T) {
	withTempHome(t)
	// Sanity: staged file lands under ~/.freedom/keys.
	p, err := stagePath("mysite")
	if err != nil {
		t.Fatalf("stagePath: %v", err)
	}
	if filepath.Base(p) != "mysite.records.json" {
		t.Fatalf("unexpected staged path: %s", p)
	}
}

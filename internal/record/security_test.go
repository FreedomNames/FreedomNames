package record

import (
	"strings"
	"testing"
)

// --- Record size limits: an unbounded record makes the network carry it ---

func TestValidateRecordsEnforcesSizeLimits(t *testing.T) {
	tests := []struct {
		name    string
		rec     FNRecord
		wantErr string
	}{
		{
			name:    "TXT longer than a DNS character-string",
			rec:     FNRecord{Label: "x", Records: []RR{{Type: RecordTypeTXT, Value: strings.Repeat("a", maxTXTLen+1)}}},
			wantErr: "TXT value",
		},
		{
			name:    "too many resource records",
			rec:     FNRecord{Label: "x", Records: manyRecords(maxRRsPerRecord + 1)},
			wantErr: "resource records",
		},
		{
			name:    "oversized label",
			rec:     FNRecord{Label: strings.Repeat("a", MaxLabelLen+1), Records: []RR{{Type: RecordTypeA, Value: "10.0.0.1"}}},
			wantErr: "label is",
		},
		{
			name:    "oversized CNAME target",
			rec:     FNRecord{Label: "x", Records: []RR{{Type: RecordTypeCNAME, Value: strings.Repeat("a", maxDNSNameLen+1)}}},
			wantErr: "CNAME target",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rec.ValidateRecords()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}

	// The limits must not reject an ordinary record set.
	ok := FNRecord{Label: "mysite", Records: []RR{
		{Type: RecordTypeA, Value: "10.0.0.1", TTL: 300},
		{Type: RecordTypeTXT, Value: strings.Repeat("a", maxTXTLen), TTL: 300},
	}}
	if err := ok.ValidateRecords(); err != nil {
		t.Fatalf("valid record set rejected: %v", err)
	}
}

func manyRecords(n int) []RR {
	out := make([]RR, n)
	for i := range out {
		out[i] = RR{Type: RecordTypeTXT, Value: "v", TTL: 300}
	}
	return out
}

// --- CLI label validation ---

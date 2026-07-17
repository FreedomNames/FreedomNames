package main

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestRateLimiterNilPassthrough(t *testing.T) {
	if newRateLimiter(0) != nil || newRateLimiter(-1) != nil {
		t.Fatalf("zero/negative rate should yield nil limiter")
	}
	var buf bytes.Buffer
	if limitWriter(&buf, nil) != io.Writer(&buf) {
		t.Fatalf("nil limiter writer not a passthrough")
	}
	r := bytes.NewReader(nil)
	if limitReader(r, nil) != io.Reader(r) {
		t.Fatalf("nil limiter reader not a passthrough")
	}
}

func TestRateLimitedCopySlowsDown(t *testing.T) {
	data := testBytes(256 << 10) // 256 KiB
	// 512 KiB/s with a 64 KiB min burst: 256 KiB should need roughly 380ms
	// ((256-64) KiB beyond the burst at 512 KiB/s). Assert a loose lower bound.
	l := newRateLimiter(512 << 10)
	var buf bytes.Buffer
	start := time.Now()
	if _, err := io.Copy(limitWriter(&buf, l), bytes.NewReader(data)); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Fatalf("limited copy finished too fast: %v", elapsed)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Fatalf("limited copy corrupted data")
	}

	// Reader side: same budget, same expectation.
	l2 := newRateLimiter(512 << 10)
	var buf2 bytes.Buffer
	start = time.Now()
	if _, err := io.Copy(&buf2, limitReader(bytes.NewReader(data), l2)); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Fatalf("limited read finished too fast: %v", elapsed)
	}
	if !bytes.Equal(buf2.Bytes(), data) {
		t.Fatalf("limited read corrupted data")
	}
}

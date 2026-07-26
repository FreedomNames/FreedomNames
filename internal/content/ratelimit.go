package content

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

// Optional bandwidth caps for the content layer's bulk paths (serving,
// fetching, pushing, receiving pushes). A nil limiter means unlimited and
// every wrapper is a passthrough, so the default config costs nothing. DHT
// and naming traffic are never limited.

// rateChunk is the largest single WaitN reservation, keeping bursts small and
// wait times short regardless of caller buffer sizes.
const rateChunk = 32 << 10

// NewRateLimiter returns a token bucket for bytesPerSec, or nil (unlimited)
// for zero/negative rates.
func NewRateLimiter(bytesPerSec int64) *rate.Limiter {
	if bytesPerSec <= 0 {
		return nil
	}
	burst := int(bytesPerSec / 10)
	if burst < 64<<10 {
		burst = 64 << 10
	}
	return rate.NewLimiter(rate.Limit(bytesPerSec), burst)
}

// LimitReader wraps r so reads consume tokens from l. Nil l returns r as-is.
func LimitReader(r io.Reader, l *rate.Limiter) io.Reader {
	if l == nil {
		return r
	}
	return &limitedReader{r: r, l: l}
}

// LimitWriter wraps w so writes consume tokens from l. Nil l returns w as-is.
func LimitWriter(w io.Writer, l *rate.Limiter) io.Writer {
	if l == nil {
		return w
	}
	return &limitedWriter{w: w, l: l}
}

type limitedReader struct {
	r io.Reader
	l *rate.Limiter
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	if len(p) > rateChunk {
		p = p[:rateChunk]
	}
	n, err := lr.r.Read(p)
	if n > 0 {
		if werr := lr.l.WaitN(context.Background(), n); werr != nil && err == nil {
			err = werr
		}
	}
	return n, err
}

type limitedWriter struct {
	w io.Writer
	l *rate.Limiter
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	var written int
	for len(p) > 0 {
		n := len(p)
		if n > rateChunk {
			n = rateChunk
		}
		if err := lw.l.WaitN(context.Background(), n); err != nil {
			return written, err
		}
		w, err := lw.w.Write(p[:n])
		written += w
		if err != nil {
			return written, err
		}
		p = p[n:]
	}
	return written, nil
}

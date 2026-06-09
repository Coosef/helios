package httpclient

import (
	"context"
	"errors"
	"io"
	"math"
	"testing"
)

// chunkRC serves a byte slice, optionally capping bytes per Read (to exercise the
// overflow detection across multiple reads), and records Close.
type chunkRC struct {
	data   []byte
	pos    int
	chunk  int
	closed bool
}

func (r *chunkRC) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := len(r.data) - r.pos
	if n > len(p) {
		n = len(p)
	}
	if r.chunk > 0 && n > r.chunk {
		n = r.chunk
	}
	copy(p, r.data[r.pos:r.pos+n])
	r.pos += n
	return n, nil
}

func (r *chunkRC) Close() error { r.closed = true; return nil }

func bytesN(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}

func TestLimitedBodyUnderLimit(t *testing.T) {
	src := &chunkRC{data: bytesN(10)}
	got, err := io.ReadAll(newLimitedBody(src, 100))
	if err != nil {
		t.Fatalf("under-limit read errored: %v", err)
	}
	if len(got) != 10 {
		t.Errorf("read %d bytes, want 10", len(got))
	}
}

func TestLimitedBodyExactLimit(t *testing.T) {
	src := &chunkRC{data: bytesN(64)}
	got, err := io.ReadAll(newLimitedBody(src, 64))
	if err != nil {
		t.Fatalf("exact-limit read errored: %v", err)
	}
	if len(got) != 64 {
		t.Errorf("read %d bytes, want 64", len(got))
	}
}

func TestLimitedBodyOverLimit(t *testing.T) {
	src := &chunkRC{data: bytesN(65)}
	got, err := io.ReadAll(newLimitedBody(src, 64))
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("over-limit err = %v, want ErrResponseTooLarge (NO silent truncation)", err)
	}
	if int64(len(got)) > 64 {
		t.Errorf("delivered %d bytes, must not exceed the limit (64)", len(got))
	}
}

func TestLimitedBodyOverLimitChunked(t *testing.T) {
	// 1 byte per Read: overflow must still be detected across many small reads.
	src := &chunkRC{data: bytesN(100), chunk: 1}
	_, err := io.ReadAll(newLimitedBody(src, 10))
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("chunked over-limit err = %v, want ErrResponseTooLarge", err)
	}
}

func TestLimitedBodyReadAfterErrorStaysErrored(t *testing.T) {
	lb := newLimitedBody(&chunkRC{data: bytesN(100)}, 10)
	if _, err := lb.Read(make([]byte, 50)); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("first read err = %v, want ErrResponseTooLarge", err)
	}
	if _, err := lb.Read(make([]byte, 50)); !errors.Is(err, ErrResponseTooLarge) {
		t.Errorf("subsequent read err = %v, want ErrResponseTooLarge (sticky)", err)
	}
}

// A very large cap (up to math.MaxInt64, the natural "effectively unlimited"
// value) must behave as a large cap, never panic on the left+1 slice overflow.
func TestLimitedBodyHugeLimitNoOverflowPanic(t *testing.T) {
	for _, limit := range []int64{math.MaxInt64, math.MaxInt64 - 1, 1 << 40} {
		src := &chunkRC{data: bytesN(50)}
		got, err := io.ReadAll(newLimitedBody(src, limit))
		if err != nil {
			t.Errorf("limit=%d: read errored: %v", limit, err)
		}
		if len(got) != 50 {
			t.Errorf("limit=%d: read %d bytes, want 50", limit, len(got))
		}
	}
}

func TestLimitedBodyClosePropagates(t *testing.T) {
	src := &chunkRC{data: bytesN(10)}
	if err := newLimitedBody(src, 100).Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !src.closed {
		t.Error("wrapper Close did not propagate to the underlying body (leak)")
	}
}

func TestNewLimitedBodyDisabled(t *testing.T) {
	src := &chunkRC{data: bytesN(10)}
	if got := newLimitedBody(src, 0); got != io.ReadCloser(src) {
		t.Error("limit <= 0 must return the body unchanged (cap disabled)")
	}
	if got := newLimitedBody(nil, 100); got != nil {
		t.Error("nil body must pass through as nil")
	}
}

func TestEffectiveLimit(t *testing.T) {
	base := context.Background()
	cases := []struct {
		name string
		ctx  context.Context
		def  int64
		want int64
	}{
		{"no override -> default", base, 1024, 1024},
		{"override raises", WithMaxResponseBytes(base, 4096), 1024, 4096},
		{"override lowers", WithMaxResponseBytes(base, 256), 1024, 256},
		{"zero override ignored", WithMaxResponseBytes(base, 0), 1024, 1024},
		{"negative override ignored", WithMaxResponseBytes(base, -5), 1024, 1024},
		{"nil ctx -> default", nil, 1024, 1024},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := effectiveLimit(c.ctx, c.def); got != c.want {
				t.Errorf("effectiveLimit = %d, want %d", got, c.want)
			}
		})
	}
}

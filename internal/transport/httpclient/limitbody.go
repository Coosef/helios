package httpclient

import (
	"context"
	"errors"
	"io"
)

// ErrResponseTooLarge is returned (via the response body's Read) when a response
// exceeds the configured maximum size. It is a permanent, non-retryable condition:
// the body is NOT silently truncated — a too-large response fails loudly so a
// compromised/buggy server cannot OOM the agent (T13-C3) nor feed it a partial,
// half-parsed message.
var ErrResponseTooLarge = errors.New("httpclient: response body exceeds maximum allowed size")

// maxResponseBytesKey is the context key carrying a per-request size override.
type maxResponseBytesKey struct{}

// WithMaxResponseBytes returns a context that overrides the client's default
// response-size limit for requests made with it. A value <= 0 is ignored (the
// client default applies). This lets a caller that legitimately expects a larger
// body (e.g. the updater's pinned manifest fetch, T24) raise the cap WITHOUT any
// signature or generated-code change — the limit rides the existing ctx.
func WithMaxResponseBytes(ctx context.Context, n int64) context.Context {
	return context.WithValue(ctx, maxResponseBytesKey{}, n)
}

// effectiveLimit returns the per-request override from ctx when positive, else def.
func effectiveLimit(ctx context.Context, def int64) int64 {
	if ctx != nil {
		if v, ok := ctx.Value(maxResponseBytesKey{}).(int64); ok && v > 0 {
			return v
		}
	}
	return def
}

// limitedBody wraps a response body so reads beyond `limit` payload bytes fail
// with ErrResponseTooLarge instead of being read unboundedly (OOM) or silently
// truncated. It never buffers more than the limit: the (limit+1)th byte trips the
// error. Close always delegates to the wrapped body (no leak).
type limitedBody struct {
	rc    io.ReadCloser
	left  int64 // payload bytes still permitted
	erred bool
}

// newLimitedBody wraps rc to cap it at limit bytes. A nil body or non-positive
// limit returns rc unchanged (the cap is disabled).
func newLimitedBody(rc io.ReadCloser, limit int64) io.ReadCloser {
	if rc == nil || limit <= 0 {
		return rc
	}
	return &limitedBody{rc: rc, left: limit}
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.erred {
		return 0, ErrResponseTooLarge
	}
	if len(p) == 0 {
		return 0, nil
	}
	// Narrow the read window to at most left+1 bytes so a single extra byte trips
	// the limit. The condition is written as len(p)-1 > left (not len(p) > left+1)
	// so it is overflow-safe: when left is near math.MaxInt64 the branch is simply
	// not taken (the buffer is far smaller than the cap), avoiding a left+1 wrap to
	// a negative slice bound. Inside the branch left < len(p)-1, so left+1 fits.
	if int64(len(p))-1 > b.left {
		p = p[:b.left+1]
	}
	n, err := b.rc.Read(p)
	if int64(n) > b.left {
		// The body produced more than the limit: deliver the allowed prefix, then
		// fail permanently. The extra byte is discarded; the error is terminal.
		b.erred = true
		return int(b.left), ErrResponseTooLarge
	}
	b.left -= int64(n)
	return n, err
}

func (b *limitedBody) Close() error {
	return b.rc.Close()
}

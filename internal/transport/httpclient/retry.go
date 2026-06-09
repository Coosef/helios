package httpclient

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// retryableStatus reports whether an HTTP status warrants a retry: 408, 429, and
// the whole 5xx class EXCEPT 501 (Not Implemented, a permanent failure). Using
// the 5xx range — rather than an allow-list — also covers reverse-proxy/CDN codes
// such as 520-524. 4xx (incl. 426 Upgrade Required and 401 Unauthorized) are
// permanent and surfaced to the caller without retry.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests: // 408, 429
		return true
	case http.StatusNotImplemented: // 501 — permanent
		return false
	}
	return code >= 500 && code <= 599
}

// parseRetryAfter returns the Retry-After delay (delta-seconds or HTTP-date), or
// 0 if absent/unparseable.
func parseRetryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// newBackOff builds a cenkalti exponential backoff with full-ish jitter, capped
// at maxBackoff. Retry count is bounded by the caller, so MaxElapsedTime is unset.
func (c *Client) newBackOff() *backoff.ExponentialBackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = c.baseBackoff
	b.MaxInterval = c.maxBackoff
	b.Multiplier = 2
	b.RandomizationFactor = 0.5
	b.MaxElapsedTime = 0
	b.Reset()
	return b
}

// nextDelay returns the next jittered backoff delay, hard-capped at maxBackoff.
func (c *Client) nextDelay(b *backoff.ExponentialBackOff) time.Duration {
	d := b.NextBackOff()
	if d == backoff.Stop || d > c.maxBackoff {
		d = c.maxBackoff
	}
	if d < 0 {
		d = 0
	}
	return d
}

// defaultSleeper sleeps for d, returning early if the context is cancelled.
func defaultSleeper(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// drainAndClose drains a little of the body then closes it, so the keep-alive
// connection can be reused before a retry.
func drainAndClose(rc io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(rc, 4096))
	_ = rc.Close()
}

package httpclient_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

// The hardened client must satisfy the generated API client's doer interface so
// it can become the transport for enroll/register/heartbeat/poll (S1-T13).
var _ proto.HttpRequestDoer = (*httpclient.Client)(nil)

func tlsServer(t *testing.T, h http.Handler) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv, httpclient.PinFromCertificate(srv.Certificate())
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func newClient(t *testing.T, cfg httpclient.Config, opts ...httpclient.Option) *httpclient.Client {
	t.Helper()
	c, err := httpclient.New(cfg, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func doGET(t *testing.T, c *httpclient.Client, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c.Do(req)
}

func cfgFor(pin string) httpclient.Config {
	cfg := httpclient.DefaultConfig()
	cfg.Pins = []string{pin}
	cfg.ServerName = "127.0.0.1" // httptest serves on the 127.0.0.1 IP literal (no SNI)
	return cfg
}

func TestPinnedConnectionSucceeds(t *testing.T) {
	srv, pin := tlsServer(t, okHandler())
	resp, err := doGET(t, newClient(t, cfgFor(pin)), srv.URL)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestUnpinnedConnectionRefused(t *testing.T) {
	srv, _ := tlsServer(t, okHandler())
	bogus := "sha256:" + strings.Repeat("ab", 32)

	// A pin mismatch must NOT be retried (permanent failure + MITM signal).
	var slept int
	cfg := cfgFor(bogus)
	cfg.MaxRetries = 4
	c := newClient(t, cfg, httpclient.WithSleeper(func(context.Context, time.Duration) error { slept++; return nil }))

	_, err := doGET(t, c, srv.URL)
	if err == nil {
		t.Fatal("expected pin mismatch error")
	}
	if !errors.Is(err, httpclient.ErrPinMismatch) && !strings.Contains(strings.ToLower(err.Error()), "pin mismatch") {
		t.Errorf("error = %v, want pin mismatch", err)
	}
	if slept != 0 {
		t.Errorf("pin mismatch was retried %d times; must fail closed immediately", slept)
	}
}

func TestMultiplePinsOneMatches(t *testing.T) {
	srv, pin := tlsServer(t, okHandler())
	bogus := "sha256:" + strings.Repeat("cd", 32)
	for name, pins := range map[string][]string{
		"match-second": {bogus, pin},
		"match-first":  {pin, bogus},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := httpclient.DefaultConfig()
			cfg.Pins = pins
			cfg.ServerName = "127.0.0.1"
			resp, err := doGET(t, newClient(t, cfg), srv.URL)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
		})
	}
}

func TestPinMatchButWrongHostRejected(t *testing.T) {
	srv, pin := tlsServer(t, okHandler())
	cfg := cfgFor(pin)
	cfg.ServerName = "wrong.example.com" // the cert is for 127.0.0.1 / example.com
	_, err := doGET(t, newClient(t, cfg), srv.URL)
	if err == nil || (!errors.Is(err, httpclient.ErrPinMismatch) && !strings.Contains(strings.ToLower(err.Error()), "pin mismatch")) {
		t.Errorf("pin-match-but-wrong-host = %v, want rejected", err)
	}
}

func TestNoHostIdentityFailsClosed(t *testing.T) {
	srv, pin := tlsServer(t, okHandler())
	cfg := httpclient.DefaultConfig()
	cfg.Pins = []string{pin}
	cfg.ServerName = "" // IP-literal target -> empty SNI and no configured host
	_, err := doGET(t, newClient(t, cfg), srv.URL)
	if err == nil {
		t.Error("an unverifiable host (no SNI, no configured ServerName) must fail closed")
	}
}

func TestPinConfigValidation(t *testing.T) {
	if _, err := httpclient.New(httpclient.Config{}); !errors.Is(err, httpclient.ErrNoPins) {
		t.Errorf("New(no pins) = %v, want ErrNoPins", err)
	}
	if _, err := httpclient.New(httpclient.Config{Pins: []string{"not-a-pin"}}); !errors.Is(err, httpclient.ErrInvalidPin) {
		t.Errorf("New(bad pin) = %v, want ErrInvalidPin", err)
	}
	if _, err := httpclient.New(httpclient.Config{Pins: []string{"sha256:zz"}}); !errors.Is(err, httpclient.ErrInvalidPin) {
		t.Errorf("New(short pin) = %v, want ErrInvalidPin", err)
	}
}

func TestHeaderInjection(t *testing.T) {
	var got http.Header
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))

	cfg := cfgFor(pin)
	cfg.TokenProvider = func() string { return "ast_session_token_value" }
	resp, err := doGET(t, newClient(t, cfg), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if got.Get("X-Agent-Version") == "" || got.Get("X-Protocol-Version") == "" {
		t.Errorf("version headers missing: %v", got)
	}
	if got.Get("Authorization") != "Bearer ast_session_token_value" {
		t.Errorf("Authorization = %q", got.Get("Authorization"))
	}
	if !strings.Contains(got.Get("User-Agent"), "beyz-backup-agent") {
		t.Errorf("User-Agent = %q", got.Get("User-Agent"))
	}

	// No token provider -> no Authorization header.
	got = nil
	resp2, err := doGET(t, newClient(t, cfgFor(pin)), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if got.Get("Authorization") != "" {
		t.Errorf("Authorization should be absent without a token, got %q", got.Get("Authorization"))
	}
}

func TestRetryBehaviorWithCappedBackoff(t *testing.T) {
	var count int32
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Body must be replayable across retries.
		if b, _ := io.ReadAll(r.Body); string(b) != "payload" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if atomic.AddInt32(&count, 1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	var delays []time.Duration
	rec := func(_ context.Context, d time.Duration) error { delays = append(delays, d); return nil }

	cfg := cfgFor(pin)
	cfg.MaxRetries = 3
	cfg.BaseBackoff = 10 * time.Millisecond
	cfg.MaxBackoff = 100 * time.Millisecond
	c := newClient(t, cfg, httpclient.WithSleeper(rec))

	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("payload"))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("final status = %d, want 200", resp.StatusCode)
	}
	if atomic.LoadInt32(&count) != 3 {
		t.Errorf("server hits = %d, want 3", count)
	}
	if len(delays) != 2 {
		t.Fatalf("retry delays = %d, want 2", len(delays))
	}
	for _, d := range delays {
		if d <= 0 || d > cfg.MaxBackoff {
			t.Errorf("delay %v out of (0, %v]", d, cfg.MaxBackoff)
		}
	}
}

func TestRetryAfterHonored(t *testing.T) {
	var count int32
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&count, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	var delays []time.Duration
	rec := func(_ context.Context, d time.Duration) error { delays = append(delays, d); return nil }
	cfg := cfgFor(pin)
	cfg.MaxRetries = 2
	cfg.MaxBackoff = 5 * time.Second
	c := newClient(t, cfg, httpclient.WithSleeper(rec))

	resp, err := doGET(t, c, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if len(delays) != 1 || delays[0] != time.Second {
		t.Errorf("Retry-After delays = %v, want [1s]", delays)
	}
}

func TestNonRetryableStatusesNotRetried(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusUpgradeRequired} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var count int32
			srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				atomic.AddInt32(&count, 1)
				w.WriteHeader(code)
			}))
			var slept int
			cfg := cfgFor(pin)
			cfg.MaxRetries = 3
			c := newClient(t, cfg, httpclient.WithSleeper(func(context.Context, time.Duration) error { slept++; return nil }))

			resp, err := doGET(t, c, srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != code {
				t.Errorf("status = %d, want %d", resp.StatusCode, code)
			}
			if count != 1 || slept != 0 {
				t.Errorf("non-retryable %d was retried: hits=%d slept=%d", code, count, slept)
			}
		})
	}
}

func TestTransientNetworkErrorRetried(t *testing.T) {
	var n int32
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			// Abruptly drop the connection so the client sees a network error.
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					_ = conn.Close()
				}
			}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	var slept int
	cfg := cfgFor(pin)
	cfg.MaxRetries = 2
	c := newClient(t, cfg, httpclient.WithSleeper(func(context.Context, time.Duration) error { slept++; return nil }))

	resp, err := doGET(t, c, srv.URL)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || atomic.LoadInt32(&n) != 2 || slept != 1 {
		t.Errorf("transient error not retried as expected: status=%d hits=%d slept=%d", resp.StatusCode, n, slept)
	}
}

func TestRetryUsesRealSleeper(t *testing.T) {
	var n int32
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	cfg := cfgFor(pin)
	cfg.MaxRetries = 2
	cfg.BaseBackoff = time.Millisecond
	cfg.MaxBackoff = 5 * time.Millisecond
	resp, err := doGET(t, newClient(t, cfg), srv.URL) // real sleeper
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRetryAfterCappedAtMaxBackoff(t *testing.T) {
	var count int32
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&count, 1) == 1 {
			w.Header().Set("Retry-After", "10")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	var delays []time.Duration
	cfg := cfgFor(pin)
	cfg.MaxRetries = 2
	cfg.MaxBackoff = 2 * time.Second
	c := newClient(t, cfg, httpclient.WithSleeper(func(_ context.Context, d time.Duration) error {
		delays = append(delays, d)
		return nil
	}))
	resp, err := doGET(t, c, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if len(delays) != 1 || delays[0] != 2*time.Second {
		t.Errorf("Retry-After delay = %v, want capped to 2s", delays)
	}
}

func TestBodyReplayedWhenRequestNotRewindable(t *testing.T) {
	var count int32
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != "payload" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if atomic.AddInt32(&count, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	req.Body = io.NopCloser(strings.NewReader("payload")) // GetBody intentionally nil
	req.GetBody = nil

	cfg := cfgFor(pin)
	cfg.MaxRetries = 2
	c := newClient(t, cfg, httpclient.WithSleeper(func(context.Context, time.Duration) error { return nil }))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || atomic.LoadInt32(&count) != 2 {
		t.Errorf("body not replayed across retry: status=%d hits=%d", resp.StatusCode, count)
	}
}

func TestSleeperErrorAbortsRetry(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	wantErr := errors.New("context cancelled during backoff")
	cfg := cfgFor(pin)
	cfg.MaxRetries = 3
	c := newClient(t, cfg, httpclient.WithSleeper(func(context.Context, time.Duration) error { return wantErr }))
	if _, err := doGET(t, c, srv.URL); !errors.Is(err, wantErr) {
		t.Errorf("Do = %v, want the sleeper error", err)
	}
}

func TestRequestTimeout(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	cfg := cfgFor(pin)
	cfg.RequestTimeout = 40 * time.Millisecond
	cfg.MaxRetries = 0
	if _, err := doGET(t, newClient(t, cfg), srv.URL); err == nil {
		t.Fatal("expected a timeout error")
	}
}

func TestPerAttemptTimeoutIsRetried(t *testing.T) {
	var n int32
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			time.Sleep(150 * time.Millisecond) // first attempt exceeds RequestTimeout
		}
		w.WriteHeader(http.StatusOK)
	}))
	cfg := cfgFor(pin)
	cfg.RequestTimeout = 40 * time.Millisecond
	cfg.MaxRetries = 3
	c := newClient(t, cfg, httpclient.WithSleeper(func(context.Context, time.Duration) error { return nil }))

	resp, err := doGET(t, c, srv.URL)
	if err != nil {
		t.Fatalf("a transient per-attempt timeout must be retried, got: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || atomic.LoadInt32(&n) < 2 {
		t.Errorf("per-attempt timeout not retried: status=%d hits=%d", resp.StatusCode, n)
	}
}

func TestCallerCancellationIsTerminal(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	var slept int
	cfg := cfgFor(pin)
	cfg.MaxRetries = 3
	c := newClient(t, cfg, httpclient.WithSleeper(func(context.Context, time.Duration) error { slept++; return nil }))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // caller already cancelled
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if _, err := c.Do(req); err == nil {
		t.Error("a cancelled caller context must terminate Do")
	}
	if slept != 0 {
		t.Errorf("caller cancellation must not retry, slept=%d", slept)
	}
}

func TestFullFiveXXRangeRetried(t *testing.T) {
	var n int32
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(520) // Cloudflare-style code outside the classic 5xx allow-list
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	cfg := cfgFor(pin)
	cfg.MaxRetries = 2
	c := newClient(t, cfg, httpclient.WithSleeper(func(context.Context, time.Duration) error { return nil }))
	resp, err := doGET(t, c, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || atomic.LoadInt32(&n) != 2 {
		t.Errorf("520 not retried: status=%d hits=%d", resp.StatusCode, n)
	}
}

func TestExplicitProxyIsUsed(t *testing.T) {
	var hits int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "expected CONNECT", http.StatusMethodNotAllowed)
			return
		}
		atomic.AddInt32(&hits, 1)
		dst, err := net.DialTimeout("tcp", r.Host, 5*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		src, _, err := hj.Hijack()
		if err != nil {
			_ = dst.Close()
			return
		}
		_, _ = src.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() { _, _ = io.Copy(dst, src); _ = dst.Close() }()
		_, _ = io.Copy(src, dst)
		_ = src.Close()
	}))
	t.Cleanup(proxy.Close)

	srv, pin := tlsServer(t, okHandler())
	cfg := cfgFor(pin)
	cfg.Proxy = proxy.URL
	resp, err := doGET(t, newClient(t, cfg), srv.URL)
	if err != nil {
		t.Fatalf("Do via proxy: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if atomic.LoadInt32(&hits) == 0 {
		t.Error("request did not go through the configured proxy")
	}
}

func TestMalformedProxyRejected(t *testing.T) {
	cfg := httpclient.Config{Pins: []string{"sha256:" + strings.Repeat("00", 32)}, Proxy: "://bad-url"}
	if _, err := httpclient.New(cfg); err == nil {
		t.Error("New with malformed proxy should error")
	}
}

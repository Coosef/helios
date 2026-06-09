// Package httpclient is the agent's hardened control-channel HTTP client: the
// reusable transport for enrollment, registration, heartbeat, and task polling.
//
// Security/transport guarantees (SEC-4, GAP-5/8, SCALE-1/3):
//   - TLS 1.2 minimum (1.3 preferred), HTTP/2, IPv4/IPv6, keep-alive + pooling.
//   - SPKI public-key PINNING is the trust anchor (a pin SET supports rotation);
//     a server presenting an unpinned key is refused (ErrPinMismatch). System-CA-
//     only trust is not relied upon for the control channel.
//   - Exponential backoff with jitter (cenkalti/backoff/v4), capped at MaxBackoff;
//     retries 5xx/429/408 (honoring Retry-After), surfaces 401/426 without retry.
//   - System + explicit proxy support.
//   - Auto-injects X-Agent-Version / X-Protocol-Version (pkg/wireversion) and the
//     bearer Authorization header (from a token provider).
//
// The client NEVER logs (no logger dependency): bearer/enrollment tokens, certs,
// and keys cannot leak from here. Client.Do satisfies pkg/proto's HttpRequestDoer,
// so the generated API client plugs straight in (S1-T13).
//
// Scope (S1-T12): the transport only. No enrollment or heartbeat logic.
package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/beyzbackup/beyz-backup/pkg/wireversion"
)

// Defaults applied when a Config field is left zero (timeouts/backoff intervals).
const (
	defaultDialTimeout           = 10 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultResponseHeaderTimeout = 30 * time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultRequestTimeout        = 60 * time.Second
	defaultBaseBackoff           = 500 * time.Millisecond
	defaultMaxBackoff            = 30 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultMaxIdleConns          = 100
	defaultMaxIdleConnsPerHost   = 10
)

// Config configures a Client.
type Config struct {
	// Pins is the REQUIRED SPKI pin set ("sha256:<hex>"). Multiple pins enable
	// rotation with overlapping validity (§0.5).
	Pins []string
	// TokenProvider returns the current bearer token, or "" for none (e.g. the
	// unauthenticated enroll call). Called per request.
	TokenProvider func() string
	// Proxy is an explicit proxy URL; empty uses the environment proxy.
	Proxy string
	// ServerName is the expected server host for certificate identity
	// verification. It is REQUIRED when the endpoint is addressed by an IP literal
	// (where TLS SNI is empty); for DNS endpoints the SNI is used when this is
	// empty. Set it to the host of the control-plane base URL.
	ServerName string
	// RootCAs optionally restricts chain roots; pinning is the anchor regardless.
	RootCAs *x509.CertPool

	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	RequestTimeout        time.Duration // per-attempt overall timeout

	// MaxRetries is the number of RETRIES after the first attempt (0 = no retry).
	MaxRetries  int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
}

// DefaultConfig returns production defaults (without pins). Set Pins (and usually
// TokenProvider) before calling New.
func DefaultConfig() Config {
	return Config{
		DialTimeout:           defaultDialTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		ResponseHeaderTimeout: defaultResponseHeaderTimeout,
		IdleConnTimeout:       defaultIdleConnTimeout,
		RequestTimeout:        defaultRequestTimeout,
		MaxRetries:            4,
		BaseBackoff:           defaultBaseBackoff,
		MaxBackoff:            defaultMaxBackoff,
	}
}

// Client is the hardened HTTP client. It is safe for concurrent use.
type Client struct {
	http          *http.Client
	tokenProvider func() string
	maxRetries    int
	baseBackoff   time.Duration
	maxBackoff    time.Duration
	sleep         func(context.Context, time.Duration) error
}

// Option customizes a Client (mainly for tests).
type Option func(*Client)

// WithSleeper overrides the inter-retry sleep function (tests inject a recorder).
func WithSleeper(f func(context.Context, time.Duration) error) Option {
	return func(c *Client) {
		if f != nil {
			c.sleep = f
		}
	}
}

func orDefault(v, d time.Duration) time.Duration {
	if v <= 0 {
		return d
	}
	return v
}

// New builds a Client from cfg. It fails closed if no valid pins are configured.
func New(cfg Config, opts ...Option) (*Client, error) {
	pinset, err := normalizePins(cfg.Pins)
	if err != nil {
		return nil, err
	}
	hc, err := buildHTTPClient(cfg, pinset)
	if err != nil {
		return nil, err
	}
	c := &Client{
		http:          hc,
		tokenProvider: cfg.TokenProvider,
		maxRetries:    cfg.MaxRetries,
		baseBackoff:   orDefault(cfg.BaseBackoff, defaultBaseBackoff),
		maxBackoff:    orDefault(cfg.MaxBackoff, defaultMaxBackoff),
		sleep:         defaultSleeper,
	}
	if c.maxRetries < 0 {
		c.maxRetries = 0
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

func buildHTTPClient(cfg Config, pinset map[string]struct{}) (*http.Client, error) {
	proxyFn := http.ProxyFromEnvironment
	if cfg.Proxy != "" {
		u, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("httpclient: invalid proxy url %q: %w", cfg.Proxy, err)
		}
		proxyFn = http.ProxyURL(u)
	}

	// InsecureSkipVerify is intentional: Go's default chain/hostname verification
	// is replaced by VerifyConnection, which enforces SPKI pinning (the trust
	// anchor) PLUS a hostname check. This is stronger than system-CA-only trust,
	// not weaker (SEC-4).
	tlsCfg := &tls.Config{ // #nosec G402 -- pinning replaces default verification in VerifyConnection
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{"h2", "http/1.1"},
		RootCAs:            cfg.RootCAs,
		InsecureSkipVerify: true,
		VerifyConnection:   pinVerifier(pinset, cfg.ServerName),
	}

	tr := &http.Transport{
		Proxy: proxyFn,
		DialContext: (&net.Dialer{
			Timeout:   orDefault(cfg.DialTimeout, defaultDialTimeout),
			KeepAlive: defaultKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       tlsCfg,
		TLSHandshakeTimeout:   orDefault(cfg.TLSHandshakeTimeout, defaultTLSHandshakeTimeout),
		ResponseHeaderTimeout: orDefault(cfg.ResponseHeaderTimeout, defaultResponseHeaderTimeout),
		IdleConnTimeout:       orDefault(cfg.IdleConnTimeout, defaultIdleConnTimeout),
		MaxIdleConns:          defaultMaxIdleConns,
		MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: tr,
		Timeout:   orDefault(cfg.RequestTimeout, defaultRequestTimeout),
	}, nil
}

// Do executes req with header injection, pinned TLS, and retry/backoff. It
// satisfies pkg/proto's HttpRequestDoer. The request body is buffered so it can
// be replayed across retries.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	c.injectHeaders(req)
	if err := makeRewindable(req); err != nil {
		return nil, err
	}

	callerCtx := req.Context()
	b := c.newBackOff()
	var nextDelay time.Duration

	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := rewindBody(req); err != nil {
				return nil, err
			}
			if err := c.sleep(callerCtx, nextDelay); err != nil {
				return nil, err
			}
		}

		resp, err := c.http.Do(req)
		if err != nil {
			// Terminal: the CALLER cancelled/deadlined (checked on callerCtx, NOT
			// the error type — a per-attempt RequestTimeout also yields a
			// context-deadline error but leaves callerCtx live and IS retried), or
			// a pin mismatch (permanent + MITM signal), or retries are exhausted.
			if callerCtx.Err() != nil || errors.Is(err, ErrPinMismatch) || attempt >= c.maxRetries {
				return nil, err
			}
			nextDelay = c.nextDelay(b)
			continue
		}

		if !retryableStatus(resp.StatusCode) || attempt >= c.maxRetries {
			return resp, nil // success, non-retryable (incl. 401/426), or retries exhausted
		}

		if ra := parseRetryAfter(resp); ra > 0 {
			nextDelay = ra
			if nextDelay > c.maxBackoff {
				nextDelay = c.maxBackoff
			}
		} else {
			nextDelay = c.nextDelay(b)
		}
		drainAndClose(resp.Body)
	}
}

// injectHeaders sets the version headers, a non-secret User-Agent, and (when a
// token is available and not already set) the bearer Authorization header.
func (c *Client) injectHeaders(req *http.Request) {
	wireversion.ApplyHeaders(req)
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "beyz-backup-agent/"+wireversion.AgentVersion())
	}
	if c.tokenProvider != nil && req.Header.Get("Authorization") == "" {
		if tok := c.tokenProvider(); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
}

// makeRewindable ensures req.GetBody is set so the body can be replayed on retry.
func makeRewindable(req *http.Request) error {
	if req.Body == nil || req.GetBody != nil {
		return nil
	}
	data, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return fmt.Errorf("httpclient: buffering request body: %w", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return nil
}

func rewindBody(req *http.Request) error {
	if req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil {
		return fmt.Errorf("httpclient: rewinding request body: %w", err)
	}
	req.Body = body
	return nil
}

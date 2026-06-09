// Package saasclient is the thin typed transport adapter between the agent
// runtime and the SaaS control-plane API.
//
// It performs request/response ONLY: each method maps to one generated pkg/proto
// call over the hardened, SPKI-pinned S1-T12 transport, classifies the HTTP
// status, and returns the typed response body. It contains NO orchestration,
// scheduling, retry policy (that lives in S1-T12), enrollment workflow, or
// business logic.
//
// ADR-005 (T12 transport approval) integration contract:
//   - ServerName is derived from the api_base_url host and set on the T12 client
//     (required for IP-literal endpoints).
//   - The bearer session token is kept IN MEMORY (no DPAPI unwrap per request).
//     Enroll/Register/Heartbeat refresh the in-memory cache when the server issues
//     or rotates a token; PERSISTING it to the state store is the CALLER's job.
//   - 401 Unauthorized and 426 Upgrade Required are surfaced as the sentinel
//     errors ErrUnauthorized / ErrUpgradeRequired for the CALLER to handle.
//   - Version headers (X-Agent-Version / X-Protocol-Version) are injected by the
//     T12 transport via pkg/wireversion; this layer adds no duplicate editor.
//   - Mutating calls (Enroll/Register/AckTask/ReportTaskStatus) optionally accept a
//     caller-supplied Idempotency-Key (WithIdempotencyKey) so the SaaS can dedupe a
//     retried request (T12 retries POSTs). The caller owns key generation and reuse
//     across retries of the SAME logical operation; this adapter only threads it.
//   - Control-channel only — never used for large backup-payload transfer.
//
// The caller builds request bodies from S1-T11 identity material and persists
// responses to the S1-T10 state store; this thin adapter does not import them
// directly, keeping it transport-only.
package saasclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

// Sentinel errors the CALLER is responsible for handling (ADR-005 C4).
var (
	// ErrUnauthorized is returned on HTTP 401 (token invalid/expired); the caller
	// refreshes the session token or re-enrolls.
	ErrUnauthorized = errors.New("saasclient: 401 unauthorized")
	// ErrUpgradeRequired is returned on HTTP 426; the caller stops calling and
	// routes to the updater (ADR-004).
	ErrUpgradeRequired = errors.New("saasclient: 426 upgrade required")
	// ErrConflict is returned on HTTP 409 (e.g. an enrollment token already
	// consumed / a duplicate that the server refuses); the caller decides whether
	// to treat it as a rejection or a benign already-applied outcome.
	ErrConflict = errors.New("saasclient: 409 conflict")
	// ErrUnexpectedStatus is returned for any other non-2xx status.
	ErrUnexpectedStatus = errors.New("saasclient: unexpected status")
	// ErrEmptyBody is returned when a 2xx response carries no decodable body.
	ErrEmptyBody = errors.New("saasclient: empty response body")
)

// Options configures the SaaS client.
type Options struct {
	// BaseURL is the control-plane base URL (api_base_url).
	BaseURL string
	// Pins is the SPKI pin set ("sha256:<hex>") for the control channel (ADR-005).
	Pins []string
	// HTTPConfig optionally overrides transport defaults. Pins, ServerName, and
	// TokenProvider are always set by this package and take precedence.
	HTTPConfig *httpclient.Config
}

// Client is the thin typed transport adapter. It is safe for concurrent use.
type Client struct {
	api *proto.ClientWithResponses

	mu    sync.RWMutex
	token string // cached in-memory bearer session token
}

// New builds a Client over the hardened S1-T12 transport.
func New(opts Options) (*Client, error) {
	if opts.BaseURL == "" {
		return nil, fmt.Errorf("saasclient: empty base url")
	}
	u, err := url.Parse(opts.BaseURL)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("saasclient: invalid base url %q", opts.BaseURL)
	}

	c := &Client{}

	cfg := httpclient.DefaultConfig()
	if opts.HTTPConfig != nil {
		cfg = *opts.HTTPConfig
	}
	cfg.Pins = opts.Pins
	cfg.ServerName = u.Hostname()      // ADR-005: ServerName from api_base_url host
	cfg.TokenProvider = c.currentToken // ADR-005: in-memory cached token, no DPAPI per request

	hc, err := httpclient.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("saasclient: building transport: %w", err)
	}
	api, err := proto.NewClientWithResponses(opts.BaseURL, proto.WithHTTPClient(hc))
	if err != nil {
		return nil, fmt.Errorf("saasclient: building api client: %w", err)
	}
	c.api = api
	return c, nil
}

// SetSessionToken seeds/updates the in-memory bearer token. The caller seeds it
// at startup (from the state store) and persists rotations it reads from
// responses; this package only keeps the in-memory copy current.
func (c *Client) SetSessionToken(token string) {
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
}

// SessionToken returns the cached in-memory bearer token.
func (c *Client) SessionToken() string { return c.currentToken() }

func (c *Client) currentToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

func (c *Client) cacheToken(token string) {
	if token == "" {
		return
	}
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
}

// ---- per-request options ---------------------------------------------------

// RequestOption customizes a single mutating request. It does not change
// transport, retry, version-header, or token-rotation behavior.
type RequestOption func(*requestOptions)

type requestOptions struct {
	idempotencyKey *proto.IdempotencyKey
}

// WithIdempotencyKey attaches a caller-generated Idempotency-Key (ADR-004 / §0.2)
// to a mutating call so the SaaS server can dedupe a retried request (T12 retries
// POSTs). The caller owns key generation and stability across retries of the SAME
// logical operation; this adapter only threads the key into the request. It is a
// no-op for non-mutating calls (Heartbeat / PollTasks carry no such header).
func WithIdempotencyKey(key proto.IdempotencyKey) RequestOption {
	return func(o *requestOptions) {
		k := key
		o.idempotencyKey = &k
	}
}

func applyRequestOptions(opts []RequestOption) requestOptions {
	var o requestOptions
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	return o
}

// ---- typed methods (request/response only) ---------------------------------

// Enroll consumes the enrollment token and returns the device credential. On
// success the issued session token is cached in memory.
func (c *Client) Enroll(ctx context.Context, body proto.EnrollRequest, opts ...RequestOption) (*proto.EnrollResponse, error) {
	o := applyRequestOptions(opts)
	var params *proto.EnrollAgentParams
	if o.idempotencyKey != nil {
		params = &proto.EnrollAgentParams{IdempotencyKey: o.idempotencyKey}
	}
	resp, err := c.api.EnrollAgentWithResponse(ctx, params, body)
	if err != nil {
		return nil, fmt.Errorf("saasclient: enroll: %w", err)
	}
	if err := classify(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	if resp.JSON201 == nil {
		return nil, fmt.Errorf("saasclient: enroll: %w", ErrEmptyBody)
	}
	c.cacheToken(resp.JSON201.AgentSessionToken)
	return resp.JSON201, nil
}

// Register confirms device facts and renews the certificate/session token. The
// rotated session token is cached in memory.
func (c *Client) Register(ctx context.Context, deviceID string, body proto.RegisterRequest, opts ...RequestOption) (*proto.RegisterResponse, error) {
	o := applyRequestOptions(opts)
	var params *proto.RegisterAgentParams
	if o.idempotencyKey != nil {
		params = &proto.RegisterAgentParams{IdempotencyKey: o.idempotencyKey}
	}
	resp, err := c.api.RegisterAgentWithResponse(ctx, deviceID, params, body)
	if err != nil {
		return nil, fmt.Errorf("saasclient: register: %w", err)
	}
	if err := classify(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("saasclient: register: %w", ErrEmptyBody)
	}
	c.cacheToken(resp.JSON200.AgentSessionToken)
	return resp.JSON200, nil
}

// Heartbeat sends the presence payload. If the server rotates the session token,
// the new one is cached in memory.
func (c *Client) Heartbeat(ctx context.Context, deviceID string, body proto.HeartbeatRequest) (*proto.HeartbeatResponse, error) {
	resp, err := c.api.SendHeartbeatWithResponse(ctx, deviceID, nil, body)
	if err != nil {
		return nil, fmt.Errorf("saasclient: heartbeat: %w", err)
	}
	if err := classify(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("saasclient: heartbeat: %w", ErrEmptyBody)
	}
	if resp.JSON200.AgentSessionToken != nil {
		c.cacheToken(*resp.JSON200.AgentSessionToken)
	}
	return resp.JSON200, nil
}

// PollTasks fetches the next batch of tasks.
func (c *Client) PollTasks(ctx context.Context, deviceID string) (*proto.TasksResponse, error) {
	resp, err := c.api.PollTasksWithResponse(ctx, deviceID, nil)
	if err != nil {
		return nil, fmt.Errorf("saasclient: poll tasks: %w", err)
	}
	if err := classify(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("saasclient: poll tasks: %w", ErrEmptyBody)
	}
	return resp.JSON200, nil
}

// AckTask acknowledges receipt of a task.
func (c *Client) AckTask(ctx context.Context, deviceID, taskID string, body proto.TaskAckRequest, opts ...RequestOption) (*proto.TaskAckResponse, error) {
	o := applyRequestOptions(opts)
	var params *proto.AckTaskParams
	if o.idempotencyKey != nil {
		params = &proto.AckTaskParams{IdempotencyKey: o.idempotencyKey}
	}
	resp, err := c.api.AckTaskWithResponse(ctx, deviceID, taskID, params, body)
	if err != nil {
		return nil, fmt.Errorf("saasclient: ack task: %w", err)
	}
	if err := classify(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("saasclient: ack task: %w", ErrEmptyBody)
	}
	return resp.JSON200, nil
}

// ReportTaskStatus reports task progress / terminal status.
func (c *Client) ReportTaskStatus(ctx context.Context, deviceID, taskID string, body proto.TaskStatusRequest, opts ...RequestOption) (*proto.TaskStatusResponse, error) {
	o := applyRequestOptions(opts)
	var params *proto.ReportTaskStatusParams
	if o.idempotencyKey != nil {
		params = &proto.ReportTaskStatusParams{IdempotencyKey: o.idempotencyKey}
	}
	resp, err := c.api.ReportTaskStatusWithResponse(ctx, deviceID, taskID, params, body)
	if err != nil {
		return nil, fmt.Errorf("saasclient: report task status: %w", err)
	}
	if err := classify(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("saasclient: report task status: %w", ErrEmptyBody)
	}
	return resp.JSON200, nil
}

// ---- status classification -------------------------------------------------

func classify(status int, body []byte) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == 401:
		return fmt.Errorf("%w: %s", ErrUnauthorized, problemDetail(body))
	case status == 426:
		return fmt.Errorf("%w: %s", ErrUpgradeRequired, problemDetail(body))
	case status == 409:
		return fmt.Errorf("%w: %s", ErrConflict, problemDetail(body))
	default:
		return fmt.Errorf("%w (%d): %s", ErrUnexpectedStatus, status, problemDetail(body))
	}
}

// problemDetail extracts a short, non-sensitive summary from an RFC 9457
// problem+json body (control-plane error bodies never carry secrets). It is
// length-bounded and falls back to a trimmed raw body.
func problemDetail(body []byte) string {
	const maxLen = 256
	var p struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Code   string `json:"code"`
	}
	if err := json.Unmarshal(body, &p); err == nil && (p.Title != "" || p.Detail != "" || p.Code != "") {
		s := p.Title
		if p.Code != "" {
			s += " [" + p.Code + "]"
		}
		if p.Detail != "" {
			s += ": " + p.Detail
		}
		return truncate(s, maxLen)
	}
	return truncate(string(body), maxLen)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

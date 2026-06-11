package swap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

var (
	// ErrDownload wraps any artifact-download failure (network, non-200, write).
	ErrDownload = errors.New("swap: download failed")
	// ErrSizeExceeded is returned when the artifact exceeds the size cap (fail
	// closed; no truncation, no OOM).
	ErrSizeExceeded = errors.New("swap: artifact exceeds size cap")
)

// Downloader streams an artifact from url into dst, bounded by maxBytes. It is the
// binary-fetch seam (injectable for tests). The default is plain HTTPS — integrity
// comes from the SIGNED dual-hash (verify.Artifact), not the transport (ADR-002),
// so the artifact may be served from any HTTPS CDN.
type Downloader interface {
	Download(ctx context.Context, url string, dst io.Writer, maxBytes int64) (int64, error)
}

// HTTPDownloader is a hardened (TLS 1.2+, bounded timeouts) streaming HTTPS
// downloader. It is NOT SPKI-pinned (the binary host differs from the pinned
// control plane); integrity is the signed hash.
type HTTPDownloader struct{ client *http.Client }

// NewHTTPDownloader wraps client (nil builds a hardened default).
func NewHTTPDownloader(client *http.Client) *HTTPDownloader {
	if client == nil {
		client = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}}
	}
	return &HTTPDownloader{client: client}
}

// Download streams the body into dst, reading at most maxBytes+1 so an oversize
// body is detected (ErrSizeExceeded) rather than silently truncated.
func (d *HTTPDownloader) Download(ctx context.Context, url string, dst io.Writer, maxBytes int64) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrDownload, err)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrDownload, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%w: status %d", ErrDownload, resp.StatusCode)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return n, fmt.Errorf("%w: %w", ErrDownload, err)
	}
	if n > maxBytes {
		return n, fmt.Errorf("%w: %d bytes > cap %d", ErrSizeExceeded, n, maxBytes)
	}
	return n, nil
}

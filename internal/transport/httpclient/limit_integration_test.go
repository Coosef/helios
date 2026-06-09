package httpclient_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
)

// sizedHandler returns exactly n bytes with a 200.
func sizedHandler(n int) http.Handler {
	body := make([]byte, n)
	for i := range body {
		body[i] = 'x'
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}

func cfgWithLimit(pin string, limit int64) httpclient.Config {
	cfg := cfgFor(pin)
	cfg.MaxResponseBytes = limit
	return cfg
}

func TestDefaultConfigHasOneMiBResponseCap(t *testing.T) {
	if got := httpclient.DefaultConfig().MaxResponseBytes; got != 1<<20 {
		t.Errorf("DefaultConfig().MaxResponseBytes = %d, want %d (1 MiB)", got, 1<<20)
	}
}

func TestResponseUnderLimitReadsFully(t *testing.T) {
	srv, pin := tlsServer(t, sizedHandler(500))
	resp, err := doGET(t, newClient(t, cfgWithLimit(pin, 1000)), srv.URL)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll under limit errored: %v", err)
	}
	if len(body) != 500 {
		t.Errorf("read %d bytes, want 500", len(body))
	}
}

func TestResponseExactLimitReadsFully(t *testing.T) {
	srv, pin := tlsServer(t, sizedHandler(500))
	resp, err := doGET(t, newClient(t, cfgWithLimit(pin, 500)), srv.URL)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll at exact limit errored: %v", err)
	}
	if len(body) != 500 {
		t.Errorf("read %d bytes, want 500", len(body))
	}
}

func TestResponseOverLimitRejected(t *testing.T) {
	srv, pin := tlsServer(t, sizedHandler(2000))
	resp, err := doGET(t, newClient(t, cfgWithLimit(pin, 1000)), srv.URL)
	if err != nil {
		t.Fatalf("Do (status read) should succeed; the cap applies on body read: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.ReadAll(resp.Body)
	if !errors.Is(err, httpclient.ErrResponseTooLarge) {
		t.Errorf("over-limit body err = %v, want ErrResponseTooLarge", err)
	}
}

func TestContextOverrideRaisesLimit(t *testing.T) {
	srv, pin := tlsServer(t, sizedHandler(2000))
	c := newClient(t, cfgWithLimit(pin, 1000)) // default cap would reject 2000
	ctx := httpclient.WithMaxResponseBytes(context.Background(), 4000)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("override should permit 2000 bytes under a 4000 cap, got: %v", err)
	}
	if len(body) != 2000 {
		t.Errorf("read %d bytes, want 2000", len(body))
	}
}

func TestContextOverrideLowersLimit(t *testing.T) {
	srv, pin := tlsServer(t, sizedHandler(2000))
	c := newClient(t, cfgWithLimit(pin, 1<<20)) // generous default
	ctx := httpclient.WithMaxResponseBytes(context.Background(), 500)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); !errors.Is(err, httpclient.ErrResponseTooLarge) {
		t.Errorf("lowered override err = %v, want ErrResponseTooLarge", err)
	}
}

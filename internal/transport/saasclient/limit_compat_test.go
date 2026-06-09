package saasclient_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/internal/transport/saasclient"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

// An oversized 200 response must surface ErrResponseTooLarge through saasclient,
// not OOM and not a silently-truncated/garbled parse.
func TestSaasclientOversizedResponseRejected(t *testing.T) {
	big := `{"agent_session_token":"ast_` + strings.Repeat("x", 4000) + `"}`
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "application/json", big)
	}))

	c, err := saasclient.New(saasclient.Options{
		BaseURL:    srv.URL,
		Pins:       []string{pin},
		HTTPConfig: &httpclient.Config{MaxRetries: 0, MaxResponseBytes: 256},
	})
	if err != nil {
		t.Fatalf("saasclient.New: %v", err)
	}
	c.SetSessionToken("ast_old")
	if _, err := c.Heartbeat(ctx(), "dev_1", proto.HeartbeatRequest{}); !errors.Is(err, httpclient.ErrResponseTooLarge) {
		t.Errorf("heartbeat over cap err = %v, want ErrResponseTooLarge", err)
	}
}

// A normal-sized response must still parse cleanly under a modest cap — proving
// the wrapping body is transparent to the generated proto parser (no regression).
func TestSaasclientNormalResponseUnderCapParses(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "application/json", `{"agent_session_token":"ast_hb_new"}`)
	}))
	c, err := saasclient.New(saasclient.Options{
		BaseURL:    srv.URL,
		Pins:       []string{pin},
		HTTPConfig: &httpclient.Config{MaxRetries: 0, MaxResponseBytes: 4096},
	})
	if err != nil {
		t.Fatalf("saasclient.New: %v", err)
	}
	c.SetSessionToken("ast_old")
	if _, err := c.Heartbeat(ctx(), "dev_1", proto.HeartbeatRequest{}); err != nil {
		t.Fatalf("normal heartbeat under cap should parse, got: %v", err)
	}
	if c.SessionToken() != "ast_hb_new" {
		t.Errorf("token not rotated through the capped transport: %q", c.SessionToken())
	}
}

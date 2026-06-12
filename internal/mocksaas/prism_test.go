package mocksaas_test

import (
	"bytes"
	"net/http"
	"os"
	"testing"
	"time"
)

// validEnroll is a SPEC-VALID enroll request (the openapi escrowedDefault example
// shape: all required fields). Prism mock validates requests against the spec, so a
// 2xx (and the Prefer-selected negative) require a conforming request.
const validEnroll = `{` +
	`"enrollment_token":"bzt_2f8c1d6e9a4b7c0d3e5f1a2b3c4d5e6f",` +
	`"device_guid":"9d8f7a6b-5c4d-4e3f-8a1b-0c9d8e7f6a5b",` +
	`"csr_pem":"-----BEGIN CERTIFICATE REQUEST-----\nMIIBVTCB\n-----END CERTIFICATE REQUEST-----",` +
	`"recovery_policy":"escrowed",` +
	`"recovery_material_ack":false,` +
	`"agent_version":"1.0.0",` +
	`"supported_format_versions":{"manifest":1,"chunk":1,"crypto_envelope":1}` +
	`}`

// TestPrismControlPlane exercises the Prism control-plane mock generated from
// api/openapi.yaml. SKIPPED unless PRISM_URL is set (CI/T31 boots Prism via
// `task contract`; locally: `npx @stoplight/prism-cli mock api/openapi.yaml`).
func TestPrismControlPlane(t *testing.T) {
	base := os.Getenv("PRISM_URL")
	if base == "" {
		t.Skip("set PRISM_URL (e.g. http://127.0.0.1:4010) to run; CI boots Prism")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	versioned := http.Header{
		"X-Agent-Version":    []string{"1.0.0"},
		"X-Protocol-Version": []string{"1"},
	}

	// (1) The mock starts and serves the enroll example for a spec-valid request.
	if got := post(t, client, base+"/v1/enroll", validEnroll, versioned); got/100 != 2 {
		t.Errorf("POST /v1/enroll (valid): status %d, want 2xx", got)
	}

	// (2) Prefer: code=409 selects the token-consumed response — the stateless way to
	// let the agent's 409-handling test (AC-15) exercise that path (Prism cannot track
	// consumed tokens). Requires a spec-valid request body too.
	pref := versioned.Clone()
	pref.Set("Prefer", "code=409")
	if got := post(t, client, base+"/v1/enroll", validEnroll, pref); got != http.StatusConflict {
		t.Errorf("Prefer code=409: status %d, want 409", got)
	}

	// (3) The mock ENFORCES the OpenAPI contract: a non-conforming request is rejected.
	// This is the contract-mock's core value (it guards the agent's request shape).
	if got := post(t, client, base+"/v1/enroll", `{"enrollment_token":"not-conforming"}`, versioned); got != http.StatusUnprocessableEntity {
		t.Errorf("invalid request: status %d, want 422 (contract enforced)", got)
	}
}

func post(t *testing.T, c *http.Client, url, body string, h http.Header) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, vs := range h {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

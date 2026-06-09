package audit

import (
	"strings"
	"testing"
)

func sampleRecord() Record {
	return Record{
		SchemaVersion: SchemaVersion,
		Seq:           1,
		PrevHash:      "blake3:aa",
		TSLocal:       "2026-01-01T00:00:00Z",
		Source:        SourceAgent,
		EventType:     "auth.failure",
		Category:      CategoryAuth,
		Severity:      SeverityWarn,
		Outcome:       OutcomeFailure,
		Actor:         "system",
		TenantID:      "t1",
		DeviceID:      "d1",
		AgentVersion:  "1.0.0",
		Detail:        map[string]any{"k": "v", "n": 1},
	}
}

func TestCanonicalDeterministicAndReservedExcluded(t *testing.T) {
	r := sampleRecord()

	h1, err := computeThisHash(r)
	if err != nil {
		t.Fatalf("computeThisHash: %v", err)
	}
	h2, _ := computeThisHash(r)
	if h1 != h2 {
		t.Errorf("canonicalization not deterministic: %q vs %q", h1, h2)
	}

	// Filling the hash-excluded fields must NOT change the hash, so the server
	// can enrich a record (ts_server / signature / anchor proof) post-creation.
	ts := "2026-02-02T00:00:00Z"
	r.TSServer = &ts
	r.ThisHash = "blake3:whatever"
	r.Signature = &Signature{Alg: "ed25519", KeyID: "k1", Value: "sig"}
	proof := "anchor-proof-xyz"
	r.ServerAnchorProof = &proof
	if h3, _ := computeThisHash(r); h3 != h1 {
		t.Errorf("reserved/server fields must be excluded from the hash: %q != %q", h3, h1)
	}

	// Changing a HASHED field must change the hash.
	r.Actor = "different-actor"
	if h4, _ := computeThisHash(r); h4 == h1 {
		t.Error("a content change must change the hash")
	}
}

func TestGenesisIsDeviceBoundAndTagged(t *testing.T) {
	if GenesisPrevHash("dev-1") == GenesisPrevHash("dev-2") {
		t.Error("genesis must be device-bound")
	}
	if g := GenesisPrevHash("dev-1"); !strings.HasPrefix(g, "blake3:") {
		t.Errorf("genesis must be a tagged BLAKE3 digest: %q", g)
	}
}

// Smoke tests for the data layer. Run with: npm test  (node --test + tsx type-strip).
// These guard the mock API facade and the fixture hygiene the UI depends on.

import { test } from "node:test";
import assert from "node:assert/strict";
import { getApi } from "../api";
import * as fx from "../fixtures";
import type { AuditEventType } from "../types";

test("api facade returns fixtures", async () => {
  const api = getApi();
  const devices = await api.getDevices();
  assert.ok(devices.length > 0, "devices present");
  assert.equal((await api.getDevice("dev_01"))?.id, "dev_01");
  assert.equal(await api.getDevice("does-not-exist"), undefined);
  assert.ok((await api.getJobs()).length > 0);
  assert.ok((await api.getAuditEvents()).length > 0);
});

test("fixtures contain no forbidden product wording", () => {
  const blob = JSON.stringify(fx).toLowerCase();
  assert.ok(!blob.includes("argus"), "no 'argus' anywhere in fixtures");
  assert.ok(!blob.includes("beyz backup"), "no 'beyz backup' product wording");
});

test("license is advisory-shaped (claims present; status is a known value)", async () => {
  const lic = await getApi().getLicense();
  const known = ["valid", "expired", "not_yet_valid", "tenant_mismatch", "signature_invalid", "missing"];
  assert.ok(known.includes(lic.status));
  assert.ok(lic.seats >= lic.seatsUsed, "seat usage within seats (advisory, not enforced in UI)");
});

test("audit fixtures use the frozen DR-06 event taxonomy", async () => {
  const allowed = new Set<AuditEventType>([
    "enroll.attempt", "enroll.succeeded", "enroll.failed", "enroll.token_rejected",
    "auth.failure", "cert.renewed", "spki_pin.mismatch", "update.manifest_verified",
    "update.signature_invalid", "update.hash_mismatch", "update.staged", "update.swapped",
    "update.health_ok", "update.rolled_back", "update.downgrade_blocked", "config.reloaded",
    "config.tamper_detected", "license.signature_invalid", "service.started", "service.stopped",
  ]);
  for (const e of await getApi().getAuditEvents()) {
    assert.ok(allowed.has(e.eventType), `unknown event_type: ${e.eventType}`);
  }
});

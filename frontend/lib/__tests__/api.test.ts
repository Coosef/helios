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

test("dashboard + executive view models are served through the facade", async () => {
  const api = getApi();
  const insights = await api.getDashboardInsights();
  assert.ok(insights.resilience.score >= 0 && insights.resilience.score <= 100, "resilience score in range");
  assert.ok(insights.trend.protectedTB.length > 1, "trend series has points");
  assert.equal(insights.trend.protectedTB.length, insights.trend.resilienceScore.length, "trend series aligned");
  assert.ok(insights.activity.length > 0, "24h activity distribution present");
  assert.ok(insights.fleet.online + insights.fleet.warning + insights.fleet.offline > 0, "fleet health present");
  assert.ok(insights.securityPosture.length > 0, "security posture present");
  assert.ok(insights.topRisks.length > 0, "top risks present");

  const exec = await api.getExecutiveSummary();
  assert.ok(exec.kpis.protectedAssets > 0, "executive KPIs present");
  assert.ok(exec.financials.projectedAnnualUsd > 0, "financials present");
  assert.ok(exec.topRisks.length > 0, "executive top risks present");
});

test("PR-2 view models (restore/locations/super) are served through the facade", async () => {
  const api = getApi();

  const rc = await api.getRestoreCenter();
  assert.ok(rc.confidenceScore > 0 && rc.confidenceScore <= rc.maxScore, "restore confidence in range");
  assert.ok(rc.points.length > 1, "restore points present");
  assert.ok(rc.tree.length > 0, "file tree present");
  assert.ok(rc.readiness.some((r) => r.status === "pending"), "a readiness check is honestly marked pending");
  assert.ok(rc.activity.length > 0, "restore activity present");
  // restore points reference real device ids (internal consistency)
  const devIds = new Set((await api.getDevices()).map((d) => d.id));
  assert.ok(rc.points.every((p) => devIds.has(p.deviceId)), "restore points reference real device ids");

  const lo = await api.getLocationsOverview();
  assert.equal(lo.kpis.siteCount, lo.sites.length, "site KPI matches site count");
  assert.equal(lo.kpis.deviceCount, lo.sites.reduce((s, x) => s + x.deviceCount, 0), "device KPI is the site sum");
  assert.ok(lo.groups.length > 0, "region groups present");
  assert.ok(lo.sites.some((s) => s.linuxPrepOnly > 0), "a site notes a Linux prep-only device");
  // getLocations() (lean) and getLocationsOverview() (rich) stay coherent on shared ids
  const leanIds = new Set((await api.getLocations()).map((l) => l.id));
  assert.ok(lo.sites.every((s) => leanIds.has(s.id)), "overview site ids match lean locations");

  const so = await api.getSuperOverview();
  assert.equal(so.kpis.tenants, so.tenants.length, "tenant KPI matches rollup count");
  assert.equal(so.kpis.managedDevices, so.tenants.reduce((s, t) => s + t.devices, 0), "device KPI is the tenant sum");
  assert.ok(so.regions.length > 0 && so.crossTenantAlerts.length > 0, "regions + cross-tenant alerts present");
  // super rollups reuse the real tenant ids — no invented tenants
  const tenantIds = new Set((await api.getTenants()).map((t) => t.id));
  assert.ok(so.tenants.every((t) => tenantIds.has(t.id)), "super tenant rollups reuse real tenant ids");
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

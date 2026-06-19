// Route smoke + hygiene tests. Static (no browser): verify the key route modules
// exist and export a default component, and that NO source file imports runtime code
// from the original design package (../Backup). The authoritative "does it render"
// gate is `npm run build` (prerenders all routes); this guards structure + the
// Backup-isolation rule cheaply on every `npm test`.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const FE = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

function read(rel: string): string {
  return readFileSync(join(FE, rel), "utf8");
}

const KEY_ROUTES = [
  "app/page.tsx", // homepage (redirects to /login)
  "app/login/page.tsx", // login
  "app/(app)/layout.tsx", // shell
  "app/(app)/dashboard/page.tsx", // dashboard
  "app/(app)/executive/page.tsx", // executive overview
  "app/(app)/restore/page.tsx", // restore center (PR-2)
  "app/(app)/locations/page.tsx", // locations (PR-2)
  "app/(app)/super/page.tsx", // super-admin overview (PR-2)
  "app/(app)/devices/page.tsx", // devices list (PR-3)
  "app/(app)/devices/[id]/page.tsx", // device details (PR-3)
  "app/(app)/storage/page.tsx", // storage (PR-3)
  "app/(app)/jobs/page.tsx", // backup jobs (Batch-B PR-1)
  "app/(app)/settings/page.tsx", // settings (Batch-B PR-1)
  "app/(app)/audit/page.tsx", // audit logs (Batch-B PR-1)
];

test("key route modules exist and export a default", () => {
  for (const r of KEY_ROUTES) {
    const src = read(r);
    assert.ok(src.length > 0, `${r} is non-empty`);
    assert.match(src, /export default/, `${r} exports a default`);
  }
});

test("homepage redirects to /login", () => {
  assert.match(read("app/page.tsx"), /redirect\(\s*["']\/login["']\s*\)/);
});

// Walk app/ + components/ + lib/ and assert no source IMPORTS from ../Backup.
function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name === ".next") continue;
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (/\.(ts|tsx|css)$/.test(name)) out.push(p);
  }
  return out;
}

test("no source imports runtime code from the Backup/ design package", () => {
  const importRe = /\b(?:import|require)\b[^\n;]*['"][^'"]*Backup[^'"]*['"]/;
  for (const root of ["app", "components", "lib"]) {
    for (const file of walk(join(FE, root))) {
      const src = readFileSync(file, "utf8");
      assert.ok(!importRe.test(src), `${file} must not import from Backup/`);
    }
  }
});

test("error boundaries exist (no silent blank page on a client crash)", () => {
  assert.match(read("app/error.tsx"), /export default/);
  assert.match(read("app/global-error.tsx"), /export default/);
});

test("shell wraps content in a full-width .content > .page layout (no narrow-column collapse)", () => {
  const shell = read("components/AppShell.tsx");
  // .content (flex/scroll) and .page must be SEPARATE elements — combining them put
  // margin:auto on the flex child and collapsed it to content width.
  assert.match(shell, /className="content"/, "AppShell has a .content scroll container");
  assert.match(shell, /className="page page-wide"/, "AppShell wraps children in a full-width .page");
  assert.doesNotMatch(shell, /className="content page"/, "must NOT combine .content and .page on one element");
});

test("dashboard + executive use the responsive grid layout helpers", () => {
  assert.match(read("app/(app)/dashboard/page.tsx"), /stat-grid/);
  assert.match(read("app/(app)/executive/page.tsx"), /stat-grid|cols-2/);
});

test("chart primitives module exists and exports the Batch-A primitives", () => {
  const src = read("components/charts.tsx");
  assert.match(src, /^"use client";/, "charts.tsx is a client module");
  for (const name of ["AreaChart", "Donut", "Gauge", "CapacityBar"]) {
    assert.match(src, new RegExp(`export function ${name}\\b`), `charts.tsx exports ${name}`);
  }
  // Gradient IDs must be stable (useId), never Math.random — that would break hydration.
  assert.match(src, /useId\(\)/, "charts use useId() for stable SVG ids");
  assert.doesNotMatch(src, /Math\.random\s*\(/, "charts must not call Math.random for ids");
});

test("dashboard + executive render the ported chart primitives", () => {
  for (const page of ["app/(app)/dashboard/page.tsx", "app/(app)/executive/page.tsx"]) {
    const src = read(page);
    assert.match(src, /from "@\/components\/charts"/, `${page} imports chart primitives`);
    assert.match(src, /<(Gauge|Donut|AreaChart)\b/, `${page} renders a chart primitive`);
  }
});

test("pages read data only through getApi(), never raw fixtures", () => {
  const pages = [
    "app/(app)/dashboard/page.tsx", "app/(app)/executive/page.tsx",
    "app/(app)/restore/page.tsx", "app/(app)/locations/page.tsx", "app/(app)/super/page.tsx",
    "app/(app)/devices/page.tsx", "app/(app)/devices/[id]/page.tsx", "app/(app)/storage/page.tsx",
    "app/(app)/jobs/page.tsx", "app/(app)/settings/page.tsx", "app/(app)/audit/page.tsx",
  ];
  for (const page of pages) {
    const src = read(page);
    assert.match(src, /getApi\(\)/, `${page} uses the getApi() facade`);
    assert.doesNotMatch(src, /from ["'][^"']*lib\/fixtures/, `${page} must not import lib/fixtures directly`);
  }
});

test("PR-2 pages (restore/locations/super) have richer compositions + honest preview labels", () => {
  const restore = read("app/(app)/restore/page.tsx");
  // backend-pending banner must remain; timeline + file tree + readiness gauge + activity table
  assert.match(restore, /Banner kind="pending"/, "restore keeps the backend-pending banner");
  assert.match(restore, /from "@\/components\/charts"/, "restore uses a shared chart primitive");
  assert.match(restore, /className="tl"/, "restore renders a recovery timeline");
  assert.match(restore, /tree-row/, "restore renders a file-tree browser");
  assert.match(restore, /<DataTable\b/, "restore renders the recent-activity table");

  const locations = read("app/(app)/locations/page.tsx");
  assert.match(locations, /stat-grid/, "locations has a KPI grid");
  assert.match(locations, /grid-auto/, "locations renders per-site cards");
  assert.match(locations, /<Meter\b/, "locations renders health meters");
  assert.match(locations, /prep-only/, "locations keeps the Linux prep-only note");

  const sup = read("app/(app)/super/page.tsx");
  assert.match(sup, /Banner kind="preview"/, "super keeps the future-preview banner");
  assert.match(sup, /stat-grid/, "super has a cross-tenant KPI grid");
  assert.match(sup, /<DataTable\b/, "super renders the tenant fleet table");
  assert.match(sup, /from "@\/components\/charts"/, "super uses a shared chart primitive");
});

test("PR-3 pages (devices/device-details/storage) have richer compositions", () => {
  const devices = read("app/(app)/devices/page.tsx");
  assert.match(devices, /stat-grid/, "devices has a summary KPI row");
  assert.match(devices, /\bchip\b/, "devices has the mock filter chip bar");
  assert.match(devices, /design preview/, "devices labels its mock controls as design preview");
  assert.match(devices, /<Meter\b/, "devices table shows health meters");
  assert.match(devices, /deviceHealth/, "devices derives health via the shared helper");

  const details = read("app/(app)/devices/[id]/page.tsx");
  assert.match(details, /notFound\(\)/, "device details handles unknown ids with notFound()");
  assert.match(details, /stat-grid/, "device details has a scoreboard");
  assert.match(details, /Audit chain/, "device details keeps the audit chain");
  assert.match(details, /prep-only/, "device details keeps the Linux prep-only note");

  const storage = read("app/(app)/storage/page.tsx");
  assert.match(storage, /Banner kind="pending"/, "storage keeps the backend-pending banner");
  assert.match(storage, /stat-grid/, "storage has a KPI row");
  assert.match(storage, /<CapacityBar\b/, "storage renders capacity bars");
  assert.match(storage, /<DonutBreakdown\b/, "storage renders the tier donut");
});

test("invalid device route guards with notFound() BEFORE using the device (no crash)", () => {
  const src = read("app/(app)/devices/[id]/page.tsx");
  // The notFound() guard must appear before the first use of `device.` so an unknown id
  // 404s instead of dereferencing undefined.
  const guard = src.indexOf("notFound()");
  const firstUse = src.search(/device\.\w/);
  assert.ok(guard !== -1, "device details calls notFound()");
  assert.ok(guard < firstUse, "notFound() guard precedes any device.<field> access");
});

test("Batch-B pages (jobs/settings/audit) have richer compositions + honest mock labels", () => {
  const jobs = read("app/(app)/jobs/page.tsx");
  assert.match(jobs, /Banner kind="pending"/, "jobs keeps the backend-pending banner");
  assert.match(jobs, /stat-grid/, "jobs has a KPI row");
  assert.match(jobs, /<DonutBreakdown\b/, "jobs renders the pipeline donut");
  assert.match(jobs, /<AreaChart\b/, "jobs renders the 14-day trend");
  assert.match(jobs, /getJobsOverview/, "jobs reads the bundled overview via the facade");

  const audit = read("app/(app)/audit/page.tsx");
  assert.match(audit, /Banner kind="preview"/, "audit keeps the preview banner");
  assert.match(audit, /stat-grid/, "audit has a KPI row");
  assert.match(audit, /Integrity chain/, "audit renders the integrity panel");
  assert.match(audit, /className="tl"/, "audit renders the event timeline");
  assert.match(audit, /design preview/, "audit labels its mock filter controls");

  const settings = read("app/(app)/settings/page.tsx");
  assert.match(settings, /SettingsTabs/, "settings uses the tabbed structure");
  assert.match(settings, /Management API integration lands in Sprint 2/, "settings keeps the integrations banner verbatim");
  assert.match(settings, /getSettingsOverview/, "settings reads via the facade");
});

test("settings-tabs client component holds UI state only and is hydration-safe", () => {
  const tabs = read("components/settings-tabs.tsx");
  assert.match(tabs, /^"use client";/, "settings-tabs is a client component");
  // UI-state only — must not fetch, import fixtures, or use nondeterministic render inputs.
  assert.doesNotMatch(tabs, /Math\.random\s*\(|Date\.now\s*\(|new Date\(/, "no nondeterministic render inputs (hydration-safe)");
  assert.doesNotMatch(tabs, /lib\/fixtures|getApi|fetch\s*\(/, "client tabs hold no data — UI state only");
});

test("no source performs real network calls (mock-only shell)", () => {
  const netRe = /\b(?:fetch\s*\(|axios|XMLHttpRequest|new\s+WebSocket)\b/;
  for (const root of ["app", "components", "lib"]) {
    for (const file of walk(join(FE, root))) {
      if (/__tests__/.test(file)) continue;
      const src = readFileSync(file, "utf8");
      assert.ok(!netRe.test(src), `${file} must not make real network calls`);
    }
  }
});

test("no source mentions forbidden product wording (Argus / Beyz Backup)", () => {
  for (const root of ["app", "components", "lib"]) {
    for (const file of walk(join(FE, root))) {
      if (/__tests__/.test(file)) continue; // guard tests assert the strings' absence
      const src = readFileSync(file, "utf8").toLowerCase();
      assert.ok(!src.includes("argus"), `${file} must not mention 'argus'`);
      assert.ok(!src.includes("beyz backup"), `${file} must not mention 'beyz backup'`);
    }
  }
});

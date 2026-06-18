// Typed mock fixtures for UI Sprint 1. This is the ONLY source of data in the shell:
// there is no backend yet. It is deliberately separate from UI components and is read
// exclusively through the lib/api.ts facade, so a future OpenAPI-generated client can
// replace it without touching any screen. No real device data, no secrets, and no leftover
// prototype credential placeholders.

import type {
  ActivitySlice, AgentVersion, Alert, AuditEvent, DashboardInsights, DashboardSummary,
  Device, ExecutiveSummary, Financials, FleetHealth, Job, License, LocationSite,
  LocationsOverview, RegionGroup, Resilience, RestoreCenter, SecurityPostureItem,
  SiteRollup, StorageOverview, StorageTarget, SuperOverview, Tenant, TopRisk, Trend, User,
} from "./types";

export const tenants: Tenant[] = [
  { id: "tnt_meridian", name: "Meridian Hotels", plan: "Enterprise", color: "#2563EB" },
  { id: "tnt_aegis", name: "Aegis Manufacturing", plan: "Business", color: "#14B8A6" },
  { id: "tnt_lindqvist", name: "Lindqvist Legal", plan: "Business", color: "#a78bff" },
];

export const locations: LocationSite[] = [
  { id: "loc_ist", tenantId: "tnt_meridian", name: "Istanbul HQ", deviceCount: 18, health: 97 },
  { id: "loc_belek", tenantId: "tnt_meridian", name: "Belek Resort", deviceCount: 12, health: 91 },
  { id: "loc_ams", tenantId: "tnt_aegis", name: "Amsterdam Plant", deviceCount: 14, health: 88 },
  { id: "loc_sto", tenantId: "tnt_lindqvist", name: "Stockholm Office", deviceCount: 4, health: 99 },
];

export const devices: Device[] = [
  { id: "dev_01", host: "ist-dc-01", os: "windows", role: "Domain Controller", siteId: "loc_ist", enrollment: "enrolled", presence: "online", agentVersion: "0.1.0", updateStatus: "up_to_date", lastSeen: "2026-06-16T09:41:00Z", fingerprint: "sha256:9f2a…b41c" },
  { id: "dev_02", host: "ist-sql-02", os: "windows", role: "SQL Server", siteId: "loc_ist", enrollment: "enrolled", presence: "online", agentVersion: "0.1.0", updateStatus: "update_available", lastSeen: "2026-06-16T09:40:12Z", fingerprint: "sha256:3c7d…02ab" },
  { id: "dev_03", host: "belek-fs-01", os: "windows", role: "File Server", siteId: "loc_belek", enrollment: "enrolled", presence: "stale", agentVersion: "0.1.0", updateStatus: "up_to_date", lastSeen: "2026-06-16T07:58:33Z", fingerprint: "sha256:aa18…7f90" },
  { id: "dev_04", host: "ams-app-07", os: "linux", role: "App Server (prep-only)", siteId: "loc_ams", enrollment: "unenrolled", presence: "offline", agentVersion: "0.1.0", updateStatus: "up_to_date", lastSeen: "2026-06-15T22:10:00Z", fingerprint: "sha256:5b21…d4e6" },
  { id: "dev_05", host: "ams-vm-12", os: "windows", role: "Hypervisor", siteId: "loc_ams", enrollment: "degraded", presence: "online", agentVersion: "0.1.0", updateStatus: "rolled_back", lastSeen: "2026-06-16T09:31:04Z", fingerprint: "sha256:7c1d…aef0" },
  { id: "dev_06", host: "sto-law-03", os: "windows", role: "Workstation", siteId: "loc_sto", enrollment: "enrolled", presence: "online", agentVersion: "0.1.0", updateStatus: "up_to_date", lastSeen: "2026-06-16T09:42:18Z", fingerprint: "sha256:1e4f…99c2" },
];

export const jobs: Job[] = [
  { id: "job_1001", deviceId: "dev_01", deviceHost: "ist-dc-01", type: "Full image", status: "success", startedAt: "2026-06-16T03:00:00Z", durationSec: 2240, sizeBytes: 84_000_000_000 },
  { id: "job_1002", deviceId: "dev_02", deviceHost: "ist-sql-02", type: "Database dump", status: "running", startedAt: "2026-06-16T09:30:00Z", durationSec: 640, sizeBytes: 12_000_000_000 },
  { id: "job_1003", deviceId: "dev_03", deviceHost: "belek-fs-01", type: "Incremental", status: "failed", startedAt: "2026-06-16T02:15:00Z", durationSec: 95, sizeBytes: 0 },
  { id: "job_1004", deviceId: "dev_05", deviceHost: "ams-vm-12", type: "VM snapshot", status: "queued", startedAt: "2026-06-16T10:00:00Z", durationSec: 0, sizeBytes: 0 },
  { id: "job_1005", deviceId: "dev_06", deviceHost: "sto-law-03", type: "File-level", status: "success", startedAt: "2026-06-16T01:30:00Z", durationSec: 410, sizeBytes: 5_400_000_000 },
];

export const storageTargets: StorageTarget[] = [
  { id: "st_helios_eu", name: "Helios Cloud · eu-central", kind: "helios_cloud", usedBytes: 4_100_000_000_000, capacityBytes: 10_000_000_000_000, status: "healthy", region: "eu-central-1", encryption: "AES-256-GCM · Helios KMS", immutable: true, protocol: "HTTPS", throughput: "1.2 GB/s" },
  { id: "st_ist_qnap", name: "Istanbul QNAP", kind: "smb", usedBytes: 2_800_000_000_000, capacityBytes: 4_000_000_000_000, status: "healthy", region: "Istanbul, TR", encryption: "AES-256-GCM", immutable: false, protocol: "SMB 3.1.1", throughput: "920 MB/s" },
  { id: "st_belek_vault", name: "Belek Vault", kind: "sftp", usedBytes: 980_000_000_000, capacityBytes: 1_000_000_000_000, status: "warning", region: "Antalya, TR", encryption: "AES-256-GCM", immutable: true, protocol: "SFTP", throughput: "180 MB/s" },
  { id: "st_ams_minio", name: "Amsterdam MinIO", kind: "minio", usedBytes: 1_200_000_000_000, capacityBytes: 6_000_000_000_000, status: "healthy", region: "Amsterdam, NL", encryption: "AES-256-GCM", immutable: false, protocol: "S3", throughput: "640 MB/s" },
];

export const alerts: Alert[] = [
  { id: "al_1", severity: "critical", title: "Backup failed", detail: "belek-fs-01: incremental job aborted (storage warning).", deviceId: "dev_03", at: "2026-06-16T02:16:00Z", acknowledged: false },
  { id: "al_2", severity: "warning", title: "Agent degraded", detail: "ams-vm-12: update rolled back after failed health gate.", deviceId: "dev_05", at: "2026-06-16T09:32:00Z", acknowledged: false },
  { id: "al_3", severity: "warning", title: "Heartbeat stale", detail: "belek-fs-01: no heartbeat for 1h 43m.", deviceId: "dev_03", at: "2026-06-16T09:40:00Z", acknowledged: false },
  { id: "al_4", severity: "info", title: "Update available", detail: "ist-sql-02: agent 0.1.0 → newer build offered.", deviceId: "dev_02", at: "2026-06-16T08:00:00Z", acknowledged: true },
];

export const auditEvents: AuditEvent[] = [
  { id: "au_1", seq: 42, eventType: "enroll.succeeded", outcome: "success", actor: "installer", deviceId: "dev_06", tenantId: "tnt_lindqvist", tsLocal: "2026-06-16T09:42:18Z" },
  { id: "au_2", seq: 41, eventType: "update.rolled_back", outcome: "failure", actor: "agent", deviceId: "dev_05", tenantId: "tnt_aegis", tsLocal: "2026-06-16T09:31:04Z" },
  { id: "au_3", seq: 40, eventType: "update.health_ok", outcome: "success", actor: "agent", deviceId: "dev_01", tenantId: "tnt_meridian", tsLocal: "2026-06-16T08:57:51Z" },
  { id: "au_4", seq: 39, eventType: "spki_pin.mismatch", outcome: "denied", actor: "agent", deviceId: "dev_03", tenantId: "tnt_meridian", tsLocal: "2026-06-16T08:44:22Z" },
  { id: "au_5", seq: 38, eventType: "enroll.token_rejected", outcome: "denied", actor: "installer", deviceId: "dev_04", tenantId: "tnt_aegis", tsLocal: "2026-06-16T08:12:09Z" },
  { id: "au_6", seq: 37, eventType: "update.downgrade_blocked", outcome: "denied", actor: "agent", deviceId: "dev_02", tenantId: "tnt_meridian", tsLocal: "2026-06-16T07:41:17Z" },
];

export const users: User[] = [
  { id: "u_1", name: "Sofia Delacroix", email: "s.delacroix@meridian.example", role: "Owner", lastActive: "2026-06-16T09:38:00Z" },
  { id: "u_2", name: "Jonas Weyland", email: "j.weyland@meridian.example", role: "Admin", lastActive: "2026-06-16T09:10:00Z" },
  { id: "u_3", name: "Mara Okonkwo", email: "m.okonkwo@aegis.example", role: "Operator", lastActive: "2026-06-16T08:50:00Z" },
  { id: "u_4", name: "Anders Lindqvist", email: "a.lindqvist@lindqvist.example", role: "Viewer", lastActive: "2026-06-15T17:22:00Z" },
];

export const agentVersions: AgentVersion[] = [
  { version: "0.1.0", channel: "stable", releasedAt: "2026-06-10", devices: 46, rolloutPct: 100 },
  { version: "0.1.1-rc1", channel: "beta", releasedAt: "2026-06-15", devices: 2, rolloutPct: 5 },
];

export const license: License = {
  licenseId: "lic_meridian_2026",
  tenantId: "tnt_meridian",
  plan: "Enterprise",
  seats: 50,
  seatsUsed: 46,
  quotaBytes: 10_000_000_000_000,
  quotaUsedBytes: 6_980_000_000_000,
  notAfter: "2027-01-31T00:00:00Z",
  status: "valid",
};

export const dashboard: DashboardSummary = {
  devicesTotal: 48,
  devicesOnline: 41,
  devicesDegraded: 2,
  jobsSucceeded24h: 312,
  jobsFailed24h: 4,
  protectedBytes: 6_980_000_000_000,
  openAlerts: 3,
};

// ---- Batch-A view-model fixtures (illustrative mock data, mock-only) -----------------
// Colors reference theme tokens (var(--ok) etc.) so charts re-theme in dark/light mode.

export const resilience: Resilience = {
  score: 87,
  grade: "A−",
  delta: 3,
  pillars: [
    { label: "Backup success", score: 96, color: "var(--ok)" },
    { label: "Restore readiness", score: 88, color: "var(--accent)" },
    { label: "Coverage", score: 82, color: "var(--info)" },
    { label: "Storage headroom", score: 74, color: "var(--warn)" },
  ],
};

// 14-day trend. protectedTB in TB, resilienceScore on 0–100.
export const trend: Trend = {
  labels: ["Jun 3", "Jun 7", "Jun 11", "Jun 15"],
  protectedTB: [5.9, 6.0, 6.1, 6.2, 6.25, 6.3, 6.4, 6.5, 6.55, 6.6, 6.7, 6.8, 6.9, 6.98],
  resilienceScore: [80, 81, 81, 83, 82, 84, 85, 84, 85, 86, 86, 87, 86, 87],
};

// 24h job-activity distribution (counts by outcome) for the dashboard donut.
export const activity24h: ActivitySlice[] = [
  { label: "Succeeded", value: 312, color: "var(--ok)" },
  { label: "Running", value: 9, color: "var(--info)" },
  { label: "Queued", value: 11, color: "var(--text-2)" },
  { label: "Failed", value: 4, color: "var(--crit)" },
];

export const fleetHealth: FleetHealth = {
  online: 41,
  warning: 5,
  offline: 2,
};

export const securityPosture: SecurityPostureItem[] = [
  { label: "Agent identity (mTLS / SPKI pinning)", ok: true, detail: "All enrolled agents present a pinned client certificate." },
  { label: "Update signature enforcement", ok: true, detail: "Signed manifests verified fail-closed before swap." },
  { label: "Audit chain integrity", ok: true, detail: "BLAKE3 hash chain continuous across the fleet." },
  { label: "Storage encryption at rest", ok: true, detail: "AES-256-GCM on all attached targets." },
  { label: "Offsite copy (3-2-1)", ok: false, detail: "Belek Vault second copy lagging — backend-pending." },
];

export const topRisks: TopRisk[] = [
  { id: "risk_1", severity: "critical", title: "belek-fs-01 backup failing", impact: "File-server recovery point 30h+ stale", owner: "Meridian Ops" },
  { id: "risk_2", severity: "warning", title: "Belek Vault near capacity", impact: "98% used — new recovery points at risk", owner: "Meridian Ops" },
  { id: "risk_3", severity: "warning", title: "ams-vm-12 agent degraded", impact: "Update rolled back; hypervisor unprotected", owner: "Aegis IT" },
  { id: "risk_4", severity: "info", title: "ams-app-07 not enrolled", impact: "Linux app server outside protection scope", owner: "Aegis IT" },
];

const executiveKpis: ExecutiveSummary["kpis"] = {
  protectedAssets: 48,
  protectedTB: 6.98,
  successRate: 98.7,
  complianceScore: 92,
  restoreReadiness: 88,
  storageRunwayDays: 134,
};

const financials: Financials = {
  savedByDedupUsd: 184_000,
  projectedAnnualUsd: 612_000,
  dataAtRiskAvoidedUsd: 2_400_000,
};

export const dashboardInsights: DashboardInsights = {
  resilience,
  trend,
  activity: activity24h,
  fleet: fleetHealth,
  securityPosture,
  topRisks,
};

export const executiveSummary: ExecutiveSummary = {
  resilience,
  trend,
  kpis: executiveKpis,
  financials,
  topRisks,
};

// ---- PR-2 view-model fixtures (restore / locations / super) — illustrative, mock-only.
// Reuses real device/tenant/site ids so the mock stays internally consistent. Wording
// follows the existing fail-closed / BLAKE3 / WORM verification vocabulary so nothing
// implies a capability the Sprint-1 backend lacks.

export const restoreCenter: RestoreCenter = {
  confidenceScore: 94,
  maxScore: 100,
  sourceDeviceId: "dev_01",
  sourceHost: "ist-dc-01",
  points: [
    { id: "rp_1", deviceId: "dev_01", deviceHost: "ist-dc-01", kind: "Incremental", at: "2026-06-16T03:00:00Z", sizeBytes: 8_400_000_000, verified: true, chainOk: true },
    { id: "rp_2", deviceId: "dev_01", deviceHost: "ist-dc-01", kind: "Incremental", at: "2026-06-15T03:00:00Z", sizeBytes: 11_200_000_000, verified: true, chainOk: true },
    { id: "rp_3", deviceId: "dev_01", deviceHost: "ist-dc-01", kind: "Incremental", at: "2026-06-14T03:00:00Z", sizeBytes: 9_900_000_000, verified: true, chainOk: true },
    { id: "rp_4", deviceId: "dev_01", deviceHost: "ist-dc-01", kind: "Full image", at: "2026-06-09T03:00:00Z", sizeBytes: 412_000_000_000, verified: false, chainOk: true },
  ],
  tree: [
    {
      name: "C:\\", kind: "dir", children: [
        {
          name: "shares", kind: "dir", children: [
            {
              name: "finance", kind: "dir", children: [
                { name: "Q2-2026-forecast.xlsx", kind: "file", ext: "xlsx", sizeBytes: 2_400_000, modAt: "2026-06-07T18:22:00Z", selected: true },
                { name: "invoices.db", kind: "file", ext: "db", sizeBytes: 64_000_000, modAt: "2026-06-16T02:14:00Z" },
                { name: "audit-2025.pdf", kind: "file", ext: "pdf", sizeBytes: 8_800_000, modAt: "2026-01-12T09:03:00Z" },
              ],
            },
            { name: "contracts", kind: "dir", children: [] },
          ],
        },
        {
          name: "AD", kind: "dir", children: [
            { name: "ntds.dit", kind: "file", ext: "dit", sizeBytes: 188_000_000, modAt: "2026-06-16T03:00:00Z" },
          ],
        },
      ],
    },
  ],
  readiness: [
    { label: "Recovery validation", status: "pass", detail: "Dry-run restore passed — target writable, decryption OK." },
    { label: "Integrity verification", status: "pass", detail: "BLAKE3 manifest re-verified against the chain." },
    { label: "Last verified restore", status: "pass", detail: "Jun 2 · completed in 4m 12s." },
    { label: "RTO validation", status: "pending", detail: "Within 5m target — automated check is backend-pending." },
    { label: "Immutability", status: "pass", detail: "WORM-locked offsite copy present." },
  ],
  activity: [
    { id: "ra_1", item: "finance\\Q2-2026-forecast.xlsx", type: "File", destination: "ist-dc-01 · original location", by: "Sofia Delacroix", status: "success", progressPct: null, when: "12 min ago" },
    { id: "ra_2", item: "meridian_prod", type: "Database", destination: "staging area", by: "Jonas Weyland", status: "running", progressPct: 64, when: "in progress" },
    { id: "ra_3", item: "belek-fs-01 (full image)", type: "Bare-metal", destination: "dissimilar hardware", by: "Mara Okonkwo", status: "queued", progressPct: null, when: "queued" },
    { id: "ra_4", item: "sto-law-03\\Desktop", type: "Folder", destination: "download archive (.zip)", by: "Anders Lindqvist", status: "success", progressPct: null, when: "yesterday" },
  ],
};

// Super-admin-plane rollups. Same 3 tenant ids/names/colors as `tenants` — no new tenants
// invented. mrr/health are illustrative platform metrics (EUR), clearly mock.
const tenantRollups = [
  { id: "tnt_meridian", name: "Meridian Hotels", plan: "Enterprise", color: "#2563EB", devices: 30, online: 28, offline: 2, health: 96, mrr: 2100, status: "active" as const },
  { id: "tnt_aegis", name: "Aegis Manufacturing", plan: "Business", color: "#14B8A6", devices: 14, online: 12, offline: 2, health: 88, mrr: 1120, status: "active" as const },
  { id: "tnt_lindqvist", name: "Lindqvist Legal", plan: "Business", color: "#a78bff", devices: 4, online: 4, offline: 0, health: 99, mrr: 320, status: "active" as const },
];

export const superOverview: SuperOverview = {
  kpis: {
    tenants: tenantRollups.length,
    managedDevices: tenantRollups.reduce((s, t) => s + t.devices, 0),
    mrr: tenantRollups.reduce((s, t) => s + t.mrr, 0),
    arr: tenantRollups.reduce((s, t) => s + t.mrr, 0) * 12,
    slaPct: 99.98,
    openCriticalAlerts: 1,
  },
  deviceTrend: [40, 41, 41, 42, 43, 43, 44, 45, 45, 46, 46, 47, 47, 48],
  trendLabels: ["May 20", "May 27", "Jun 3", "Jun 10"],
  tenants: tenantRollups,
  regions: [
    { name: "EU-West · Frankfurt", role: "Primary", uptimePct: 99.99, nodes: 12, usedTB: 142, capacityTB: 280, tint: "var(--accent)" },
    { name: "EU-Central · Amsterdam", role: "Replica", uptimePct: 99.98, nodes: 8, usedTB: 98, capacityTB: 220, tint: "var(--info)" },
    { name: "EU-North · Stockholm", role: "Replica", uptimePct: 99.95, nodes: 5, usedTB: 44, capacityTB: 120, tint: "var(--ok)" },
  ],
  crossTenantAlerts: [
    { id: "xa_1", severity: "critical", title: "Storage node ams-03 at 88% capacity", source: "Infrastructure", category: "Capacity", at: "8 min ago" },
    { id: "xa_2", severity: "warning", title: "Aegis agent fleet: 3 devices on EOL build", source: "Aegis Manufacturing", category: "Updates", at: "1h ago" },
    { id: "xa_3", severity: "warning", title: "Belek Vault second copy lagging", source: "Meridian Hotels", category: "Replication", at: "2h ago" },
    { id: "xa_4", severity: "info", title: "Lindqvist Legal onboarding 2 new sites", source: "Lindqvist Legal", category: "Provisioning", at: "today" },
  ],
};

// Site rollups reuse the 4 existing location ids/health from `locations`, augmented with
// city/breakdown/storage fields the cards need. linuxPrepOnly reflects the existing Linux
// 'prep-only' device (dev_04 at the Amsterdam plant).
const siteRollups: SiteRollup[] = [
  { id: "loc_ist", tenantId: "tnt_meridian", tenantName: "Meridian Hotels", tenantColor: "#2563EB", name: "Istanbul HQ", city: "Istanbul", deviceCount: 18, online: 17, offline: 0, warning: 1, linuxPrepOnly: 0, health: 97, protectedBytes: 2_800_000_000_000, storageStatus: "healthy", storageName: "Istanbul QNAP" },
  { id: "loc_belek", tenantId: "tnt_meridian", tenantName: "Meridian Hotels", tenantColor: "#2563EB", name: "Belek Resort", city: "Antalya", deviceCount: 12, online: 10, offline: 1, warning: 1, linuxPrepOnly: 0, health: 91, protectedBytes: 980_000_000_000, storageStatus: "warning", storageName: "Belek Vault" },
  { id: "loc_ams", tenantId: "tnt_aegis", tenantName: "Aegis Manufacturing", tenantColor: "#14B8A6", name: "Amsterdam Plant", city: "Amsterdam", deviceCount: 14, online: 12, offline: 1, warning: 1, linuxPrepOnly: 1, health: 88, protectedBytes: 1_200_000_000_000, storageStatus: "healthy", storageName: "Amsterdam MinIO" },
  { id: "loc_sto", tenantId: "tnt_lindqvist", tenantName: "Lindqvist Legal", tenantColor: "#a78bff", name: "Stockholm Office", city: "Stockholm", deviceCount: 4, online: 4, offline: 0, warning: 0, linuxPrepOnly: 0, health: 99, protectedBytes: 0, storageStatus: "healthy", storageName: "Helios Cloud · eu-central" },
];

const siteGroups: RegionGroup[] = Object.values(
  siteRollups.reduce<Record<string, RegionGroup>>((acc, s) => {
    (acc[s.city] ??= { city: s.city, sites: [], deviceCount: 0, avgHealth: 0 }).sites.push(s);
    return acc;
  }, {}),
).map((g) => ({
  ...g,
  deviceCount: g.sites.reduce((s, x) => s + x.deviceCount, 0),
  avgHealth: Math.round(g.sites.reduce((s, x) => s + x.health, 0) / g.sites.length),
}));

export const locationsOverview: LocationsOverview = {
  sites: siteRollups,
  groups: siteGroups,
  kpis: {
    siteCount: siteRollups.length,
    deviceCount: siteRollups.reduce((s, x) => s + x.deviceCount, 0),
    cityCount: siteGroups.length,
    avgHealth: Math.round(siteRollups.reduce((s, x) => s + x.health, 0) / siteRollups.length),
  },
};

// /storage bundled view model. Used/capacity are SUMMED from the existing storageTargets
// so the KPI strip never contradicts the per-target cards. Coverage/tier splits and the
// reduction ratio / runway are illustrative mock figures.
const _stUsed = storageTargets.reduce((s, t) => s + t.usedBytes, 0);
const _stCap = storageTargets.reduce((s, t) => s + t.capacityBytes, 0);

export const storageOverview: StorageOverview = {
  kpis: {
    usedBytes: _stUsed,
    capacityBytes: _stCap,
    usagePct: Math.round((_stUsed / _stCap) * 100),
    immutablePct: 78,
    reductionRatio: 2.75,
    runwayDays: 134,
  },
  coverage: [
    { pct: 78, color: "var(--ok)", label: "Immutable (WORM)" },
    { pct: 18, color: "var(--info)", label: "Encrypted (mutable)" },
    { pct: 4, color: "var(--text-2)", label: "Standard" },
  ],
  tiers: [
    { label: "Hot", value: 49, color: "var(--accent)" },
    { label: "Warm", value: 20, color: "var(--info)" },
    { label: "Cold", value: 13, color: "var(--warn)" },
    { label: "Archive", value: 9, color: "var(--text-2)" },
  ],
  targets: storageTargets,
};

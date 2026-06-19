// Typed mock fixtures for UI Sprint 1. This is the ONLY source of data in the shell:
// there is no backend yet. It is deliberately separate from UI components and is read
// exclusively through the lib/api.ts facade, so a future OpenAPI-generated client can
// replace it without touching any screen. No real device data, no secrets, and no leftover
// prototype credential placeholders.

import type {
  ActivitySlice, AgentVersion, Alert, AlertsOverview, AugmentedAlert, AugmentedUser, AuditEvent,
  DashboardInsights, DashboardSummary, Device, ExecutiveSummary, Financials, FleetHealth, Job,
  License, LocationSite, AuditOverview, AuditTimelineItem, JobsOverview, LicensingOverview,
  LocationsOverview, RegionGroup, Resilience, RestoreCenter, Role, SecurityPostureItem,
  SettingsOverview, SiteRollup, StorageOverview, StorageTarget, SuperOverview, Tenant, TopRisk,
  Trend, UpdatesOverview, User, UsersOverview,
} from "./types";
import { ROLE_LEVEL, ROLES, capabilities } from "./rbac";

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

// ---- Batch-B PR-1 fixtures (jobs / audit / settings) — illustrative, mock-only.
// KPI totals are illustrative aggregates (the 5/6-row fixtures are too small to read as a
// product, mirroring dashboard.jobsSucceeded24h:312). Per-row tables stay backed by the
// real jobs/auditEvents fixtures, and truthful counts are derived from them.

export const jobsOverview: JobsOverview = {
  // total = sum of the pipeline distribution below, so the KPI and donut center agree.
  kpis: { total: 1221, running: 9, successRatePct: 98.7, failedToday: 14 },
  pipeline: [
    { label: "Queued", value: 11, color: "var(--text-2)" },
    { label: "Running", value: 9, color: "var(--info)" },
    { label: "Success", value: 1184, color: "var(--ok)" },
    { label: "Failed", value: 14, color: "var(--crit)" },
    { label: "Cancelled", value: 3, color: "var(--warn)" },
  ],
  trend: {
    labels: ["Jun 5", "Jun 9", "Jun 13", "Jun 17"],
    completed: [180, 176, 184, 190, 188, 192, 196, 201, 198, 205, 210, 208, 214, 219],
    failed: [6, 3, 5, 2, 4, 3, 1, 5, 2, 3, 4, 2, 3, 5],
  },
  throughput: { perDayTB: 1.2, perWeekTB: 8.7, spark: [0.9, 1.0, 1.1, 1.0, 1.2, 1.3, 1.2, 1.15, 1.25, 1.2, 1.3, 1.18, 1.22, 1.2] },
  topFailureReasons: [
    { reason: "Storage target unreachable", count: 6, pct: 43 },
    { reason: "Snapshot quiesce timeout", count: 4, pct: 29 },
    { reason: "Insufficient capacity", count: 2, pct: 14 },
    { reason: "Agent heartbeat lost", count: 2, pct: 14 },
  ],
};

// --- audit derivations over the existing auditEvents + devices fixtures ---
const AUDIT_ACTION: Record<string, string> = {
  "enroll.succeeded": "Device enrolled",
  "enroll.token_rejected": "Enrollment token rejected",
  "update.rolled_back": "Agent update rolled back",
  "update.health_ok": "Update health check passed",
  "update.downgrade_blocked": "Agent downgrade blocked",
  "spki_pin.mismatch": "SPKI pin mismatch blocked",
};
function auditCategory(eventType: string): string {
  if (eventType.startsWith("enroll")) return "Enrollment";
  if (eventType.startsWith("update")) return "Updates";
  if (eventType.startsWith("config")) return "Configuration";
  if (eventType.startsWith("license")) return "Licensing";
  if (eventType.startsWith("service")) return "Service";
  return "Security";
}

const auditTimeline: AuditTimelineItem[] = auditEvents.map((e) => ({
  id: e.id,
  seq: e.seq,
  at: e.tsLocal,
  actor: e.actor,
  deviceHost: devices.find((d) => d.id === e.deviceId)?.host,
  action: AUDIT_ACTION[e.eventType] ?? e.eventType,
  detail: `${e.eventType} · ${e.outcome}`,
  category: auditCategory(e.eventType),
  severity: e.outcome === "success" ? "ok" : e.outcome === "failure" ? "crit" : "warn",
  ip: `10.10.0.${e.seq % 254}`,
}));

export const auditOverview: AuditOverview = {
  kpis: {
    eventsToday: 247,
    critical: auditEvents.filter((e) => e.outcome === "failure" || e.outcome === "denied").length,
    integrityOkPct: 100,
    retentionYears: 7,
  },
  integrity: { algorithm: "BLAKE3", tamperEvident: true, chainIntact: true, lastVerified: "2 min ago", verifiedBlocks: 247 },
  timeline: auditTimeline,
  selectedDetail: {
    id: "au_4",
    eventHash: "b3:9f2a4c7e1d8b…a41c5e90",
    previousHash: "b3:3c7d02ab55f1…7f90a18e",
    signatureValid: true,
    chainIntact: true,
    userAgent: "HeliosConsole/0.1.0 · macOS",
    result: "Denied",
  },
};

export const settingsOverview: SettingsOverview = {
  general: { timezone: "Europe/Istanbul (TRT)", dateFormat: "YYYY-MM-DD", language: "English", organization: "Meridian Hotels" },
  security: { mfaEnforced: true, sessionTimeout: "30 min idle", passwordPolicy: "14+ chars · rotation 90d", encryptionKms: "Helios KMS · AES-256-GCM" },
  notifications: [
    { channel: "Email alerts", connected: true, detail: "Critical & warning alerts to admins." },
    { channel: "Webhook", connected: true, detail: "Custom HTTP events to your endpoint." },
    { channel: "Slack", connected: false, detail: "Post alerts to a workspace channel." },
    { channel: "Microsoft Teams", connected: false, detail: "Post alerts to a Teams channel." },
  ],
  branding: {
    logoLabel: "Helios",
    theme: "Dark",
    accentName: "Helios Blue",
    accentSwatches: [
      { name: "Helios Blue", color: "var(--accent)" },
      { name: "Teal", color: "var(--info)" },
      { name: "Violet", color: "var(--ai)" },
      { name: "Emerald", color: "var(--ok)" },
    ],
  },
  about: { product: "Helios Data Protection Platform", version: "0.1.0", build: "ui-s1-polish-b", environment: "Preview · mock fixtures", copyright: "© Beyz System A.Ş." },
};

// ---- Batch-B PR-2 fixtures (updates / licensing) — illustrative, mock-only.
// The updater chain maps 1:1 to the real DR-06 update.* audit taxonomy; the staged-rollout
// rings are a DISABLED preview (no real publishing). Licensing is strictly ADVISORY — every
// figure is parsed/audited, never enforced. No billing/monetary fields (Sprint-2).

const _fleetDevices = agentVersions.reduce((s, v) => s + v.devices, 0); // 48 (illustrative fleet)
const _stableDevices = agentVersions.filter((v) => v.channel === "stable").reduce((s, v) => s + v.devices, 0);

export const updatesOverview: UpdatesOverview = {
  kpis: {
    fleetDevices: _fleetDevices,
    onCurrentPct: Math.round((_stableDevices / _fleetDevices) * 100),
    updateAvailable: devices.filter((d) => d.updateStatus === "update_available").length,
    rolledBack: devices.filter((d) => d.updateStatus === "rolled_back").length,
    signatureFailures: 0,
  },
  chain: [
    { step: "verify", auditEvents: ["update.manifest_verified", "update.signature_invalid", "update.hash_mismatch"], tone: "ok" },
    { step: "decide", auditEvents: ["update.downgrade_blocked"], tone: "info" },
    { step: "stage", auditEvents: ["update.staged"], tone: "info" },
    { step: "swap", auditEvents: ["update.swapped"], tone: "info" },
    { step: "health gate", auditEvents: ["update.health_ok"], tone: "ok" },
    { step: "rollback", auditEvents: ["update.rolled_back"], tone: "warn" },
  ],
  rings: [
    { name: "Canary", pct: 5, devices: 1, successPct: 100, rollbacks: 0, risk: "Low", status: "done", color: "var(--ok)" },
    { name: "Early Adopters", pct: 25, devices: 2, successPct: 50, rollbacks: 1, risk: "Medium", status: "active", color: "var(--accent)" },
    { name: "Production", pct: 70, devices: 0, successPct: 100, rollbacks: 0, risk: "Low", status: "pending", color: "var(--text-2)" },
  ],
  channels: [
    { channel: "stable", versions: 1, devices: 46, color: "var(--ok)" },
    { channel: "beta", versions: 1, devices: 2, color: "var(--info)" },
    { channel: "dev", versions: 0, devices: 0, color: "var(--warn)" },
  ],
  adoption: [
    { label: "0.1.0 (stable)", value: 46, color: "var(--ok)" },
    { label: "0.1.1-rc1 (beta)", value: 2, color: "var(--info)" },
  ],
  adoptionTrend: {
    labels: ["Jun 5", "Jun 9", "Jun 13", "Jun 17"],
    series: [
      { version: "0.1.0", color: "var(--ok)", data: [40, 41, 42, 43, 44, 45, 45, 46] },
      { version: "0.1.1-rc1", color: "var(--info)", data: [0, 0, 1, 1, 1, 2, 2, 2] },
    ],
  },
  eventTimeline: [
    { id: "ue_1", at: "2026-06-16T03:00:00Z", eventType: "update.manifest_verified", tone: "ok", detail: "Ed25519 signature + JCS manifest hash verified for 0.1.1-rc1." },
    { id: "ue_2", at: "2026-06-16T03:01:00Z", eventType: "update.staged", tone: "info", detail: "New build staged beside the running version." },
    { id: "ue_3", at: "2026-06-16T03:02:00Z", eventType: "update.swapped", tone: "info", detail: "Atomic swap to the staged build." },
    { id: "ue_4", at: "2026-06-16T08:57:51Z", eventType: "update.health_ok", tone: "ok", detail: "ist-dc-01 passed the 90s post-swap health gate." },
    { id: "ue_5", at: "2026-06-16T09:31:04Z", eventType: "update.rolled_back", tone: "warn", detail: "ams-vm-12 failed the health gate — previous build restored." },
    { id: "ue_6", at: "2026-06-16T07:41:17Z", eventType: "update.downgrade_blocked", tone: "warn", detail: "ist-sql-02 downgrade attempt blocked by anti-rollback." },
  ],
  trust: [
    { label: "Ed25519 signature", detail: "Manifest signed with the Helios code-signing key.", ok: true },
    { label: "JCS-canonicalized manifest", detail: "RFC 8785 canonical JSON hashed before verify.", ok: true },
    { label: "Anti-rollback", detail: "Downgrade to an older version is blocked (update.downgrade_blocked).", ok: true },
    { label: "90s health gate", detail: "Post-swap health check; failure triggers rollback.", ok: true },
    { label: "Rollback-restore", detail: "Previous signed build restored automatically on health-gate failure.", ok: true },
  ],
  rollbacks: devices.filter((d) => d.updateStatus === "rolled_back").map((d) => ({
    deviceHost: d.host, reason: "Health gate failed after swap", at: d.lastSeen, auditEvent: "update.rolled_back" as const,
  })),
};

// Licensing — ADVISORY only. daysToExpiry is a fixed constant (notAfter 2027-01-31 vs the
// 2026-06-19 reference date ≈ 226 days) so the fixture stays deterministic.
const _daysToExpiry = 226;

export const licensingOverview: LicensingOverview = {
  kpis: {
    plan: license.plan,
    seatsUsed: license.seatsUsed,
    seats: license.seats,
    seatPct: Math.round((license.seatsUsed / license.seats) * 100),
    quotaUsedBytes: license.quotaUsedBytes,
    quotaBytes: license.quotaBytes,
    quotaPct: Math.round((license.quotaUsedBytes / license.quotaBytes) * 100),
    status: license.status,
    daysToExpiry: _daysToExpiry,
  },
  warningThresholds: [80, 90, 100],
  statusCatalog: [
    { status: "valid", active: true, advisoryAction: "Signature verified — operations continue normally." },
    { status: "expired", active: false, advisoryAction: "Surfaced to audit; backups are NOT blocked (advisory)." },
    { status: "not_yet_valid", active: false, advisoryAction: "Surfaced to audit only; never enforced." },
    { status: "tenant_mismatch", active: false, advisoryAction: "Audited as an anomaly; not enforced in Sprint 1." },
    { status: "signature_invalid", active: false, advisoryAction: "Audited (license.signature_invalid); not enforced.", auditEvent: "license.signature_invalid" },
    { status: "missing", active: false, advisoryAction: "Audited as an anomaly; operations continue (advisory)." },
  ],
  entitlements: [
    { feature: "Plan", limit: license.plan, used: "—", enforced: false },
    { feature: "Seats", limit: String(license.seats), used: String(license.seatsUsed), enforced: false },
    { feature: "Storage quota", limit: "10.0 TB", used: "7.0 TB", enforced: false },
    { feature: "Continuous backup", limit: "Included", used: "Active", enforced: false },
    { feature: "Immutable storage", limit: "Included", used: "Active", enforced: false },
  ],
  seatBreakdown: [
    { label: "Allocated", value: license.seatsUsed, color: "var(--accent)" },
    { label: "Available", value: license.seats - license.seatsUsed, color: "var(--text-2)" },
  ],
  quotaTrend: {
    labels: ["Mar", "Apr", "May", "Jun"],
    data: [52, 58, 63, 67, 68, 69, 70, 70],
  },
  auditTimeline: [
    { id: "lau_1", at: "2026-06-16T03:00:00Z", eventType: "license.signature_invalid", outcome: "failure", detail: "Illustrative: the parser flagged an invalid signature on a re-issued token — surfaced to audit, not enforced.", severity: "warn" },
  ],
  history: [
    { at: "2026-02-01", event: "License issued", detail: "Enterprise plan · 50 seats · 10 TB quota." },
    { at: "2026-04-15", event: "Seat true-up", detail: "Seat allocation reviewed — advisory, no change blocked." },
    { at: "2026-06-16", event: "Signature anomaly (illustrative)", detail: "Re-issued token signature flagged; surfaced to audit only." },
  ],
  renewalTimeline: [
    { at: "2026-02-01", label: "Issued", state: "done" },
    { at: "2026-06-19", label: "Current", state: "current" },
    { at: "2027-01-01", label: "Renewal window opens", state: "future" },
    { at: "2027-01-31", label: "Expiry (advisory)", state: "future" },
  ],
  renewal: { issuedAt: "2026-02-01T00:00:00Z", notAfter: license.notAfter, daysToExpiry: _daysToExpiry, autoRenew: true, note: "Expiry is advisory in Sprint 1 — surfaced, never enforced." },
};

// ---- Batch-B PR-3 fixtures (user management / alerts) — illustrative, mock-only.
// Built from the existing users/alerts/auditEvents/tenants/locations fixtures + rbac.ts.
// Core User/Alert types are NOT mutated; augmented rows live only in these view-models.

const ROLE_COLOR: Record<Role, string> = { Owner: "var(--ai)", Admin: "var(--ok)", Operator: "var(--info)", Viewer: "var(--text-2)" };

// Per-user mock metadata (status/tenant/location/department/mfa) keyed by the real user id.
const _userMeta: Record<string, { status: AugmentedUser["status"]; tenantId: string; locationId: string; department: string; mfa: boolean }> = {
  u_1: { status: "active", tenantId: "tnt_meridian", locationId: "loc_ist", department: "Executive", mfa: true },
  u_2: { status: "active", tenantId: "tnt_meridian", locationId: "loc_ist", department: "IT Operations", mfa: true },
  u_3: { status: "active", tenantId: "tnt_aegis", locationId: "loc_ams", department: "Site Operations", mfa: true },
  u_4: { status: "disabled", tenantId: "tnt_lindqvist", locationId: "loc_sto", department: "Legal", mfa: false },
};

const _augUsers: AugmentedUser[] = users.map((u) => {
  const m = _userMeta[u.id];
  const tenant = tenants.find((t) => t.id === m.tenantId);
  const loc = locations.find((l) => l.id === m.locationId);
  return {
    id: u.id, name: u.name, email: u.email, role: u.role, lastActive: u.lastActive,
    status: m.status, tenantId: m.tenantId, tenantName: tenant?.name ?? m.tenantId, tenantColor: tenant?.color ?? "var(--text-2)",
    locationId: m.locationId, locationName: loc?.name, department: m.department, mfa: m.mfa,
  };
});

const _activeUsers = _augUsers.filter((u) => u.status === "active").length;
const _admins = users.filter((u) => u.role === "Owner" || u.role === "Admin").length;
const _suspended = _augUsers.filter((u) => u.status === "disabled").length;

export const usersOverview: UsersOverview = {
  kpis: {
    total: users.length,
    active: _activeUsers,
    administrators: _admins,
    suspended: _suspended,
    mfaPct: Math.round((_augUsers.filter((u) => u.mfa).length / _augUsers.length) * 100),
  },
  rows: _augUsers,
  roleDistribution: ROLES.map((r) => ({ label: r, value: users.filter((u) => u.role === r).length, color: ROLE_COLOR[r] })),
  privileges: ROLES.map((r) => {
    const c = capabilities(r);
    const count = users.filter((u) => u.role === r).length;
    return { role: r, level: ROLE_LEVEL[r], count, pct: Math.round((count / users.length) * 100), color: ROLE_COLOR[r], read: c.read, write: c.write, manage: c.manage, admin: c.admin };
  }),
  // Illustrative invitation pipeline — invited/pending are in-flight (not yet accounts),
  // active/disabled reconcile with the real directory rows above.
  statusDistribution: [
    { label: "Invited", value: 2, color: "var(--info)" },
    { label: "Pending", value: 1, color: "var(--warn)" },
    { label: "Active", value: _activeUsers, color: "var(--ok)" },
    { label: "Disabled", value: _suspended, color: "var(--text-2)" },
  ],
  activity: auditEvents.map((e) => ({
    id: `ua_${e.id}`,
    actor: e.actor,
    action: e.eventType,
    detail: `${devices.find((d) => d.id === e.deviceId)?.host ?? e.tenantId} · ${e.outcome}`,
    at: e.tsLocal,
    outcome: e.outcome,
    severity: e.outcome === "success" ? "ok" : e.outcome === "failure" ? "crit" : "warn",
    auditId: e.id,
  })),
  org: {
    tenants: tenants.map((t) => ({
      id: t.id, name: t.name, color: t.color,
      users: _augUsers.filter((u) => u.tenantId === t.id).length,
      locations: locations.filter((l) => l.tenantId === t.id).length,
    })),
    departments: Object.values(
      _augUsers.reduce<Record<string, { name: string; users: number; color: string }>>((acc, u) => {
        (acc[u.department] ??= { name: u.department, users: 0, color: "var(--accent)" }).users += 1;
        return acc;
      }, {}),
    ),
    locationCount: locations.length,
  },
};

// Per-alert mock augmentation (lifecycle state + correlation/source/category) keyed by alert id.
const _alertMeta: Record<string, { state: AugmentedAlert["state"]; source: string; category: string; correlationId: string; occurrences: number }> = {
  al_1: { state: "OPEN", source: "belek-fs-01", category: "Backup", correlationId: "corr_belek_storage", occurrences: 3 },
  al_2: { state: "DEGRADED", source: "ams-vm-12", category: "Updates", correlationId: "corr_ams_update", occurrences: 1 },
  al_3: { state: "OPEN", source: "belek-fs-01", category: "Connectivity", correlationId: "corr_belek_storage", occurrences: 2 },
  al_4: { state: "CLOSED", source: "ist-sql-02", category: "Updates", correlationId: "corr_sql_update", occurrences: 1 },
};

const _augAlerts: AugmentedAlert[] = alerts.map((a) => ({ ...a, ..._alertMeta[a.id] }));
const _lifecycleCount = (s: AugmentedAlert["state"]) => _augAlerts.filter((a) => a.state === s).length;

export const alertsOverview: AlertsOverview = {
  kpis: {
    openCritical: _augAlerts.filter((a) => !a.acknowledged && a.severity === "critical").length,
    openTotal: alerts.filter((a) => !a.acknowledged).length,
    acknowledged: alerts.filter((a) => a.acknowledged).length,
    resolved: 0, // illustrative — nothing fully resolved in the current sample
    mtta: "4m 12s",
    mttr: "38m",
    mttd: "1m 06s",
  },
  rows: _augAlerts,
  severityDistribution: [
    { label: "Critical", value: alerts.filter((a) => a.severity === "critical").length, color: "var(--crit)" },
    { label: "Warning", value: alerts.filter((a) => a.severity === "warning").length, color: "var(--warn)" },
    { label: "Info", value: alerts.filter((a) => a.severity === "info").length, color: "var(--accent)" },
  ],
  lifecycleDistribution: [
    { label: "OPEN", value: _lifecycleCount("OPEN"), color: "var(--crit)" },
    { label: "DEGRADED", value: _lifecycleCount("DEGRADED"), color: "var(--warn)" },
    { label: "RECOVERING", value: _lifecycleCount("RECOVERING"), color: "var(--info)" },
    { label: "CLOSED", value: _lifecycleCount("CLOSED"), color: "var(--ok)" },
    { label: "SUPPRESSED", value: _lifecycleCount("SUPPRESSED"), color: "var(--text-2)" },
  ],
  trend: {
    labels: ["Jun 3", "Jun 7", "Jun 11", "Jun 15"],
    opened: [2, 3, 1, 4, 2, 3, 2, 1, 3, 2, 4, 1, 2, 3],
    resolved: [1, 2, 2, 3, 2, 2, 3, 1, 2, 3, 2, 2, 1, 3],
  },
  sources: [
    { source: "belek-fs-01", category: "Backup / Connectivity", count: 2, pct: 50, color: "var(--crit)" },
    { source: "ams-vm-12", category: "Updates", count: 1, pct: 25, color: "var(--warn)" },
    { source: "ist-sql-02", category: "Updates", count: 1, pct: 25, color: "var(--accent)" },
  ],
  correlationGroups: [
    { correlationId: "corr_belek_storage", title: "Belek storage saturation", rootCause: "Belek Vault at 98% capacity", memberAlertIds: ["al_1", "al_3"], members: 2, groupWaitSec: 30, bounceWindowSec: 120, state: "DEGRADED", severity: "critical" },
    { correlationId: "corr_ams_update", title: "Amsterdam hypervisor update", rootCause: "ams-vm-12 update rolled back", memberAlertIds: ["al_2"], members: 1, groupWaitSec: 30, bounceWindowSec: 90, state: "DEGRADED", severity: "warning" },
  ],
  timeline: [
    { id: "at_1", at: "2026-06-16T02:16:00Z", label: "Detected — Backup failed", detail: "correlation_id corr_belek_storage opened", severity: "crit" },
    { id: "at_2", at: "2026-06-16T02:16:30Z", label: "Grouped — group_wait elapsed", detail: "al_1 + al_3 correlated (group_wait 30s)", severity: "warn" },
    { id: "at_3", at: "2026-06-16T09:32:00Z", label: "Degraded — ams-vm-12 agent", detail: "correlation_id corr_ams_update", severity: "warn" },
    { id: "at_4", at: "2026-06-16T08:00:00Z", label: "Acknowledged — update available", detail: "ist-sql-02 · al_4 closed", severity: "ok" },
  ],
  escalation: [
    { tier: "Immediate", afterLabel: "0m", action: "Notify on-call engineer", channel: "PagerDuty", state: "done", color: "var(--crit)" },
    { tier: "After 15m", afterLabel: "15m", action: "Escalate to team lead", channel: "Slack #ops", state: "current", color: "var(--warn)" },
    { tier: "After 30m", afterLabel: "30m", action: "Page secondary on-call", channel: "Phone", state: "future", color: "var(--text-2)" },
  ],
  suppression: [
    { id: "sup_1", scope: "category:License", reason: "Planned renewal window", window: "7d", matchedAlerts: 0, active: true },
    { id: "sup_2", scope: "host:ams-app-07", reason: "Linux prep-only (not enrolled)", window: "until enrolled", matchedAlerts: 0, active: true },
  ],
};

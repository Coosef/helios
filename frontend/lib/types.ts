// Domain types for the Helios console. These mirror the REAL Sprint-1 agent/updater
// system (enrollment FSM, updater chain, audit taxonomy, advisory license) so the UI
// speaks the same vocabulary the management API will eventually expose. They are NOT a
// generated client — they are the hand-authored shape the mock fixtures satisfy and
// that a future OpenAPI-generated client must remain compatible with.

export type OS = "windows" | "linux";

/** Agent enrollment state machine (ADR-003 / internal/agent/enroll). */
export type EnrollmentState =
  | "unenrolled"
  | "enrolling"
  | "enrolled"
  | "degraded"
  | "decommissioned";

/** Control-plane presence, derived from heartbeat recency. */
export type Presence = "online" | "offline" | "stale";

/** Agent self-update posture (internal/updater chain T21–T27). */
export type UpdateStatus = "up_to_date" | "update_available" | "updating" | "rolled_back";

export interface Tenant {
  id: string;
  name: string;
  plan: string;
  color: string;
}

export interface LocationSite {
  id: string;
  tenantId: string;
  name: string;
  deviceCount: number;
  /** Operational health score 0–100 (mock aggregate). */
  health: number;
}

export interface Device {
  id: string;
  host: string;
  os: OS;
  role: string;
  siteId: string;
  enrollment: EnrollmentState;
  presence: Presence;
  agentVersion: string;
  updateStatus: UpdateStatus;
  /** ISO timestamp of the last heartbeat. */
  lastSeen: string;
  /** SPKI thumbprint fingerprint (sha256:hex), display-truncated in the UI. */
  fingerprint: string;
}

export type JobType =
  | "Full image"
  | "Incremental"
  | "File-level"
  | "VM snapshot"
  | "Database dump"
  | "Bare-metal";

export type JobStatus = "success" | "running" | "failed" | "queued";

export interface Job {
  id: string;
  deviceId: string;
  deviceHost: string;
  type: JobType;
  status: JobStatus;
  startedAt: string;
  durationSec: number;
  sizeBytes: number;
}

export type StorageKind = "smb" | "sftp" | "s3" | "minio" | "helios_cloud" | "synology";

export interface StorageTarget {
  id: string;
  name: string;
  kind: StorageKind;
  usedBytes: number;
  capacityBytes: number;
  status: "healthy" | "warning" | "offline";
  // Optional presentational fields (PR-3). Optional so existing consumers (e.g. /cloud,
  // which filters by `kind`) keep compiling unchanged.
  region?: string;
  encryption?: string;
  immutable?: boolean;
  protocol?: string;
  throughput?: string;
}

export type AlertSeverity = "critical" | "warning" | "info";

export interface Alert {
  id: string;
  severity: AlertSeverity;
  title: string;
  detail: string;
  deviceId?: string;
  at: string;
  acknowledged: boolean;
}

/** Frozen audit event vocabulary (DR-06 / internal/agent/audit). Closed set. */
export type AuditEventType =
  | "enroll.attempt"
  | "enroll.succeeded"
  | "enroll.failed"
  | "enroll.token_rejected"
  | "auth.failure"
  | "cert.renewed"
  | "spki_pin.mismatch"
  | "update.manifest_verified"
  | "update.signature_invalid"
  | "update.hash_mismatch"
  | "update.staged"
  | "update.swapped"
  | "update.health_ok"
  | "update.rolled_back"
  | "update.downgrade_blocked"
  | "config.reloaded"
  | "config.tamper_detected"
  | "license.signature_invalid"
  | "service.started"
  | "service.stopped";

export type AuditOutcome = "success" | "failure" | "denied";

export interface AuditEvent {
  id: string;
  /** Monotonic per-device chain sequence (BLAKE3 hash-chained, DR-06). */
  seq: number;
  eventType: AuditEventType;
  outcome: AuditOutcome;
  actor: string;
  deviceId?: string;
  tenantId: string;
  tsLocal: string;
}

export type Role = "Owner" | "Admin" | "Operator" | "Viewer";

export interface User {
  id: string;
  name: string;
  email: string;
  role: Role;
  lastActive: string;
}

export interface AgentVersion {
  version: string;
  channel: "stable" | "beta" | "dev";
  releasedAt: string;
  /** Devices currently on this version (mock aggregate). */
  devices: number;
  rolloutPct: number;
}

/** Advisory license status (S1-T17). Verification is real + fail-closed; consequences
 *  are advisory — never enforced in Sprint 1. */
export type LicenseStatus =
  | "valid"
  | "expired"
  | "not_yet_valid"
  | "tenant_mismatch"
  | "signature_invalid"
  | "missing";

export interface License {
  licenseId: string;
  tenantId: string;
  plan: string;
  seats: number;
  seatsUsed: number;
  quotaBytes: number;
  quotaUsedBytes: number;
  notAfter: string;
  status: LicenseStatus;
}

export interface DashboardSummary {
  devicesTotal: number;
  devicesOnline: number;
  devicesDegraded: number;
  jobsSucceeded24h: number;
  jobsFailed24h: number;
  protectedBytes: number;
  openAlerts: number;
}

// ---- Batch-A dashboard/executive mock view models (all illustrative, mock-only) ----

export interface ResiliencePillar {
  label: string;
  score: number;
  color: string;
}

export interface Resilience {
  score: number; // 0–100
  grade: string; // e.g. "A−"
  delta: number; // points vs last month
  pillars: ResiliencePillar[];
}

export interface Trend {
  labels: string[];
  protectedTB: number[];
  resilienceScore: number[];
}

export interface ActivitySlice {
  label: string;
  value: number;
  color: string;
}

export interface FleetHealth {
  online: number;
  warning: number;
  offline: number;
}

export interface SecurityPostureItem {
  label: string;
  ok: boolean;
  detail: string;
}

export interface TopRisk {
  id: string;
  severity: AlertSeverity;
  title: string;
  impact: string;
  owner: string;
}

export interface ExecutiveKpis {
  protectedAssets: number;
  protectedTB: number;
  successRate: number; // %
  complianceScore: number; // /100
  restoreReadiness: number; // /100
  storageRunwayDays: number;
}

export interface Financials {
  savedByDedupUsd: number;
  projectedAnnualUsd: number;
  dataAtRiskAvoidedUsd: number;
}

export interface DashboardInsights {
  resilience: Resilience;
  trend: Trend;
  activity: ActivitySlice[];
  fleet: FleetHealth;
  securityPosture: SecurityPostureItem[];
  topRisks: TopRisk[];
}

export interface ExecutiveSummary {
  resilience: Resilience;
  trend: Trend;
  kpis: ExecutiveKpis;
  financials: Financials;
  topRisks: TopRisk[];
}

// ---- Batch-A PR-2 view models: restore / locations / super (all illustrative mock) ----

/** A point in a recovery timeline (mock — restore engine lands in a later sprint). */
export interface RestorePoint {
  id: string;
  deviceId: string;
  deviceHost: string;
  kind: "Full image" | "Incremental";
  at: string;
  sizeBytes: number;
  verified: boolean;
  chainOk: boolean;
}

export interface RecoveryReadinessCheck {
  label: string;
  status: "pass" | "pending" | "fail";
  detail: string;
}

export interface RestoreActivityEntry {
  id: string;
  item: string;
  type: "File" | "Folder" | "Database" | "VM" | "Bare-metal";
  destination: string;
  by: string;
  status: JobStatus;
  /** 0–100 while running; null when not applicable. */
  progressPct: number | null;
  when: string;
}

/** Recursive file-browser node (mock listing of a recovery point). */
export interface FileNode {
  name: string;
  kind: "dir" | "file";
  ext?: string;
  sizeBytes?: number;
  modAt?: string;
  selected?: boolean;
  children?: FileNode[];
}

export interface RestoreCenter {
  confidenceScore: number;
  maxScore: number;
  sourceDeviceId: string;
  sourceHost: string;
  points: RestorePoint[];
  tree: FileNode[];
  readiness: RecoveryReadinessCheck[];
  activity: RestoreActivityEntry[];
}

// ---- /locations ----

export interface SiteRollup {
  id: string;
  tenantId: string;
  tenantName: string;
  tenantColor: string;
  name: string;
  city: string;
  deviceCount: number;
  online: number;
  offline: number;
  warning: number;
  linuxPrepOnly: number;
  health: number;
  protectedBytes: number;
  storageStatus: "healthy" | "warning" | "offline";
  storageName: string;
}

export interface RegionGroup {
  city: string;
  sites: SiteRollup[];
  deviceCount: number;
  avgHealth: number;
}

export interface LocationsOverview {
  sites: SiteRollup[];
  groups: RegionGroup[];
  kpis: { siteCount: number; deviceCount: number; cityCount: number; avgHealth: number };
}

// ---- /super (super-admin cross-tenant plane) ----

export interface TenantRollup {
  id: string;
  name: string;
  plan: string;
  color: string;
  devices: number;
  online: number;
  offline: number;
  health: number;
  /** Monthly recurring revenue (illustrative, EUR). */
  mrr: number;
  status: "active" | "suspended";
}

export interface RegionRollup {
  name: string;
  role: string;
  uptimePct: number;
  nodes: number;
  usedTB: number;
  capacityTB: number;
  tint: string;
}

export interface PlatformKpis {
  tenants: number;
  managedDevices: number;
  mrr: number;
  arr: number;
  slaPct: number;
  openCriticalAlerts: number;
}

export interface CrossTenantAlert {
  id: string;
  severity: AlertSeverity;
  title: string;
  source: string;
  category: string;
  at: string;
}

export interface SuperOverview {
  kpis: PlatformKpis;
  deviceTrend: number[];
  trendLabels: string[];
  tenants: TenantRollup[];
  regions: RegionRollup[];
  crossTenantAlerts: CrossTenantAlert[];
}

// ---- /storage bundled view model (illustrative mock) ----
// Segment shapes are inlined (structurally compatible with charts' CapacitySegment and
// panels' BreakdownSegment) so this domain type stays free of any component import.
export interface StorageOverview {
  kpis: {
    usedBytes: number;
    capacityBytes: number;
    usagePct: number;
    immutablePct: number;
    reductionRatio: number;
    runwayDays: number;
  };
  coverage: Array<{ pct: number; color: string; label?: string }>;
  tiers: Array<{ label: string; value: number; color: string }>;
  targets: StorageTarget[];
}

// ---- Batch-B PR-1 view models: jobs / audit / settings (all illustrative mock) ----

export interface JobsOverview {
  kpis: { total: number; running: number; successRatePct: number; failedToday: number };
  // Pipeline status distribution for the donut. "Cancelled" has no Job.status member, so
  // it is a mock-only aggregate here (NOT derived from Job[]).
  pipeline: Array<{ label: "Queued" | "Running" | "Success" | "Failed" | "Cancelled"; value: number; color: string }>;
  // 14-day trend; aligned arrays. labels are sparse (every Nth) for the x-axis.
  trend: { labels: string[]; completed: number[]; failed: number[] };
  throughput: { perDayTB: number; perWeekTB: number; spark: number[] };
  topFailureReasons: Array<{ reason: string; count: number; pct: number }>;
}

export interface AuditTimelineItem {
  id: string;
  seq: number;
  at: string;
  actor: string;
  deviceHost?: string;
  action: string;
  detail: string;
  category: string;
  severity: "ok" | "warn" | "crit" | "info";
  ip: string;
}

export interface AuditOverview {
  kpis: { eventsToday: number; critical: number; integrityOkPct: number; retentionYears: number };
  integrity: { algorithm: "BLAKE3"; tamperEvident: boolean; chainIntact: boolean; lastVerified: string; verifiedBlocks: number };
  timeline: AuditTimelineItem[];
  // Static mock detail for the (non-flyout) drawer — cryptographic-chain display only.
  // Hashes are deterministic display hex, never real cryptography.
  selectedDetail: { id: string; eventHash: string; previousHash: string; signatureValid: boolean; chainIntact: boolean; userAgent: string; result: "Success" | "Denied" };
}

export interface SettingsOverview {
  general: { timezone: string; dateFormat: string; language: string; organization: string };
  security: { mfaEnforced: boolean; sessionTimeout: string; passwordPolicy: string; encryptionKms: string };
  notifications: Array<{ channel: "Email alerts" | "Webhook" | "Slack" | "Microsoft Teams"; connected: boolean; detail: string }>;
  branding: { logoLabel: string; theme: "Dark" | "Light"; accentName: string; accentSwatches: Array<{ name: string; color: string }> };
  about: { product: string; version: string; build: string; environment: string; copyright: string };
}

// ---- Batch-B PR-2 view models: updates / licensing (all illustrative mock) ----

type ToneLite = "ok" | "warn" | "crit" | "info";

export interface UpdatesOverview {
  // KPI strip — derived from the devices fixture (truthful counts); illustrative aggregates noted.
  kpis: { fleetDevices: number; onCurrentPct: number; updateAvailable: number; rolledBack: number; signatureFailures: number };
  // Updater chain mapped 1:1 to the REAL DR-06 audit eventTypes — the source of trust truth.
  // (icon is mapped in the page from `step`, so this type imports nothing from components.)
  chain: Array<{ step: string; auditEvents: AuditEventType[]; tone: ToneLite }>;
  // Staged-rollout rings (Canary / Early Adopters / Production) — DISABLED preview mock.
  rings: Array<{ name: string; pct: number; devices: number; successPct: number; rollbacks: number; risk: "Low" | "Medium" | "High"; status: "pending" | "active" | "done"; color: string }>;
  // Release channels from agentVersions.channel.
  channels: Array<{ channel: "stable" | "beta" | "dev"; versions: number; devices: number; color: string }>;
  // Version-adoption donut segments over agentVersions.devices.
  adoption: Array<{ label: string; value: number; color: string }>;
  // Per-version adoption trend (aligned series for an AreaChart).
  adoptionTrend: { labels: string[]; series: Array<{ version: string; color: string; data: number[] }> };
  // DR-06 update-event timeline (illustrative, drawn from the update.* taxonomy).
  eventTimeline: Array<{ id: string; at: string; eventType: AuditEventType; tone: ToneLite; detail: string }>;
  // Trust panel — Ed25519, JCS-canonicalized manifest, anti-rollback, health gate, rollback-restore.
  trust: Array<{ label: string; detail: string; ok: boolean }>;
  // Failed health-gate / rollback list — derived from devices.updateStatus==='rolled_back'.
  rollbacks: Array<{ deviceHost: string; reason: string; at: string; auditEvent: AuditEventType }>;
}

export interface LicensingOverview {
  // All advisory; none enforced.
  kpis: { plan: string; seatsUsed: number; seats: number; seatPct: number; quotaUsedBytes: number; quotaBytes: number; quotaPct: number; status: LicenseStatus; daysToExpiry: number };
  // Advisory warning markers on the meters (e.g. 80/90/100) — surfaced, never block.
  warningThresholds: number[];
  // ALL SIX LicenseStatus values. active=true only on this tenant's real status; the rest are
  // illustrative parser-detected anomaly states that are NEVER blocked on.
  statusCatalog: Array<{ status: LicenseStatus; active: boolean; advisoryAction: string; auditEvent?: AuditEventType }>;
  // Plan features/limits — advisory caps, not enforced quotas.
  entitlements: Array<{ feature: string; limit: string; used: string; enforced: false }>;
  // Seat-allocation breakdown (donut/capacity segments).
  seatBreakdown: Array<{ label: string; value: number; color: string }>;
  // Quota-consumption trend (AreaChart series, % of quota over time).
  quotaTrend: { labels: string[]; data: number[] };
  // License audit-trail preview — derives license.signature_invalid from the DR-06 taxonomy.
  auditTimeline: Array<{ id: string; at: string; eventType: AuditEventType; outcome: AuditOutcome; detail: string; severity: ToneLite }>;
  // Tenant license history.
  history: Array<{ at: string; event: string; detail: string }>;
  // Renewal/expiry milestone timeline.
  renewalTimeline: Array<{ at: string; label: string; state: "done" | "current" | "future" }>;
  renewal: { issuedAt: string; notAfter: string; daysToExpiry: number; autoRenew: boolean; note: string };
}

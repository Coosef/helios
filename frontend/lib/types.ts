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

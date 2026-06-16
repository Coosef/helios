// API facade — the single boundary between the UI and its data source.
//
// UI Sprint 1 ships ONLY the `mockApi` implementation (backed by lib/fixtures). When
// the management OpenAPI backend lands, generate a typed client that implements this
// same `HeliosApi` interface and swap it in `getApi()` — no screen changes required.
// Screens MUST import data only through getApi(), never from lib/fixtures directly.

import type {
  AgentVersion, Alert, AuditEvent, DashboardSummary, Device, Job, License,
  LocationSite, StorageTarget, Tenant, User,
} from "./types";
import * as fx from "./fixtures";

export interface HeliosApi {
  getTenants(): Promise<Tenant[]>;
  getLocations(): Promise<LocationSite[]>;
  getDashboard(): Promise<DashboardSummary>;
  getDevices(): Promise<Device[]>;
  getDevice(id: string): Promise<Device | undefined>;
  getJobs(): Promise<Job[]>;
  getJob(id: string): Promise<Job | undefined>;
  getStorageTargets(): Promise<StorageTarget[]>;
  getAlerts(): Promise<Alert[]>;
  getAuditEvents(): Promise<AuditEvent[]>;
  getUsers(): Promise<User[]>;
  getAgentVersions(): Promise<AgentVersion[]>;
  getLicense(): Promise<License>;
}

// Resolve immediately — these are local fixtures. The Promise shape is intentional so
// call sites already look like real network calls (await getApi().getDevices()).
const ok = <T>(v: T): Promise<T> => Promise.resolve(v);

export const mockApi: HeliosApi = {
  getTenants: () => ok(fx.tenants),
  getLocations: () => ok(fx.locations),
  getDashboard: () => ok(fx.dashboard),
  getDevices: () => ok(fx.devices),
  getDevice: (id) => ok(fx.devices.find((d) => d.id === id)),
  getJobs: () => ok(fx.jobs),
  getJob: (id) => ok(fx.jobs.find((j) => j.id === id)),
  getStorageTargets: () => ok(fx.storageTargets),
  getAlerts: () => ok(fx.alerts),
  getAuditEvents: () => ok(fx.auditEvents),
  getUsers: () => ok(fx.users),
  getAgentVersions: () => ok(fx.agentVersions),
  getLicense: () => ok(fx.license),
};

/** The active API. Today: always the mock. Future: choose the generated client. */
export function getApi(): HeliosApi {
  return mockApi;
}

/** True when the console is running against mock fixtures (UI Sprint 1 — always true). */
export const IS_MOCK = true;

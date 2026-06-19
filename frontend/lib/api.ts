// API facade — the single boundary between the UI and its data source.
//
// UI Sprint 1 ships ONLY the `mockApi` implementation (backed by lib/fixtures). When
// the management OpenAPI backend lands, generate a typed client that implements this
// same `HeliosApi` interface and swap it in `getApi()` — no screen changes required.
// Screens MUST import data only through getApi(), never from lib/fixtures directly.

import type {
  AgentVersion, Alert, AlertsOverview, AuditEvent, AuditOverview, DashboardInsights,
  DashboardSummary, Device, ExecutiveSummary, Job, JobsOverview, License, LicensingOverview,
  LocationSite, LocationsOverview, RestoreCenter, SettingsOverview, StorageOverview,
  StorageTarget, SuperOverview, Tenant, UpdatesOverview, User, UsersOverview,
} from "./types";
import * as fx from "./fixtures";

export interface HeliosApi {
  getTenants(): Promise<Tenant[]>;
  getLocations(): Promise<LocationSite[]>;
  getLocationsOverview(): Promise<LocationsOverview>;
  getDashboard(): Promise<DashboardSummary>;
  getDashboardInsights(): Promise<DashboardInsights>;
  getExecutiveSummary(): Promise<ExecutiveSummary>;
  getRestoreCenter(): Promise<RestoreCenter>;
  getSuperOverview(): Promise<SuperOverview>;
  getDevices(): Promise<Device[]>;
  getDevice(id: string): Promise<Device | undefined>;
  getJobs(): Promise<Job[]>;
  getJob(id: string): Promise<Job | undefined>;
  getJobsOverview(): Promise<JobsOverview>;
  getStorageTargets(): Promise<StorageTarget[]>;
  getStorageOverview(): Promise<StorageOverview>;
  getAlerts(): Promise<Alert[]>;
  getAlertsOverview(): Promise<AlertsOverview>;
  getAuditEvents(): Promise<AuditEvent[]>;
  getAuditOverview(): Promise<AuditOverview>;
  getUsers(): Promise<User[]>;
  getUsersOverview(): Promise<UsersOverview>;
  getAgentVersions(): Promise<AgentVersion[]>;
  getUpdatesOverview(): Promise<UpdatesOverview>;
  getLicense(): Promise<License>;
  getLicensingOverview(): Promise<LicensingOverview>;
  getSettingsOverview(): Promise<SettingsOverview>;
}

// Resolve immediately — these are local fixtures. The Promise shape is intentional so
// call sites already look like real network calls (await getApi().getDevices()).
const ok = <T>(v: T): Promise<T> => Promise.resolve(v);

export const mockApi: HeliosApi = {
  getTenants: () => ok(fx.tenants),
  getLocations: () => ok(fx.locations),
  getLocationsOverview: () => ok(fx.locationsOverview),
  getDashboard: () => ok(fx.dashboard),
  getDashboardInsights: () => ok(fx.dashboardInsights),
  getExecutiveSummary: () => ok(fx.executiveSummary),
  getRestoreCenter: () => ok(fx.restoreCenter),
  getSuperOverview: () => ok(fx.superOverview),
  getDevices: () => ok(fx.devices),
  getDevice: (id) => ok(fx.devices.find((d) => d.id === id)),
  getJobs: () => ok(fx.jobs),
  getJob: (id) => ok(fx.jobs.find((j) => j.id === id)),
  getJobsOverview: () => ok(fx.jobsOverview),
  getStorageTargets: () => ok(fx.storageTargets),
  getStorageOverview: () => ok(fx.storageOverview),
  getAlerts: () => ok(fx.alerts),
  getAlertsOverview: () => ok(fx.alertsOverview),
  getAuditEvents: () => ok(fx.auditEvents),
  getAuditOverview: () => ok(fx.auditOverview),
  getUsers: () => ok(fx.users),
  getUsersOverview: () => ok(fx.usersOverview),
  getAgentVersions: () => ok(fx.agentVersions),
  getUpdatesOverview: () => ok(fx.updatesOverview),
  getLicense: () => ok(fx.license),
  getLicensingOverview: () => ok(fx.licensingOverview),
  getSettingsOverview: () => ok(fx.settingsOverview),
};

/** The active API. Today: always the mock. Future: choose the generated client. */
export function getApi(): HeliosApi {
  return mockApi;
}

/** True when the console is running against mock fixtures (UI Sprint 1 — always true). */
export const IS_MOCK = true;

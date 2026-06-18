// Pure view-model derivations over the mock fixtures. No data of its own — these turn
// existing Device fields into the health signals the UI shows. Kept out of the JSX so the
// heuristics live in one clearly-labelled place. Illustrative only: a real per-device
// health metric is backend-pending; this is a deterministic proxy for the design preview.

import type { Device } from "./types";

export interface DeviceHealth {
  score: number; // 0–100
  grade: "Excellent" | "Good" | "Warning" | "Critical";
  color: string; // theme token
  tone: "ok" | "warn" | "crit";
  factors: { backup: number; connectivity: number; trust: number; update: number; storage: number };
}

const PRESENCE: Record<Device["presence"], number> = { online: 100, stale: 55, offline: 20 };
const ENROLL: Record<Device["enrollment"], number> = {
  enrolled: 100, enrolling: 70, degraded: 55, unenrolled: 20, decommissioned: 0,
};
const UPDATE: Record<Device["updateStatus"], number> = {
  up_to_date: 100, updating: 85, update_available: 80, rolled_back: 45,
};

/** Composite device-health score (illustrative). Weights mirror the design package:
 *  backup .3, connectivity .25, trust .2, update .15, storage .1. */
export function deviceHealth(d: Device): DeviceHealth {
  const connectivity = PRESENCE[d.presence];
  const trust = ENROLL[d.enrollment];
  const backup = Math.round((trust + connectivity) / 2); // proxy: enrolled + reachable ⇒ can protect
  const update = UPDATE[d.updateStatus];
  const storage = 80; // placeholder — real per-device storage telemetry is backend-pending
  const score = Math.round(backup * 0.3 + connectivity * 0.25 + trust * 0.2 + update * 0.15 + storage * 0.1);
  const [grade, color, tone] =
    score >= 90 ? (["Excellent", "var(--ok)", "ok"] as const)
    : score >= 75 ? (["Good", "var(--accent-2)", "ok"] as const)
    : score >= 50 ? (["Warning", "var(--warn)", "warn"] as const)
    : (["Critical", "var(--crit)", "crit"] as const);
  return { score, grade, color, tone, factors: { backup, connectivity, trust, update, storage } };
}

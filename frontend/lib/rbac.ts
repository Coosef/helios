// UI-only role gating (mock). Mirrors the prototype's Owner/Admin/Operator/Viewer
// model. In production the SaaS backend is the source of truth for authorization;
// this is UX gating only and must never be relied on for security.

import type { Role } from "./types";

export const ROLES: Role[] = ["Owner", "Admin", "Operator", "Viewer"];

export const ROLE_LEVEL: Record<Role, number> = {
  Owner: 4,
  Admin: 3,
  Operator: 2,
  Viewer: 1,
};

export interface Capabilities {
  read: boolean;
  write: boolean; // Operator+
  manage: boolean; // Admin+
  admin: boolean; // Owner
}

export function capabilities(role: Role): Capabilities {
  const lvl = ROLE_LEVEL[role];
  return { read: lvl >= 1, write: lvl >= 2, manage: lvl >= 3, admin: lvl >= 4 };
}

// Navigation model, ported from the design package (Backup/shell.jsx) and mapped to
// real Next.js routes. Two planes: the tenant operator console and the super-admin
// control plane. `icon` is a key into components/icons.tsx; `tkey` is an i18n key.

import type { IconKey } from "@/components/icons";

export interface NavItem {
  id: string;
  label: string;
  tkey: string;
  href: string;
  icon: IconKey;
  badge?: string;
  crit?: boolean;
  ai?: boolean;
}

export interface NavGroup {
  group: string;
  gkey: string;
  items: NavItem[];
}

export const NAV: NavGroup[] = [
  {
    group: "Operations", gkey: "nav.group.operations", items: [
      { id: "executive", label: "Executive Overview", tkey: "nav.executive", href: "/executive", icon: "target" },
      { id: "dashboard", label: "Dashboard", tkey: "nav.dashboard", href: "/dashboard", icon: "dashboard" },
      { id: "devices", label: "Devices", tkey: "nav.devices", href: "/devices", icon: "devices", badge: "48" },
      { id: "jobs", label: "Backup Jobs", tkey: "nav.jobs", href: "/jobs", icon: "jobs" },
      { id: "restore", label: "Restore Center", tkey: "nav.restore", href: "/restore", icon: "restore" },
    ],
  },
  {
    group: "Intelligence", gkey: "nav.group.intelligence", items: [
      { id: "intelligence", label: "Helios Intelligence", tkey: "nav.intelligence", href: "/intelligence", icon: "sparkle", ai: true, badge: "AI" },
    ],
  },
  {
    group: "Storage & Licensing", gkey: "nav.group.storage", items: [
      { id: "storage", label: "Storage", tkey: "nav.storage", href: "/storage", icon: "storage" },
      { id: "cloud", label: "Helios Cloud", tkey: "nav.cloud", href: "/cloud", icon: "cloud" },
      { id: "licensing", label: "Licensing", tkey: "nav.licensing", href: "/licensing", icon: "license" },
    ],
  },
  {
    group: "Security & Governance", gkey: "nav.group.security", items: [
      { id: "alerts", label: "Alerts", tkey: "nav.alerts", href: "/alerts", icon: "alerts", badge: "4", crit: true },
      { id: "audit", label: "Audit Logs", tkey: "nav.audit", href: "/audit", icon: "audit" },
      { id: "users", label: "User Management", tkey: "nav.users", href: "/users", icon: "users" },
      { id: "locations", label: "Locations", tkey: "nav.locations", href: "/locations", icon: "pin" },
    ],
  },
  {
    group: "Platform", gkey: "nav.group.platform", items: [
      { id: "updates", label: "Agent Updates", tkey: "nav.updates", href: "/updates", icon: "update" },
      { id: "reports", label: "Reports", tkey: "nav.reports", href: "/reports", icon: "reports" },
      { id: "settings", label: "Settings", tkey: "nav.settings", href: "/settings", icon: "settings" },
    ],
  },
];

export const SUPER_NAV: NavGroup[] = [
  {
    group: "Control Plane", gkey: "nav.group.controlPlane", items: [
      { id: "sa-overview", label: "Global Overview", tkey: "nav.saOverview", href: "/super", icon: "dashboard" },
      { id: "sa-tenants", label: "Tenants", tkey: "nav.saTenants", href: "/super/tenants", icon: "tenants" },
      { id: "sa-monitoring", label: "Global Monitoring", tkey: "nav.saMonitoring", href: "/super/monitoring", icon: "activity" },
    ],
  },
  {
    group: "Governance", gkey: "nav.group.governance", items: [
      { id: "sa-billing", label: "Billing & Licensing", tkey: "nav.saBilling", href: "/super/billing", icon: "license" },
      { id: "sa-settings", label: "Platform Settings", tkey: "nav.saSettings", href: "/super/settings", icon: "settings" },
    ],
  },
];

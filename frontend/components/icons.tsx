import type { ReactNode } from "react";

// Compact stroke-icon set (Lucide-style geometry). Keys are referenced by the nav
// model and shell. Clean and maintainable over pixel-matching the prototype's set.

export type IconKey =
  | "dashboard" | "devices" | "jobs" | "restore" | "storage" | "cloud" | "license"
  | "alerts" | "audit" | "users" | "pin" | "update" | "reports" | "settings"
  | "target" | "sparkle" | "tenants" | "activity" | "shield" | "search" | "play"
  | "logout" | "bell" | "check" | "x" | "sun" | "moon" | "chevron" | "clock"
  | "globe" | "plus" | "warning";

const P: Record<IconKey, ReactNode> = {
  dashboard: (<><rect x="3" y="3" width="7" height="9" rx="1" /><rect x="14" y="3" width="7" height="5" rx="1" /><rect x="14" y="12" width="7" height="9" rx="1" /><rect x="3" y="16" width="7" height="5" rx="1" /></>),
  devices: (<><rect x="2" y="4" width="14" height="10" rx="2" /><path d="M2 18h14" /><rect x="18" y="8" width="4" height="12" rx="1" /></>),
  jobs: (<><path d="M4 6h16M4 12h16M4 18h10" /></>),
  restore: (<><path d="M3 12a9 9 0 1 0 3-6.7" /><path d="M3 4v5h5" /></>),
  storage: (<><ellipse cx="12" cy="5" rx="8" ry="3" /><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5" /><path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3" /></>),
  cloud: (<><path d="M6 18a4 4 0 0 1 .5-8 6 6 0 0 1 11.4 1.5A3.5 3.5 0 0 1 17 18Z" /></>),
  license: (<><circle cx="9" cy="9" r="5" /><path d="M12.5 12.5 17 21l-2.5-1L12 22l-2-8" /></>),
  alerts: (<><path d="M12 3a6 6 0 0 0-6 6c0 5-2 6-2 6h16s-2-1-2-6a6 6 0 0 0-6-6Z" /><path d="M10 20a2 2 0 0 0 4 0" /></>),
  audit: (<><rect x="4" y="3" width="16" height="18" rx="2" /><path d="M8 8h8M8 12h8M8 16h5" /></>),
  users: (<><circle cx="9" cy="8" r="3" /><path d="M3 20a6 6 0 0 1 12 0" /><path d="M16 6a3 3 0 0 1 0 6M21 20a6 6 0 0 0-5-5.9" /></>),
  pin: (<><path d="M12 21s7-6.4 7-12a7 7 0 1 0-14 0c0 5.6 7 12 7 12Z" /><circle cx="12" cy="9" r="2.5" /></>),
  update: (<><path d="M21 12a9 9 0 1 1-3-6.7" /><path d="M21 4v5h-5" /></>),
  reports: (<><rect x="4" y="3" width="16" height="18" rx="2" /><path d="M8 13v4M12 9v8M16 11v6" /></>),
  settings: (<><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-2.7 1.1V21a2 2 0 1 1-4 0v-.1A1.6 1.6 0 0 0 6.8 19l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1A1.6 1.6 0 0 0 3 13.6H3a2 2 0 1 1 0-4h.1A1.6 1.6 0 0 0 4.6 7L4.5 6.9a2 2 0 1 1 2.8-2.8l.1.1A1.6 1.6 0 0 0 10 3.3V3a2 2 0 1 1 4 0v.1A1.6 1.6 0 0 0 17 4.6l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0-.3 1.8" /></>),
  target: (<><circle cx="12" cy="12" r="9" /><circle cx="12" cy="12" r="5" /><circle cx="12" cy="12" r="1.5" /></>),
  sparkle: (<><path d="M12 3l2.2 5.8L20 11l-5.8 2.2L12 19l-2.2-5.8L4 11l5.8-2.2Z" /></>),
  tenants: (<><rect x="3" y="9" width="8" height="12" rx="1" /><rect x="13" y="3" width="8" height="18" rx="1" /><path d="M6 13h2M6 17h2M16 7h2M16 11h2M16 15h2" /></>),
  activity: (<><path d="M3 12h4l3 8 4-16 3 8h4" /></>),
  shield: (<><path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6Z" /><path d="m9 12 2 2 4-4" /></>),
  search: (<><circle cx="11" cy="11" r="7" /><path d="m21 21-4.3-4.3" /></>),
  play: (<><path d="M7 5v14l11-7Z" /></>),
  logout: (<><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" /><path d="M16 17l5-5-5-5M21 12H9" /></>),
  bell: (<><path d="M12 3a6 6 0 0 0-6 6c0 5-2 6-2 6h16s-2-1-2-6a6 6 0 0 0-6-6Z" /><path d="M10 20a2 2 0 0 0 4 0" /></>),
  check: (<><path d="m5 12 5 5 9-11" /></>),
  x: (<><path d="M6 6l12 12M18 6 6 18" /></>),
  sun: (<><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M5 19l1.5-1.5M17.5 6.5 19 5" /></>),
  moon: (<><path d="M21 12.8A8 8 0 1 1 11.2 3a6 6 0 0 0 9.8 9.8Z" /></>),
  chevron: (<><path d="m6 9 6 6 6-6" /></>),
  clock: (<><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>),
  globe: (<><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c2.5 2.5 2.5 15 0 18M12 3c-2.5 2.5-2.5 15 0 18" /></>),
  plus: (<><path d="M12 5v14M5 12h14" /></>),
  warning: (<><path d="M12 3 2 20h20Z" /><path d="M12 10v4M12 17.5v.5" /></>),
};

export interface IconProps {
  name: IconKey;
  size?: number;
  className?: string;
}

export function Icon({ name, size = 18, className }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" className={className} aria-hidden="true">
      {P[name]}
    </svg>
  );
}

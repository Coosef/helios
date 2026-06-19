"use client";

// The only PAGE-LEVEL client component under (app)/ (the shell — AppShell/Sidebar/Topbar —
// is already a client boundary at the layout). Holds ONLY the active-tab index (UI state) —
// no data fetching, no fixture import, deterministic initial tab (0) so SSR and CSR match
// (hydration-safe). All six panels are server-rendered and passed in as `content`.

import { useState, type ReactNode } from "react";
import { cx } from "./ui";
import { Icon, type IconKey } from "./icons";

export interface SettingsTab {
  key: string;
  label: string;
  icon: IconKey;
  content: ReactNode;
}

export function SettingsTabs({ tabs }: { tabs: SettingsTab[] }) {
  const [active, setActive] = useState(0);
  return (
    <div className="stack">
      <div className="scroll-x">
        <div className="seg" role="tablist" aria-label="Settings sections">
          {tabs.map((t, i) => (
            <button
              key={t.key}
              type="button"
              role="tab"
              aria-selected={i === active}
              className={cx(i === active && "active")}
              onClick={() => setActive(i)}
            >
              <Icon name={t.icon} size={14} /> {t.label}
            </button>
          ))}
        </div>
      </div>
      <div role="tabpanel">{tabs[active]?.content}</div>
    </div>
  );
}

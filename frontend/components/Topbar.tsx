"use client";

import { usePathname } from "next/navigation";
import { NAV, SUPER_NAV } from "@/lib/nav";
import { useAppState } from "./app-state";
import { useI18n, LANGS, type Lang } from "@/lib/i18n";
import { capabilities } from "@/lib/rbac";
import { Icon } from "./icons";

function titleForPath(pathname: string): { label: string; tkey?: string } {
  const all = [...NAV, ...SUPER_NAV].flatMap((g) => g.items);
  // Longest matching href wins (so /devices/x matches Devices).
  const match = all
    .filter((i) => pathname === i.href || pathname.startsWith(i.href + "/"))
    .sort((a, b) => b.href.length - a.href.length)[0];
  return match ? { label: match.label, tkey: match.tkey } : { label: "Helios" };
}

export function Topbar() {
  const pathname = usePathname();
  const { theme, setTheme, role } = useAppState();
  const { t, lang, setLang } = useI18n();
  const can = capabilities(role);
  const title = titleForPath(pathname);

  return (
    <header className="topbar between">
      <div className="vcenter" style={{ gap: 12 }}>
        <h2 className="tb-title display" style={{ margin: 0, fontSize: 16, fontWeight: 600 }}>
          {title.tkey ? t(title.tkey, title.label) : title.label}
        </h2>
        <span className="tb-livepill"><span className="bdot bdot-pulse" /> LIVE</span>
      </div>

      <div className="tb-spacer" />

      <div className="vcenter" style={{ gap: 10 }}>
        <div className="search" role="search" title="Command palette (placeholder)">
          <Icon name="search" size={15} />
          <input className="input" placeholder={t("topbar.search", "Search devices, jobs, files…")} aria-label="Search" readOnly />
          <kbd className="muted fs-11">⌘K</kbd>
        </div>

        {/* Run Backup is disabled below Operator and is a mock no-op in Sprint 1. */}
        <button className="btn btn-primary btn-sm" disabled={!can.write} title="Mock action (no backend yet)">
          <Icon name="play" size={14} /> {t("topbar.runBackup", "Run Backup")}
        </button>

        <select
          value={lang}
          onChange={(e) => setLang(e.target.value as Lang)}
          aria-label="Language"
          className="btn btn-sm"
          style={{ cursor: "pointer" }}
        >
          {LANGS.map((l) => <option key={l.id} value={l.id} style={{ color: "#000" }}>{l.label}</option>)}
        </select>

        <button
          className="btn btn-icon btn-sm"
          onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
          aria-label="Toggle theme"
          title="Toggle theme"
        >
          <Icon name={theme === "dark" ? "sun" : "moon"} size={16} />
        </button>
      </div>
    </header>
  );
}

"use client";

// Lightweight i18n scaffold (EN / TR / DE), ported in spirit from Backup/i18n.jsx.
// t(key, fallback) resolves: current lang -> English -> fallback -> key. Dictionaries
// are intentionally partial (shell chrome + nav); page bodies pass English fallbacks,
// so adding a language later is additive and never crashes on a missing key.

import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";

export type Lang = "en" | "tr" | "de";
export const LANGS: { id: Lang; label: string }[] = [
  { id: "en", label: "English" },
  { id: "tr", label: "Türkçe" },
  { id: "de", label: "Deutsch" },
];

type Dict = Record<string, string>;

const en: Dict = {
  "brand.tagline": "Protect · Restore",
  "topbar.search": "Search devices, jobs, files…",
  "topbar.runBackup": "Run Backup",
  "nav.group.operations": "Operations",
  "nav.group.intelligence": "Intelligence",
  "nav.group.storage": "Storage & Licensing",
  "nav.group.security": "Security & Governance",
  "nav.group.platform": "Platform",
  "nav.group.controlPlane": "Control Plane",
  "nav.group.governance": "Governance",
  "nav.executive": "Executive Overview",
  "nav.dashboard": "Dashboard",
  "nav.devices": "Devices",
  "nav.jobs": "Backup Jobs",
  "nav.restore": "Restore Center",
  "nav.intelligence": "Helios Intelligence",
  "nav.storage": "Storage",
  "nav.cloud": "Helios Cloud",
  "nav.licensing": "Licensing",
  "nav.alerts": "Alerts",
  "nav.audit": "Audit Logs",
  "nav.users": "User Management",
  "nav.locations": "Locations",
  "nav.updates": "Agent Updates",
  "nav.reports": "Reports",
  "nav.settings": "Settings",
};

const tr: Dict = {
  "brand.tagline": "Koru · Geri Yükle",
  "topbar.search": "Cihaz, iş, dosya ara…",
  "topbar.runBackup": "Yedekle",
  "nav.group.operations": "Operasyon",
  "nav.group.intelligence": "Zeka",
  "nav.group.storage": "Depolama & Lisans",
  "nav.group.security": "Güvenlik & Yönetişim",
  "nav.group.platform": "Platform",
  "nav.dashboard": "Panel",
  "nav.devices": "Cihazlar",
  "nav.jobs": "Yedek İşleri",
  "nav.restore": "Geri Yükleme",
  "nav.storage": "Depolama",
  "nav.alerts": "Uyarılar",
  "nav.audit": "Denetim Kayıtları",
  "nav.users": "Kullanıcılar",
  "nav.updates": "Ajan Güncellemeleri",
  "nav.settings": "Ayarlar",
};

const de: Dict = {
  "brand.tagline": "Schützen · Wiederherstellen",
  "topbar.search": "Geräte, Jobs, Dateien suchen…",
  "topbar.runBackup": "Backup starten",
  "nav.group.operations": "Betrieb",
  "nav.group.security": "Sicherheit & Governance",
  "nav.group.platform": "Plattform",
  "nav.dashboard": "Übersicht",
  "nav.devices": "Geräte",
  "nav.jobs": "Backup-Jobs",
  "nav.restore": "Wiederherstellung",
  "nav.storage": "Speicher",
  "nav.alerts": "Warnungen",
  "nav.audit": "Audit-Protokolle",
  "nav.users": "Benutzer",
  "nav.updates": "Agent-Updates",
  "nav.settings": "Einstellungen",
};

const DICTS: Record<Lang, Dict> = { en, tr, de };

interface I18nCtx {
  lang: Lang;
  setLang: (l: Lang) => void;
  t: (key: string, fallback?: string) => string;
}

const Ctx = createContext<I18nCtx | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>("en");

  useEffect(() => {
    const saved = localStorage.getItem("helios-lang") as Lang | null;
    if (saved && DICTS[saved]) setLangState(saved);
  }, []);

  const setLang = useCallback((l: Lang) => {
    setLangState(l);
    localStorage.setItem("helios-lang", l);
  }, []);

  const t = useCallback(
    (key: string, fallback?: string) => DICTS[lang][key] ?? en[key] ?? fallback ?? key,
    [lang],
  );

  return <Ctx.Provider value={{ lang, setLang, t }}>{children}</Ctx.Provider>;
}

export function useI18n(): I18nCtx {
  const c = useContext(Ctx);
  if (!c) throw new Error("useI18n must be used within I18nProvider");
  return c;
}

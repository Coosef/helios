"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { NAV, SUPER_NAV, type NavGroup } from "@/lib/nav";
import { ROLES } from "@/lib/rbac";
import type { Tenant } from "@/lib/types";
import { useAppState } from "./app-state";
import { useI18n } from "@/lib/i18n";
import { Icon } from "./icons";
import { cx } from "./ui";

function isActive(pathname: string, href: string): boolean {
  if (href === "/super") return pathname === "/super";
  return pathname === href || pathname.startsWith(href + "/");
}

export function Sidebar({ tenants }: { tenants: Tenant[] }) {
  const pathname = usePathname();
  const router = useRouter();
  const { role, setRole, tenantId, setTenantId } = useAppState();
  const { t } = useI18n();
  const isSuper = pathname.startsWith("/super");
  const groups: NavGroup[] = isSuper ? SUPER_NAV : NAV;
  const tenant = tenants.find((x) => x.id === tenantId) ?? tenants[0];

  return (
    <aside className="sidebar">
      <div className="sb-brand">
        <span className="sb-logo">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/assets/helios-mark.svg" alt="" width={20} height={20} />
        </span>
        <span className="sb-wordmark">
          <b>Helios</b>
          <span>{t("brand.tagline", "Protect · Restore")}</span>
        </span>
      </div>

      {!isSuper && tenant && (
        <label className="sb-tenant" title="Tenant scope (mock)">
          <span className="sb-tenant-ava" style={{ background: tenant.color }} />
          <span className="sb-tenant-meta">
            <select
              value={tenantId}
              onChange={(e) => setTenantId(e.target.value)}
              aria-label="Tenant"
              style={{ background: "transparent", border: 0, color: "var(--text-1)", font: "inherit", width: "100%", cursor: "pointer" }}
            >
              {tenants.map((tn) => <option key={tn.id} value={tn.id} style={{ color: "#000" }}>{tn.name}</option>)}
            </select>
            <span className="sb-tenant-cv muted fs-11">{tenant.plan}</span>
          </span>
        </label>
      )}

      <nav className="sb-nav">
        {groups.map((g) => (
          <div key={g.group}>
            <div className="sb-group-label">{t(g.gkey, g.group)}</div>
            {g.items.map((it) => (
              <Link
                key={it.id}
                href={it.href}
                className={cx("nav-item", it.ai && "nav-ai", isActive(pathname, it.href) && "active")}
                title={t(it.tkey, it.label)}
              >
                <Icon name={it.icon} size={18} />
                <span className="nav-label">{t(it.tkey, it.label)}</span>
                {it.badge && <span className={cx("nav-badge", it.crit && "crit", it.ai && "ai")}>{it.badge}</span>}
              </Link>
            ))}
          </div>
        ))}
      </nav>

      <div className="sb-foot">
        {/* Plane toggle (mock control-plane vs tenant). */}
        <button
          className="nav-item"
          onClick={() => router.push(isSuper ? "/dashboard" : "/super")}
          style={{ width: "100%", border: 0, background: "transparent", cursor: "pointer", color: "#818cf8" }}
        >
          <Icon name="shield" size={16} />
          <span className="nav-label">{isSuper ? "Exit control plane" : "Enter control plane"}</span>
        </button>

        {/* View-as role switcher (UI-only RBAC mock). */}
        <label className="sb-user" title="View as role (UI-only mock)">
          <span className="sb-user-ava"><Icon name="users" size={15} /></span>
          <span className="sb-user-meta">
            <select
              value={role}
              onChange={(e) => setRole(e.target.value as typeof ROLES[number])}
              aria-label="Role"
              style={{ background: "transparent", border: 0, color: "var(--text-1)", font: "inherit", cursor: "pointer" }}
            >
              {ROLES.map((r) => <option key={r} value={r} style={{ color: "#000" }}>{r}</option>)}
            </select>
            <span className="muted fs-11">View-as role</span>
          </span>
        </label>
      </div>
    </aside>
  );
}

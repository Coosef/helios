import type { ReactNode } from "react";
import { Icon, type IconKey } from "./icons";

export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(" ");
}

export type Tone = "ok" | "warn" | "crit" | "info" | "ai" | "muted";

const TONE_CLASS: Record<Tone, string> = {
  ok: "badge-ok",
  warn: "badge-warn",
  crit: "badge-crit",
  info: "badge-info",
  ai: "badge-ai",
  muted: "badge-muted",
};

export function Badge({ tone = "muted", children, lg }: { tone?: Tone; children: ReactNode; lg?: boolean }) {
  return <span className={cx("badge", TONE_CLASS[tone], lg && "badge-lg")}>{children}</span>;
}

// Maps the real domain status vocabularies (enrollment FSM, presence, job/update/
// license/alert states) to a visual tone. Single source so screens stay consistent.
const TONE_BY_STATUS: Record<string, Tone> = {
  // enrollment
  enrolled: "ok", enrolling: "info", unenrolled: "muted", degraded: "warn", decommissioned: "muted",
  // presence
  online: "ok", stale: "warn", offline: "crit",
  // jobs
  success: "ok", running: "info", queued: "muted", failed: "crit",
  // update
  up_to_date: "ok", update_available: "info", updating: "info", rolled_back: "warn",
  // storage / health
  healthy: "ok", warning: "warn",
  // license
  valid: "ok", expired: "crit", not_yet_valid: "warn", tenant_mismatch: "warn", signature_invalid: "crit", missing: "muted",
  // alerts
  critical: "crit", info: "info",
};

export function StatusBadge({ status, label }: { status: string; label?: string }) {
  return <Badge tone={TONE_BY_STATUS[status] ?? "muted"}>{label ?? status.replace(/_/g, " ")}</Badge>;
}

export function Card({ children, pad = true, className }: { children: ReactNode; pad?: boolean; className?: string }) {
  return <div className={cx("card", pad && "card-pad", className)}>{children}</div>;
}

export function CardHead({ title, sub, right }: { title: ReactNode; sub?: ReactNode; right?: ReactNode }) {
  return (
    <div className="card-hd between">
      <div>
        <div className="fw-6">{title}</div>
        {sub && <div className="muted fs-12">{sub}</div>}
      </div>
      {right}
    </div>
  );
}

export function StatCard({ icon, tint = "var(--accent)", label, value, sub, delta, spark }: {
  icon: IconKey; tint?: string; label: string; value: ReactNode; sub?: ReactNode;
  delta?: number; spark?: number[];
}) {
  return (
    <div className="card stat">
      <div className="stat-top">
        <span className="stat-ico" style={{ color: tint, background: "color-mix(in oklab, " + tint + " 16%, transparent)" }}>
          <Icon name={icon} size={18} />
        </span>
        <span className="muted fs-12">{label}</span>
      </div>
      <div className="stat-val display tnum">{value}</div>
      {(sub || delta != null) && (
        <div className="stat-sub muted fs-12">
          {delta != null && (
            <span className={delta >= 0 ? "delta-up" : "delta-down"} style={{ fontWeight: 600 }}>
              {delta >= 0 ? "▲" : "▼"} {Math.abs(delta)}%
            </span>
          )}
          {sub && <span>{sub}</span>}
        </div>
      )}
      {spark && spark.length > 1 && (
        <div className="stat-spark"><Sparkline data={spark} color={tint} h={40} /></div>
      )}
    </div>
  );
}

/** Small color swatch used in chart legends. */
export function Swatch({ color, size = 9 }: { color: string; size?: number }) {
  return <span style={{ width: size, height: size, borderRadius: 3, background: color, display: "inline-block" }} />;
}

export function Meter({ value, color = "var(--accent)", thin }: { value: number; color?: string; thin?: boolean }) {
  return (
    <div className={cx("meter", thin && "meter-thin")}>
      <span style={{ width: Math.max(0, Math.min(100, value)) + "%", background: color }} />
    </div>
  );
}

export function PageHeader({ title, sub, actions }: { title: ReactNode; sub?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="page-hd between">
      <div>
        <h1 className="display" style={{ margin: 0, fontSize: 22, fontWeight: 600 }}>{title}</h1>
        {sub && <div className="muted fs-13" style={{ marginTop: 4 }}>{sub}</div>}
      </div>
      {actions && <div className="vcenter" style={{ gap: 8 }}>{actions}</div>}
    </div>
  );
}

export type BannerKind = "preview" | "pending";

/** Phase banner — marks a screen as design-preview (future) or backend-pending so the
 *  shell never implies a capability the Sprint-1 backend lacks. */
export function Banner({ kind, children }: { kind: BannerKind; children: ReactNode }) {
  return (
    <div className={cx("s1-banner", kind)}>
      <Icon name={kind === "preview" ? "sparkle" : "warning"} size={16} />
      <span>{children}</span>
    </div>
  );
}

export interface Column<T> {
  header: string;
  render: (row: T) => ReactNode;
  align?: "left" | "center" | "right";
}

export function DataTable<T>({ columns, rows, getKey }: {
  columns: Column<T>[]; rows: T[]; getKey: (row: T) => string;
}) {
  const alignClass = (a?: string) => (a === "right" ? "t-right" : a === "center" ? "t-center" : undefined);
  return (
    <table className="table">
      <thead>
        <tr>{columns.map((c, i) => <th key={i} className={alignClass(c.align)}>{c.header}</th>)}</tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={getKey(row)}>
            {columns.map((c, i) => <td key={i} className={alignClass(c.align)}>{c.render(row)}</td>)}
          </tr>
        ))}
        {rows.length === 0 && (
          <tr><td className="muted" colSpan={columns.length} style={{ textAlign: "center", padding: 24 }}>No data</td></tr>
        )}
      </tbody>
    </table>
  );
}

/** Compact inline sparkline (no chart dependency). */
export function Sparkline({ data, color = "var(--accent)", w = 200, h = 40 }: {
  data: number[]; color?: string; w?: number; h?: number;
}) {
  if (data.length < 2) return null;
  const max = Math.max(...data), min = Math.min(...data), span = max - min || 1;
  const pts = data.map((v, i) => `${(i / (data.length - 1)) * w},${h - ((v - min) / span) * (h - 4) - 2}`);
  return (
    <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} className="stat-spark" preserveAspectRatio="none">
      <polyline points={pts.join(" ")} fill="none" stroke={color} strokeWidth={2} />
    </svg>
  );
}

/** Bytes → human string (TB/GB/MB). */
export function bytes(n: number): string {
  if (n <= 0) return "0";
  const u = ["B", "KB", "MB", "GB", "TB", "PB"];
  const i = Math.min(u.length - 1, Math.floor(Math.log10(n) / 3));
  return (n / 10 ** (i * 3)).toFixed(i >= 3 ? 1 : 0) + " " + u[i];
}

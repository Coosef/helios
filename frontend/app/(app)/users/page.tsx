import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, DataTable, Meter, PageHeader, StatCard, Swatch, cx,
  type Column, type Tone,
} from "@/components/ui";
import { CapacityBar } from "@/components/charts";
import { DonutBreakdown } from "@/components/panels";
import { Icon } from "@/components/icons";
import type { AugmentedUser, Role, RolePrivilege, UserLifecycleState } from "@/lib/types";

const ROLE_TONE: Record<Role, Tone> = { Owner: "ai", Admin: "ok", Operator: "info", Viewer: "muted" };
const STATUS_TONE: Record<UserLifecycleState, Tone> = { active: "ok", pending: "warn", invited: "info", disabled: "muted" };
const DIRECTORY_CHIPS = ["All", "Active", "Pending", "Invited", "Disabled"];

export default async function UsersPage() {
  const o = await getApi().getUsersOverview();
  const statusTotal = o.statusDistribution.reduce((a, x) => a + x.value, 0) || 1;

  const cols: Column<AugmentedUser>[] = [
    { header: "User", render: (u) => <div><div className="cell-strong">{u.name}</div><div className="muted fs-12">{u.email}</div></div> },
    { header: "Role", render: (u) => <Badge tone={ROLE_TONE[u.role]}>{u.role}</Badge> },
    { header: "Tenant", render: (u) => <span className="vcenter" style={{ gap: 8 }}><span style={{ width: 9, height: 9, borderRadius: "50%", background: u.tenantColor, display: "inline-block" }} />{u.tenantName}</span> },
    { header: "Department", render: (u) => <span className="fs-12">{u.department}</span> },
    { header: "Status", render: (u) => <Badge tone={STATUS_TONE[u.status]}>{u.status}</Badge> },
    { header: "MFA", align: "center", render: (u) => <span style={{ color: u.mfa ? "var(--ok)" : "var(--warn)" }}><Icon name={u.mfa ? "check" : "warning"} size={15} /></span> },
    { header: "Last active", align: "right", render: (u) => <span className="mono fs-11">{new Date(u.lastActive).toLocaleString()}</span> },
  ];

  const privCols: Column<RolePrivilege>[] = [
    { header: "Role", render: (p) => <Badge tone={ROLE_TONE[p.role]}>{p.role}</Badge> },
    { header: "Level", align: "right", render: (p) => <span className="mono fs-12">{p.level}</span> },
    { header: "Users", align: "right", render: (p) => <span className="tnum">{p.count}</span> },
    ...(["read", "write", "manage", "admin"] as const).map((cap) => ({
      header: cap.charAt(0).toUpperCase() + cap.slice(1),
      align: "center" as const,
      render: (p: RolePrivilege) => p[cap] ? <Icon name="check" size={15} /> : <span className="muted">—</span>,
    })),
  ];

  return (
    <div className="stack">
      <PageHeader title="User Management" sub={`${o.kpis.total} members · role-based access (UI gating) · mock data`} />
      <Banner kind="pending">User directory is mock data — search, filters and invitations are a design preview; the SaaS backend is the authoritative source of authorization.</Banner>

      {/* KPI strip */}
      <div className="stat-grid">
        <StatCard icon="users" label="Total members" value={o.kpis.total} sub="across all tenants" />
        <StatCard icon="check" tint="var(--ok)" label="Active" value={o.kpis.active} sub={`${o.kpis.mfaPct}% with MFA`} />
        <StatCard icon="shield" tint="var(--ai)" label="Administrators" value={o.kpis.administrators} sub="Owner + Admin" />
        <StatCard icon="warning" tint="var(--warn)" label="Suspended" value={o.kpis.suspended} sub="disabled accounts" />
      </div>

      {/* Role distribution + privilege matrix */}
      <div className="cols-2">
        <Card>
          <CardHead title="Role distribution" sub="Members by role" />
          <div className="hero-split" style={{ marginTop: 8 }}>
            <DonutBreakdown segments={o.roleDistribution} size={140} centerMain={o.kpis.total} centerSub="members" />
            <div className="stack" style={{ width: "100%" }}>
              {o.roleDistribution.map((r) => (
                <div key={r.label} className="between fs-12"><span className="vcenter" style={{ gap: 8 }}><Swatch color={r.color} />{r.label}</span><span className="mono">{r.value}</span></div>
              ))}
            </div>
          </div>
        </Card>

        <Card pad={false}>
          <CardHead title="Privilege matrix" sub="Least-privilege capabilities per role" />
          <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
            <DataTable columns={privCols} rows={o.privileges} getKey={(p) => p.role} />
            <div className="muted fs-11" style={{ marginTop: 10 }}>
              UI gating only — the SaaS backend is the authoritative source of truth for authorization.
            </div>
          </div>
        </Card>
      </div>

      {/* User directory */}
      <Card pad={false}>
        <CardHead title="Directory" sub={`${o.rows.length} members`} />
        <div style={{ padding: "0 var(--pad)" }}>
          <div className="between wrap" style={{ gap: 12, paddingBottom: 12 }}>
            <div className="vcenter wrap" style={{ gap: 6 }} aria-label="Status filter (design preview)">
              {DIRECTORY_CHIPS.map((c, i) => <span key={c} className={cx("chip", i === 0 && "active")} style={{ cursor: "default" }}>{c}</span>)}
            </div>
            <span className="search" aria-label="Search users (design preview)">
              <Icon name="search" size={15} className="muted" />
              <input className="input" placeholder="Search members…" disabled style={{ minWidth: 180 }} />
            </span>
          </div>
        </div>
        <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
          <DataTable columns={cols} rows={o.rows} getKey={(u) => u.id} />
          <div className="vcenter muted fs-11" style={{ gap: 6, marginTop: 10 }}><Icon name="warning" size={13} /> Search and filters are a design preview — not yet wired to the backend.</div>
        </div>
      </Card>

      {/* Invitation workflow */}
      <Card>
        <CardHead title="Enrollment & invitation workflow" sub="Account lifecycle pipeline · illustrative" />
        <div style={{ marginTop: 12 }}>
          <CapacityBar segments={o.statusDistribution.map((s) => ({ pct: Math.round((s.value / statusTotal) * 100), color: s.color, label: s.label }))} />
          <div className="vcenter wrap" style={{ gap: 16, marginTop: 12 }}>
            {o.statusDistribution.map((s) => (
              <span key={s.label} className="vcenter fs-12" style={{ gap: 6 }}><Swatch color={s.color} /><span className="muted">{s.label}</span><span className="mono">{s.value}</span></span>
            ))}
          </div>
          <div className="muted fs-11" style={{ marginTop: 10 }}>Invited and pending are in-flight invitations (not yet accounts); active and disabled reconcile with the directory above.</div>
        </div>
      </Card>

      {/* Activity + org structure */}
      <div className="cols-2">
        <Card pad={false}>
          <CardHead title="User activity" sub="Recent actions · linked to the audit trail" />
          <div className="tl" style={{ margin: "14px var(--pad)" }}>
            {o.activity.map((a) => (
              <div key={a.id} className={cx("tl-item", a.severity)}>
                <div className="between wrap" style={{ gap: 8 }}>
                  <span className="vcenter" style={{ gap: 8 }}><span className="fs-13 fw-6">{a.actor}</span><span className="mono fs-12 muted">{a.action}</span></span>
                  <span className="vcenter" style={{ gap: 8 }}><span className="muted mono fs-11">{new Date(a.at).toLocaleTimeString()}</span><Badge tone="muted">{a.auditId}</Badge></span>
                </div>
                <div className="muted fs-12">{a.detail}</div>
              </div>
            ))}
          </div>
        </Card>

        <Card>
          <CardHead title="Organization structure" sub={`${o.org.tenants.length} tenants · ${o.org.locationCount} locations`} />
          <div className="stack" style={{ marginTop: 12 }}>
            <div className="muted fs-11" style={{ textTransform: "uppercase", letterSpacing: ".06em" }}>Tenants</div>
            {o.org.tenants.map((t) => (
              <div key={t.id} className="between fs-13"><span className="vcenter" style={{ gap: 8 }}><span style={{ width: 9, height: 9, borderRadius: "50%", background: t.color, display: "inline-block" }} />{t.name}</span><span className="muted fs-12">{t.users} users · {t.locations} loc</span></div>
            ))}
            <div className="muted fs-11" style={{ textTransform: "uppercase", letterSpacing: ".06em", marginTop: 6 }}>Departments</div>
            {o.org.departments.map((d) => (
              <div key={d.name} className="between fs-13"><span>{d.name}</span><span className="mono fs-12">{d.users}</span></div>
            ))}
          </div>
        </Card>
      </div>
    </div>
  );
}

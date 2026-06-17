import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, DataTable, Meter, PageHeader, StatCard, bytes, cx,
  type Column,
} from "@/components/ui";
import { AreaChart, CapacityBar } from "@/components/charts";
import { Icon } from "@/components/icons";
import type { CrossTenantAlert, TenantRollup } from "@/lib/types";

const eur = (n: number) => (n >= 1000 ? `€${(n / 1000).toFixed(1)}k` : `€${n}`);

function healthColor(h: number): string {
  return h >= 96 ? "var(--ok)" : h >= 90 ? "var(--accent-2)" : h >= 80 ? "var(--warn)" : "var(--crit)";
}

const ALERT_TONE = { critical: "crit", warning: "warn", info: "info" } as const;

export default async function SuperPage() {
  const s = await getApi().getSuperOverview();
  const totalUsedTB = s.regions.reduce((a, r) => a + r.usedTB, 0);
  const totalCapTB = s.regions.reduce((a, r) => a + r.capacityTB, 0);
  const usedPct = totalCapTB > 0 ? Math.round((totalUsedTB / totalCapTB) * 100) : 0;

  const tenantCols: Column<TenantRollup>[] = [
    { header: "Tenant", render: (t) => (
      <span className="cell-strong vcenter" style={{ gap: 8 }}>
        <span style={{ width: 9, height: 9, borderRadius: "50%", background: t.color, display: "inline-block" }} />{t.name}
      </span>
    ) },
    { header: "Plan", render: (t) => <Badge tone="muted">{t.plan}</Badge> },
    { header: "Devices", align: "right", render: (t) => <span className="tnum">{t.devices}</span> },
    { header: "Health", render: (t) => (
      <div className="vcenter" style={{ gap: 8, minWidth: 120 }}>
        <Meter value={t.health} color={healthColor(t.health)} thin /><span className="mono fs-12">{t.health}</span>
      </div>
    ) },
    { header: "MRR", align: "right", render: (t) => <span className="mono fs-12">{eur(t.mrr)}</span> },
    { header: "Status", render: (t) => <Badge tone={t.status === "active" ? "ok" : "warn"}>{t.status}</Badge> },
  ];

  const alertCols: Column<CrossTenantAlert>[] = [
    { header: "Severity", render: (a) => <Badge tone={ALERT_TONE[a.severity]}>{a.severity}</Badge> },
    { header: "Issue", render: (a) => <span className="cell-strong">{a.title}</span> },
    { header: "Source", render: (a) => <span className="fs-12">{a.source}</span> },
    { header: "Category", render: (a) => <Badge tone="muted">{a.category}</Badge> },
    { header: "When", align: "right", render: (a) => <span className="muted fs-11">{a.at}</span> },
  ];

  return (
    <div className="stack">
      <PageHeader
        title="Global Overview"
        sub={`Platform-wide health across ${s.kpis.tenants} tenants · ${s.kpis.managedDevices} managed devices · ${s.regions.length} regions · mock`}
        actions={<>
          <div className="seg" aria-label="Period (design preview)">
            <button type="button" disabled>24h</button>
            <button type="button" className="active" disabled>7d</button>
            <button type="button" disabled>30d</button>
          </div>
          <Badge tone="info" lg><Icon name="globe" size={14} /> Control Plane</Badge>
        </>}
      />

      <Banner kind="preview">
        Super-admin control plane is a shell — the backend lands in Sprint 2+. KPIs, tenant rollups and
        infrastructure below are illustrative mock data.
      </Banner>

      {/* Cross-tenant KPI grid */}
      <div className="stat-grid">
        <StatCard icon="tenants" label="Active tenants" value={s.kpis.tenants} sub="0 suspended" />
        <StatCard icon="devices" tint="var(--ok)" label="Managed devices" value={s.kpis.managedDevices} spark={s.deviceTrend} />
        <StatCard icon="target" tint="var(--accent-2)" label="Platform MRR" value={eur(s.kpis.mrr)} sub={`ARR ${eur(s.kpis.arr)}`} />
        <StatCard icon="activity" tint="var(--info)" label="Global SLA" value={`${s.kpis.slaPct}%`} sub="30-day uptime · illustrative" />
      </div>

      {/* Tenant fleet + infrastructure */}
      <div className="cols-2">
        <Card pad={false}>
          <CardHead title="Tenant fleet" sub="Health & consumption per tenant" />
          <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
            <DataTable columns={tenantCols} rows={s.tenants} getKey={(t) => t.id} />
          </div>
        </Card>

        <Card>
          <CardHead title="Infrastructure" sub={`${s.regions.length} regions · ${totalUsedTB}/${totalCapTB} TB`} />
          <div className="stack" style={{ marginTop: 12 }}>
            {s.regions.map((r) => {
              const pct = r.capacityTB > 0 ? Math.round((r.usedTB / r.capacityTB) * 100) : 0;
              return (
                <div key={r.name} className="stack" style={{ gap: 6 }}>
                  <div className="between wrap fs-12">
                    <span className="vcenter" style={{ gap: 8 }}>
                      <span className={cx("bdot-pulse")} style={{ width: 8, height: 8, borderRadius: "50%", background: r.tint, display: "inline-block" }} />
                      <span className="fw-6">{r.name}</span><Badge tone="muted">{r.role}</Badge>
                    </span>
                    <span className="muted mono">{r.uptimePct}% · {r.nodes} nodes</span>
                  </div>
                  <Meter value={pct} color={r.tint} thin />
                  <div className="muted fs-11">{r.usedTB} / {r.capacityTB} TB used</div>
                </div>
              );
            })}
            <div style={{ marginTop: 4 }}>
              <div className="between fs-12" style={{ marginBottom: 6 }}>
                <span className="muted">Aggregate capacity</span><span className="mono">{usedPct}% used</span>
              </div>
              <CapacityBar segments={[{ pct: usedPct, color: "var(--accent)", label: `${totalUsedTB} TB used` }]} />
            </div>
          </div>
        </Card>
      </div>

      {/* Platform growth */}
      <Card>
        <CardHead title="Platform growth" sub="Managed devices · last 14 days (illustrative)" />
        <div style={{ marginTop: 12 }}>
          <AreaChart series={[{ data: s.deviceTrend, color: "var(--accent)", name: "Managed devices" }]} labels={s.trendLabels} h={200} />
        </div>
      </Card>

      {/* Cross-tenant operational alerts */}
      <Card pad={false}>
        <CardHead title="Cross-tenant alerts" sub="Platform risk & operational overview · mock" right={<Badge tone="crit">{s.kpis.openCriticalAlerts} critical</Badge>} />
        <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
          <DataTable columns={alertCols} rows={s.crossTenantAlerts} getKey={(a) => a.id} />
        </div>
      </Card>
    </div>
  );
}

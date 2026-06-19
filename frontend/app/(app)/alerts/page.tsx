import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, DataTable, Meter, PageHeader, StatCard, StatusBadge, Swatch, cx,
  type Column, type Tone,
} from "@/components/ui";
import { AreaChart, CapacityBar } from "@/components/charts";
import { DonutBreakdown } from "@/components/panels";
import { Icon } from "@/components/icons";
import type { AlertLifecycleState, AugmentedAlert } from "@/lib/types";

const LIFECYCLE_TONE: Record<AlertLifecycleState, Tone> = {
  OPEN: "crit", DEGRADED: "warn", RECOVERING: "info", CLOSED: "ok", SUPPRESSED: "muted",
};

export default async function AlertsPage() {
  const o = await getApi().getAlertsOverview();
  const triageTotal = o.kpis.openTotal + o.kpis.acknowledged + o.kpis.resolved || 1;
  const lifecycleTotal = o.lifecycleDistribution.reduce((a, s) => a + s.value, 0) || 1;

  const cols: Column<AugmentedAlert>[] = [
    { header: "Severity", render: (a) => <StatusBadge status={a.severity} /> },
    { header: "State", render: (a) => <Badge tone={LIFECYCLE_TONE[a.state]}>{a.state}</Badge> },
    { header: "Alert", render: (a) => <div><div className="cell-strong">{a.title}</div><div className="muted fs-12">{a.detail}</div></div> },
    { header: "Source", render: (a) => <span className="mono fs-12">{a.source}</span> },
    { header: "Correlation", render: (a) => <span className="mono fs-11 muted">{a.correlationId}</span> },
    { header: "×", align: "right", render: (a) => <span className="tnum">{a.occurrences}</span> },
    { header: "When", align: "right", render: (a) => <span className="mono fs-11">{new Date(a.at).toLocaleTimeString()}</span> },
  ];

  return (
    <div className="stack">
      <PageHeader title="Alerts" sub={`${o.rows.length} in the current sample · ${o.kpis.openTotal} open · mock data`} />
      <Banner kind="pending">Alerting backend is not built yet — this console is mock data with no real-time updates; controls are display-only.</Banner>

      {/* KPI strip */}
      <div className="stat-grid">
        <StatCard icon="alerts" tint="var(--crit)" label="Open critical" value={o.kpis.openCritical} sub="needs response" />
        <StatCard icon="bell" tint="var(--warn)" label="Open total" value={o.kpis.openTotal} sub="unacknowledged" />
        <StatCard icon="check" tint="var(--ok)" label="Acknowledged" value={o.kpis.acknowledged} sub="in triage" />
        <StatCard icon="clock" tint="var(--info)" label="MTTR" value={o.kpis.mttr} sub="mean time to resolve · illustrative" />
      </div>

      {/* Severity + lifecycle distribution */}
      <div className="cols-2">
        <Card>
          <CardHead title="Severity distribution" sub="By severity" />
          <DonutBreakdown segments={o.severityDistribution} size={140} centerMain={o.rows.length} centerSub="alerts" />
        </Card>

        <Card>
          <CardHead title="Lifecycle distribution" sub="OPEN · DEGRADED · RECOVERING · CLOSED · SUPPRESSED" />
          <div style={{ marginTop: 14 }}>
            <CapacityBar segments={o.lifecycleDistribution.map((s) => ({ pct: Math.round((s.value / lifecycleTotal) * 100), color: s.color, label: s.label }))} />
            <div className="vcenter wrap" style={{ gap: 14, marginTop: 12 }}>
              {o.lifecycleDistribution.map((s) => (
                <span key={s.label} className="vcenter fs-12" style={{ gap: 6 }}><Swatch color={s.color} /><span className="muted">{s.label}</span><span className="mono">{s.value}</span></span>
              ))}
            </div>
          </div>
        </Card>
      </div>

      {/* Triage funnel + trend */}
      <div className="cols-2">
        <Card>
          <CardHead title="Triage" sub="Open · acknowledged · resolved" />
          <div className="stack" style={{ marginTop: 12, gap: 14 }}>
            {([["Open", o.kpis.openTotal, "var(--crit)"], ["Acknowledged", o.kpis.acknowledged, "var(--warn)"], ["Resolved", o.kpis.resolved, "var(--ok)"]] as const).map(([label, val, color]) => (
              <div key={label} className="stack" style={{ gap: 6 }}>
                <div className="between fs-12"><span className="muted">{label}</span><span className="mono">{val}</span></div>
                <Meter value={Math.round((val / triageTotal) * 100)} color={color} />
              </div>
            ))}
          </div>
        </Card>

        <Card>
          <CardHead title="Alert trends" sub="Opened vs resolved · 14 days · illustrative" right={
            <span className="vcenter fs-12" style={{ gap: 12 }}>
              <span className="vcenter" style={{ gap: 6 }}><Swatch color="var(--crit)" />Opened</span>
              <span className="vcenter" style={{ gap: 6 }}><Swatch color="var(--ok)" />Resolved</span>
            </span>
          } />
          <div style={{ marginTop: 12 }}>
            <AreaChart series={[{ data: o.trend.opened, color: "var(--crit)", name: "Opened" }, { data: o.trend.resolved, color: "var(--ok)", name: "Resolved" }]} labels={o.trend.labels} h={190} />
          </div>
        </Card>
      </div>

      {/* Sources + correlation groups */}
      <div className="cols-2">
        <Card>
          <CardHead title="Alert sources" sub="Top sources by host & category" />
          <div className="stack" style={{ marginTop: 12 }}>
            {o.sources.map((s) => (
              <div key={s.source} className="stack" style={{ gap: 6 }}>
                <div className="between fs-12"><span className="vcenter" style={{ gap: 8 }}><Swatch color={s.color} /><span className="mono">{s.source}</span><Badge tone="muted">{s.category}</Badge></span><span className="mono">{s.count} · {s.pct}%</span></div>
                <Meter value={s.pct} color={s.color} thin />
              </div>
            ))}
          </div>
        </Card>

        <Card>
          <CardHead title="Correlation groups" sub="Keyed by correlation_id · group_wait & bounce_window are display-only" />
          <div className="stack" style={{ marginTop: 12 }}>
            {o.correlationGroups.map((g) => (
              <div key={g.correlationId} style={{ border: "1px solid var(--border)", borderRadius: 10, padding: "12px 14px" }}>
                <div className="between wrap" style={{ gap: 8, marginBottom: 8 }}>
                  <span className="vcenter" style={{ gap: 8 }}><span className="fw-6 fs-13">{g.title}</span><Badge tone={LIFECYCLE_TONE[g.state]}>{g.state}</Badge></span>
                  <span className="mono fs-11 muted">{g.correlationId}</span>
                </div>
                <div className="muted fs-12" style={{ marginBottom: 8 }}>{g.rootCause} · {g.members} alert{g.members === 1 ? "" : "s"}</div>
                <dl className="kv">
                  <dt>group_wait</dt><dd className="mono">{g.groupWaitSec}s</dd>
                  <dt>bounce_window</dt><dd className="mono">{g.bounceWindowSec}s</dd>
                </dl>
              </div>
            ))}
          </div>
        </Card>
      </div>

      {/* Lifecycle timeline + response times */}
      <div className="cols-2">
        <Card pad={false}>
          <CardHead title="Lifecycle timeline" sub="Correlated alert events · illustrative" />
          <div className="tl" style={{ margin: "14px var(--pad)" }}>
            {o.timeline.map((t) => (
              <div key={t.id} className={cx("tl-item", t.severity)}>
                <div className="between wrap" style={{ gap: 8 }}><span className="fs-13 fw-6">{t.label}</span><span className="muted mono fs-11">{new Date(t.at).toLocaleTimeString()}</span></div>
                <div className="muted fs-12">{t.detail}</div>
              </div>
            ))}
          </div>
        </Card>

        <Card>
          <CardHead title="Response times" sub="MTTD · MTTA · MTTR · illustrative" />
          <div className="stat-grid" style={{ marginTop: 12, gridTemplateColumns: "repeat(auto-fit, minmax(120px, 1fr))" }}>
            <div className="card card-pad"><div className="muted fs-12">MTTD</div><div className="display tnum" style={{ fontSize: 22, marginTop: 4 }}>{o.kpis.mttd}</div><div className="muted fs-11">time to detect</div></div>
            <div className="card card-pad"><div className="muted fs-12">MTTA</div><div className="display tnum" style={{ fontSize: 22, marginTop: 4 }}>{o.kpis.mtta}</div><div className="muted fs-11">time to ack</div></div>
            <div className="card card-pad"><div className="muted fs-12">MTTR</div><div className="display tnum" style={{ fontSize: 22, marginTop: 4 }}>{o.kpis.mttr}</div><div className="muted fs-11">time to resolve</div></div>
          </div>
        </Card>
      </div>

      {/* Suppression + escalation */}
      <div className="cols-2">
        <Card pad={false}>
          <CardHead title="Suppression overview" sub="Active mute rules (SUPPRESSED) · display only" />
          <div className="list-rows">
            {o.suppression.map((s) => (
              <div key={s.id} className="between wrap" style={{ gap: 8 }}>
                <div><div className="fs-13 fw-6 mono">{s.scope}</div><div className="muted fs-12">{s.reason}</div></div>
                <span className="vcenter" style={{ gap: 8 }}><Badge tone="muted">{s.window}</Badge><Badge tone={s.active ? "info" : "muted"}>{s.active ? "active" : "off"}</Badge></span>
              </div>
            ))}
          </div>
        </Card>

        <Card pad={false}>
          <CardHead title="Escalation overview" sub="Policy tiers for unacknowledged alerts · display only" />
          <div className="tl" style={{ margin: "14px var(--pad)" }}>
            {o.escalation.map((t) => (
              <div key={t.tier} className={cx("tl-item", t.state === "done" ? "crit" : t.state === "current" ? "warn" : undefined)}>
                <div className="between wrap" style={{ gap: 8 }}><span className="fs-13 fw-6">{t.tier} · {t.action}</span><span className="muted mono fs-11">{t.afterLabel}</span></div>
                <div className="muted fs-12">via {t.channel}</div>
              </div>
            ))}
          </div>
        </Card>
      </div>

      {/* Full alerts table */}
      <Card pad={false}>
        <CardHead title="All alerts" sub={`${o.rows.length} in the current sample · mock data`} />
        <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
          <DataTable columns={cols} rows={o.rows} getKey={(a) => a.id} />
        </div>
      </Card>
    </div>
  );
}

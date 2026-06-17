import Link from "next/link";
import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, DataTable, Meter, PageHeader, StatCard,
  StatusBadge, bytes, type Column,
} from "@/components/ui";
import { AreaChart } from "@/components/charts";
import { DonutBreakdown, ResilienceHero } from "@/components/panels";
import { Icon } from "@/components/icons";
import type { Alert } from "@/lib/types";

export default async function DashboardPage() {
  const api = getApi();
  const [d, insights, alerts, locations] = await Promise.all([
    api.getDashboard(),
    api.getDashboardInsights(),
    api.getAlerts(),
    api.getLocations(),
  ]);
  const { resilience, trend, activity, securityPosture } = insights;
  const open = alerts.filter((a) => !a.acknowledged).slice(0, 5);
  const activityTotal = activity.reduce((s, a) => s + a.value, 0);
  const successRate = Math.round((d.jobsSucceeded24h / (d.jobsSucceeded24h + d.jobsFailed24h)) * 100);

  const cols: Column<Alert>[] = [
    { header: "Severity", render: (a) => <StatusBadge status={a.severity} /> },
    { header: "Alert", render: (a) => <span className="cell-strong">{a.title}</span> },
    { header: "Detail", render: (a) => <span className="muted fs-12">{a.detail}</span> },
    { header: "When", align: "right", render: (a) => <span className="mono fs-11">{new Date(a.at).toLocaleTimeString()}</span> },
  ];

  return (
    <div className="stack">
      <PageHeader title="Dashboard" sub="Fleet protection overview · mock data" />

      {/* Hero: resilience score + 24h activity distribution */}
      <div className="cols-2">
        <Card>
          <CardHead title="Backup resilience" sub="Composite protection health · illustrative" right={<Badge tone="ai">Beta</Badge>} />
          <ResilienceHero resilience={resilience} />
        </Card>

        <Card>
          <CardHead title="24h activity" sub="Job outcomes · rolling" />
          <DonutBreakdown segments={activity} centerMain={activityTotal} centerSub="jobs" />
        </Card>
      </div>

      {/* KPI grid */}
      <div className="stat-grid">
        <StatCard icon="devices" label="Devices online" value={`${d.devicesOnline}/${d.devicesTotal}`} sub={`${d.devicesDegraded} degraded`} />
        <StatCard icon="jobs" tint="var(--ok)" label="Jobs succeeded (24h)" value={d.jobsSucceeded24h} sub={`${successRate}% success`} />
        <StatCard icon="storage" tint="var(--accent-2)" label="Protected data" value={bytes(d.protectedBytes)} spark={trend.protectedTB} />
        <StatCard icon="alerts" tint="var(--warn)" label="Open alerts" value={d.openAlerts} sub="needs attention" />
      </div>

      {/* Protected-data trend */}
      <Card>
        <CardHead title="Protected data trend" sub="Last 14 days · illustrative" right={<span className="muted fs-12">TB protected</span>} />
        <div style={{ marginTop: 12 }}>
          <AreaChart
            series={[{ data: trend.protectedTB, color: "var(--accent)", name: "Protected TB" }]}
            labels={trend.labels}
            h={200}
          />
        </div>
      </Card>

      {/* Site health + security posture */}
      <div className="cols-2">
        <Card pad={false}>
          <CardHead title="Site health" sub="Protection score by location" />
          <div className="list-rows">
            {locations.map((loc) => {
              const color = loc.health >= 95 ? "var(--ok)" : loc.health >= 85 ? "var(--warn)" : "var(--crit)";
              return (
                <div key={loc.id} className="stack" style={{ gap: 6 }}>
                  <div className="between fs-13">
                    <span className="cell-strong">{loc.name}</span>
                    <span className="muted fs-12">{loc.deviceCount} devices · <span className="mono">{loc.health}</span></span>
                  </div>
                  <Meter value={loc.health} color={color} thin />
                </div>
              );
            })}
          </div>
        </Card>

        <Card pad={false}>
          <CardHead title="Security posture" sub="Sprint-1 controls" right={<Badge tone="ok">{securityPosture.filter((s) => s.ok).length}/{securityPosture.length} OK</Badge>} />
          <div className="list-rows">
            {securityPosture.map((item) => (
              <div key={item.label} className="vcenter" style={{ gap: 12, alignItems: "flex-start" }}>
                <span style={{ color: item.ok ? "var(--ok)" : "var(--warn)", marginTop: 2 }}>
                  <Icon name={item.ok ? "check" : "warning"} size={16} />
                </span>
                <div>
                  <div className="fs-13 fw-6">{item.label}</div>
                  <div className="muted fs-12">{item.detail}</div>
                </div>
              </div>
            ))}
          </div>
        </Card>
      </div>

      {/* Recent alerts + AI insight preview */}
      <div className="cols-2">
        <Card pad={false}>
          <CardHead title="Recent alerts" sub="Unacknowledged" right={<Link className="btn btn-sm" href="/alerts">View all</Link>} />
          <div style={{ padding: "0 var(--pad) var(--pad)" }}>
            <DataTable columns={cols} rows={open} getKey={(a) => a.id} />
          </div>
        </Card>

        <Card>
          <CardHead title="Helios AI insights" sub="Future preview" right={<Badge tone="ai">Preview</Badge>} />
          <div style={{ marginTop: 12 }} className="stack">
            <Banner kind="preview">AI-generated insights are a design preview — not backed by a live model yet.</Banner>
            <div className="vcenter" style={{ gap: 10, alignItems: "flex-start" }}>
              <span style={{ color: "var(--ai)", marginTop: 2 }}><Icon name="sparkle" size={16} /></span>
              <div className="fs-13">belek-fs-01 has missed its last 2 incremental windows — recovery point is drifting. Suggested action: investigate Belek Vault capacity.</div>
            </div>
            <div className="vcenter" style={{ gap: 10, alignItems: "flex-start" }}>
              <span style={{ color: "var(--ai)", marginTop: 2 }}><Icon name="sparkle" size={16} /></span>
              <div className="fs-13">Protected volume is trending up ~3%/week; at current pace Helios Cloud · eu-central reaches 80% in ~6 weeks.</div>
            </div>
          </div>
        </Card>
      </div>
    </div>
  );
}

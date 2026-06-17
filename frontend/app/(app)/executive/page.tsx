import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, PageHeader, StatCard, StatusBadge, bytes,
} from "@/components/ui";
import { AreaChart } from "@/components/charts";
import { DonutBreakdown, ResilienceHero } from "@/components/panels";
import { Icon } from "@/components/icons";

const usd = (n: number) =>
  n >= 1_000_000 ? `$${(n / 1_000_000).toFixed(1)}M` : `$${Math.round(n / 1000)}K`;

export default async function ExecutivePage() {
  const api = getApi();
  const [summary, fleet, d] = await Promise.all([
    api.getExecutiveSummary(),
    api.getDashboardInsights().then((i) => i.fleet),
    api.getDashboard(),
  ]);
  const { resilience, trend, kpis, financials, topRisks } = summary;
  const fleetTotal = fleet.online + fleet.warning + fleet.offline;
  const fleetSegments = [
    { value: fleet.online, color: "var(--ok)", label: "Online" },
    { value: fleet.warning, color: "var(--warn)", label: "Warning" },
    { value: fleet.offline, color: "var(--crit)", label: "Offline" },
  ];

  return (
    <div className="stack">
      <PageHeader title="Executive Overview" sub="Fleet protection at a glance · mock data" />

      <Banner kind="preview">Executive analytics are a design preview — figures are illustrative mock data.</Banner>

      {/* Resilience hero + fleet health */}
      <div className="cols-2">
        <Card>
          <CardHead title="Backup resilience" sub="Composite protection health" right={<Badge tone="ai">Beta</Badge>} />
          <ResilienceHero resilience={resilience} gaugeSize={180} />
        </Card>

        <Card>
          <CardHead title="Fleet health" sub={`${fleetTotal} managed devices`} />
          <DonutBreakdown
            segments={fleetSegments}
            size={150}
            centerMain={`${Math.round((fleet.online / fleetTotal) * 100)}%`}
            centerSub="online"
            centerColor="var(--ok)"
          />
        </Card>
      </div>

      {/* 6-KPI grid */}
      <div className="stat-grid">
        <StatCard icon="shield" label="Protected assets" value={kpis.protectedAssets} sub="devices in scope" />
        <StatCard icon="storage" tint="var(--accent-2)" label="Protected data" value={bytes(d.protectedBytes)} spark={trend.protectedTB} />
        <StatCard icon="jobs" tint="var(--ok)" label="Backup success rate" value={`${kpis.successRate}%`} sub="trailing 30 days" />
        <StatCard icon="shield" tint="var(--info)" label="Compliance score" value={`${kpis.complianceScore}/100`} sub="3-2-1 + encryption" />
        <StatCard icon="restore" tint="var(--ai)" label="Restore readiness" value={`${kpis.restoreReadiness}/100`} sub="last verified recovery" />
        <StatCard icon="clock" tint="var(--warn)" label="Storage runway" value={`${kpis.storageRunwayDays}d`} sub="at current growth" />
      </div>

      {/* Trend */}
      <Card>
        <CardHead title="Protection trend" sub="Protected volume vs. resilience score · last 14 days" right={
          <span className="vcenter fs-12" style={{ gap: 14 }}>
            <span className="vcenter" style={{ gap: 6 }}><span style={{ width: 10, height: 3, background: "var(--accent)", display: "inline-block" }} />Protected TB</span>
            <span className="vcenter" style={{ gap: 6 }}><span style={{ width: 10, height: 3, background: "var(--ai)", display: "inline-block" }} />Resilience</span>
          </span>
        } />
        <div style={{ marginTop: 12 }}>
          <AreaChart
            series={[
              { data: trend.resilienceScore, color: "var(--ai)", name: "Resilience" },
              { data: trend.protectedTB.map((v) => v * 14), color: "var(--accent)", name: "Protected TB" },
            ]}
            labels={trend.labels}
            yMax={100}
            h={220}
          />
        </div>
      </Card>

      {/* Top risks + financial / ROI */}
      <div className="cols-2">
        <Card pad={false}>
          <CardHead title="Top risks" sub="Highest-impact open items" right={<Badge tone="warn">{topRisks.length}</Badge>} />
          <div className="list-rows">
            {topRisks.map((r) => (
              <div key={r.id} className="between" style={{ gap: 12, alignItems: "flex-start" }}>
                <div className="vcenter" style={{ gap: 12, alignItems: "flex-start" }}>
                  <StatusBadge status={r.severity} />
                  <div>
                    <div className="fs-13 fw-6">{r.title}</div>
                    <div className="muted fs-12">{r.impact}</div>
                  </div>
                </div>
                <span className="muted fs-11" style={{ whiteSpace: "nowrap" }}>{r.owner}</span>
              </div>
            ))}
          </div>
        </Card>

        <Card>
          <CardHead title="Business value" sub="Estimated · illustrative" right={<Badge tone="ai">Preview</Badge>} />
          <div className="stack" style={{ marginTop: 12 }}>
            <div className="between">
              <span className="vcenter" style={{ gap: 10 }}>
                <span style={{ color: "var(--ok)" }}><Icon name="storage" size={16} /></span>
                <span className="muted fs-13">Saved by dedup &amp; compression</span>
              </span>
              <span className="display" style={{ fontWeight: 600 }}>{usd(financials.savedByDedupUsd)}</span>
            </div>
            <div className="between">
              <span className="vcenter" style={{ gap: 10 }}>
                <span style={{ color: "var(--accent)" }}><Icon name="activity" size={16} /></span>
                <span className="muted fs-13">Projected annual storage cost</span>
              </span>
              <span className="display" style={{ fontWeight: 600 }}>{usd(financials.projectedAnnualUsd)}</span>
            </div>
            <div className="between">
              <span className="vcenter" style={{ gap: 10 }}>
                <span style={{ color: "var(--ai)" }}><Icon name="shield" size={16} /></span>
                <span className="muted fs-13">Data-at-risk exposure avoided</span>
              </span>
              <span className="display" style={{ fontWeight: 600 }}>{usd(financials.dataAtRiskAvoidedUsd)}</span>
            </div>
            <div className="muted fs-11" style={{ marginTop: 4 }}>
              ROI figures are modeling placeholders for the future analytics engine.
            </div>
          </div>
        </Card>
      </div>
    </div>
  );
}

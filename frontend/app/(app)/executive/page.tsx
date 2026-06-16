import { getApi } from "@/lib/api";
import { Banner, Card, CardHead, Meter, PageHeader, Sparkline, StatCard, bytes } from "@/components/ui";

export default async function ExecutivePage() {
  const d = await getApi().getDashboard();
  const trend = [12, 18, 15, 22, 30, 28, 35];

  const successRate = Math.round((d.jobsSucceeded24h / (d.jobsSucceeded24h + d.jobsFailed24h)) * 100);
  const onlinePct = Math.round((d.devicesOnline / d.devicesTotal) * 100);

  return (
    <div className="stack">
      <PageHeader title="Executive Overview" sub="Fleet protection at a glance · mock data" />

      <Banner kind="preview">Executive analytics are a design preview.</Banner>

      <div className="stat-grid">
        <StatCard icon="devices" label="Devices online" value={`${d.devicesOnline}/${d.devicesTotal}`} sub={`${onlinePct}% online · ${d.devicesDegraded} degraded`} />
        <StatCard icon="jobs" tint="var(--ok)" label="Jobs succeeded (24h)" value={d.jobsSucceeded24h} sub={`${d.jobsFailed24h} failed`} />
        <StatCard icon="storage" tint="var(--accent-2)" label="Protected data" value={bytes(d.protectedBytes)} sub="across all targets" />
        <StatCard icon="alerts" tint="var(--warn)" label="Open alerts" value={d.openAlerts} sub="needs attention" />
      </div>

      <div className="cols-2">
        <Card>
          <CardHead title="Protected data trend (illustrative)" sub="Last 7 intervals" />
          <div style={{ marginTop: "var(--pad)" }}>
            <Sparkline data={trend} w={640} h={120} />
          </div>
        </Card>

        <Card>
          <CardHead title="Fleet summary" sub="Rolling 24h" />
          <div className="stack" style={{ marginTop: 14 }}>
            <div>
              <div className="between fs-12" style={{ marginBottom: 6 }}><span className="muted">Backup success rate</span><span className="mono">{successRate}%</span></div>
              <Meter value={successRate} color="var(--ok)" />
            </div>
            <div>
              <div className="between fs-12" style={{ marginBottom: 6 }}><span className="muted">Devices online</span><span className="mono">{onlinePct}%</span></div>
              <Meter value={onlinePct} />
            </div>
            <div className="between fs-12"><span className="muted">Degraded agents</span><span className="mono">{d.devicesDegraded}</span></div>
            <div className="between fs-12"><span className="muted">Open alerts</span><span className="mono">{d.openAlerts}</span></div>
          </div>
        </Card>
      </div>
    </div>
  );
}

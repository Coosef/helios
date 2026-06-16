import { getApi } from "@/lib/api";
import { Banner, Card, CardHead, PageHeader, Sparkline, StatCard, bytes } from "@/components/ui";

export default async function ExecutivePage() {
  const d = await getApi().getDashboard();
  const trend = [12, 18, 15, 22, 30, 28, 35];

  return (
    <>
      <PageHeader title="Executive Overview" sub="Fleet protection at a glance · mock data" />

      <Banner kind="preview">Executive analytics are a design preview.</Banner>

      <div className="grid-auto" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))" }}>
        <StatCard icon="devices" label="Devices online" value={`${d.devicesOnline}/${d.devicesTotal}`} sub={`${d.devicesDegraded} degraded`} />
        <StatCard icon="jobs" tint="var(--ok)" label="Jobs succeeded (24h)" value={d.jobsSucceeded24h} sub={`${d.jobsFailed24h} failed`} />
        <StatCard icon="storage" tint="var(--accent-2)" label="Protected data" value={bytes(d.protectedBytes)} sub="across all targets" />
        <StatCard icon="alerts" tint="var(--warn)" label="Open alerts" value={d.openAlerts} sub="needs attention" />
      </div>

      <Card>
        <CardHead title="Protected data trend (illustrative)" sub="Last 7 intervals" />
        <div style={{ marginTop: "var(--pad)" }}>
          <Sparkline data={trend} />
        </div>
      </Card>
    </>
  );
}

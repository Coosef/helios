import { getApi } from "@/lib/api";
import { Banner, Card, CardHead, PageHeader, StatCard, bytes } from "@/components/ui";

export default async function SuperMonitoringPage() {
  const d = await getApi().getDashboard();

  return (
    <>
      <PageHeader title="Global Monitoring" sub="Cross-tenant fleet health" />

      <Banner kind="pending">Global monitoring backend is not built yet — design preview.</Banner>

      <div className="grid-auto" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))" }}>
        <StatCard icon="devices" label="Devices online" value={`${d.devicesOnline}/${d.devicesTotal}`} sub={`${d.devicesDegraded} degraded`} />
        <StatCard icon="jobs" tint="var(--ok)" label="Jobs succeeded (24h)" value={d.jobsSucceeded24h} sub={`${d.jobsFailed24h} failed`} />
        <StatCard icon="storage" tint="var(--accent-2)" label="Protected data" value={bytes(d.protectedBytes)} sub="across all targets" />
        <StatCard icon="alerts" tint="var(--warn)" label="Open alerts" value={d.openAlerts} sub="needs attention" />
      </div>

      <Card>
        <CardHead title="Cross-tenant telemetry" sub="Roadmap" />
        <p className="muted fs-13">
          These figures reflect a single tenant view today. Cross-tenant telemetry aggregation —
          fleet-wide health, per-tenant rollups, and global SLA tracking — lands in Sprint 2+.
        </p>
      </Card>
    </>
  );
}

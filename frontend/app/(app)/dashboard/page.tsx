import Link from "next/link";
import { getApi } from "@/lib/api";
import { Card, CardHead, DataTable, PageHeader, StatCard, StatusBadge, bytes, type Column } from "@/components/ui";
import type { Alert } from "@/lib/types";

export default async function DashboardPage() {
  const api = getApi();
  const [d, alerts] = await Promise.all([api.getDashboard(), api.getAlerts()]);
  const open = alerts.filter((a) => !a.acknowledged).slice(0, 5);

  const cols: Column<Alert>[] = [
    { header: "Severity", render: (a) => <StatusBadge status={a.severity} /> },
    { header: "Alert", render: (a) => <span className="cell-strong">{a.title}</span> },
    { header: "Detail", render: (a) => <span className="muted fs-12">{a.detail}</span> },
    { header: "When", align: "right", render: (a) => <span className="mono fs-11">{new Date(a.at).toLocaleTimeString()}</span> },
  ];

  return (
    <>
      <PageHeader title="Dashboard" sub="Fleet protection overview · mock data" />

      <div className="grid-auto" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))" }}>
        <StatCard icon="devices" label="Devices online" value={`${d.devicesOnline}/${d.devicesTotal}`} sub={`${d.devicesDegraded} degraded`} />
        <StatCard icon="jobs" tint="var(--ok)" label="Jobs succeeded (24h)" value={d.jobsSucceeded24h} sub={`${d.jobsFailed24h} failed`} />
        <StatCard icon="storage" tint="var(--accent-2)" label="Protected data" value={bytes(d.protectedBytes)} sub="across all targets" />
        <StatCard icon="alerts" tint="var(--warn)" label="Open alerts" value={d.openAlerts} sub="needs attention" />
      </div>

      <Card className="grid-auto" pad={false}>
        <CardHead title="Recent alerts" sub="Unacknowledged" right={<Link className="btn btn-sm" href="/alerts">View all</Link>} />
        <div style={{ padding: "0 var(--pad) var(--pad)" }}>
          <DataTable columns={cols} rows={open} getKey={(a) => a.id} />
        </div>
      </Card>
    </>
  );
}

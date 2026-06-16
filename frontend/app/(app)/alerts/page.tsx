import { getApi } from "@/lib/api";
import { Badge, Banner, Card, DataTable, PageHeader, StatusBadge, type Column } from "@/components/ui";
import type { Alert } from "@/lib/types";

export default async function AlertsPage() {
  const alerts = await getApi().getAlerts();
  const open = alerts.filter((a) => !a.acknowledged).length;

  const cols: Column<Alert>[] = [
    { header: "Severity", render: (a) => <StatusBadge status={a.severity} /> },
    { header: "Title", render: (a) => <span className="cell-strong">{a.title}</span> },
    { header: "Detail", render: (a) => <span className="muted fs-12">{a.detail}</span> },
    { header: "When", render: (a) => <span className="mono fs-11">{new Date(a.at).toLocaleString()}</span> },
    {
      header: "Ack",
      align: "right",
      render: (a) =>
        a.acknowledged ? <Badge tone="ok">ack</Badge> : <Badge tone="warn">open</Badge>,
    },
  ];

  return (
    <>
      <PageHeader title="Alerts" sub={`${alerts.length} total · ${open} open`} />
      <Banner kind="pending">Alerting backend is not built yet — mock data.</Banner>
      <Card pad={false}>
        <div style={{ padding: "var(--pad)" }}>
          <DataTable columns={cols} rows={alerts} getKey={(a) => a.id} />
        </div>
      </Card>
    </>
  );
}

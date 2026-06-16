import { getApi } from "@/lib/api";
import type { Tenant } from "@/lib/types";
import {
  PageHeader,
  Banner,
  StatCard,
  Card,
  CardHead,
  DataTable,
  Badge,
  type Column,
} from "@/components/ui";

export default async function Page() {
  const [tenants, dashboard] = await Promise.all([
    getApi().getTenants(),
    getApi().getDashboard(),
  ]);

  const columns: Column<Tenant>[] = [
    {
      header: "Name",
      render: (t) => (
        <span className="cell-strong vcenter" style={{ gap: 8 }}>
          <span
            style={{
              width: 9,
              height: 9,
              borderRadius: "50%",
              background: t.color,
              display: "inline-block",
            }}
          />
          {t.name}
        </span>
      ),
    },
    { header: "Plan", render: (t) => <Badge tone="info">{t.plan}</Badge> },
  ];

  return (
    <>
      <PageHeader title="Global Overview" sub="Control plane · mock" />
      <Banner kind="preview">
        Super-admin control plane is a shell — the backend lands in Sprint 2+.
      </Banner>

      <div className="grid-auto" style={{ marginTop: 16 }}>
        <StatCard icon="tenants" label="Tenants" value={tenants.length} />
        <StatCard
          icon="devices"
          tint="var(--ok)"
          label="Total devices"
          value={dashboard.devicesTotal}
        />
        <StatCard
          icon="alerts"
          tint="var(--warn)"
          label="Open alerts"
          value={dashboard.openAlerts}
        />
      </div>

      <Card className="card" pad={false}>
        <CardHead title="Tenants" sub={`${tenants.length} active`} />
        <DataTable columns={columns} rows={tenants} getKey={(t) => t.id} />
      </Card>
    </>
  );
}

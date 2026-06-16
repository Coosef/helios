import { getApi } from "@/lib/api";
import { Card, DataTable, PageHeader, Banner, Badge, type Column } from "@/components/ui";
import type { Tenant } from "@/lib/types";

export default async function SuperTenantsPage() {
  const tenants = await getApi().getTenants();

  const cols: Column<Tenant>[] = [
    {
      header: "Tenant",
      render: (t) => (
        <span className="cell-strong vcenter" style={{ gap: 8 }}>
          <span
            style={{
              width: 9,
              height: 9,
              borderRadius: "50%",
              background: t.color,
              display: "inline-block",
              flex: "0 0 auto",
            }}
          />
          {t.name}
        </span>
      ),
    },
    { header: "Plan", render: (t) => <Badge tone="info">{t.plan}</Badge> },
    { header: "Id", render: (t) => <span className="mono fs-11">{t.id}</span> },
  ];

  return (
    <>
      <PageHeader title="Tenants" sub={`${tenants.length} control-plane tenants`} />

      <Banner kind="preview">Control-plane shell — mock.</Banner>

      <Card pad={false}>
        <div style={{ padding: "var(--pad)" }}>
          <DataTable columns={cols} rows={tenants} getKey={(t) => t.id} />
        </div>
      </Card>

      <div className="muted fs-11" style={{ marginTop: 10 }}>
        tenant_id is immutable and certificate-bound per ADR-003 — it cannot be changed after provisioning.
      </div>
    </>
  );
}

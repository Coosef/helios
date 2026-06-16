import { getApi } from "@/lib/api";
import type { Tenant } from "@/lib/types";
import {
  Card,
  CardHead,
  StatCard,
  Meter,
  PageHeader,
  Banner,
  StatusBadge,
  DataTable,
  bytes,
  type Column,
} from "@/components/ui";
import { Icon } from "@/components/icons";

const tenantCols: Column<Tenant>[] = [
  { header: "Tenant", render: (t) => <span className="fw-6">{t.name}</span> },
  { header: "Plan", render: (t) => <span className="muted">{t.plan}</span> },
  {
    header: "Tenant ID",
    align: "right",
    render: (t) => <span className="mono fs-12 muted">{t.id}</span>,
  },
];

export default async function Page() {
  const api = getApi();
  const [license, tenants] = await Promise.all([api.getLicense(), api.getTenants()]);

  const seatPct = license.seats ? (license.seatsUsed / license.seats) * 100 : 0;
  const quotaPct = license.quotaBytes ? (license.quotaUsedBytes / license.quotaBytes) * 100 : 0;

  return (
    <>
      <PageHeader title="Billing & Licensing" sub="Plan, seats, and storage quota across the deployment." />

      <Banner kind="preview">Billing is advisory/preview — no billing backend in Sprint 1.</Banner>

      <div className="grid-auto" style={{ marginBottom: 16 }}>
        <StatCard icon="license" label="Plan" value={license.plan} sub={`License ${license.licenseId}`} />
        <StatCard
          icon="users"
          label="Seats used"
          value={`${license.seatsUsed} / ${license.seats}`}
          sub="Not enforced — informational only"
        />
        <StatCard
          icon="storage"
          label="Quota used"
          value={bytes(license.quotaUsedBytes)}
          sub={`of ${bytes(license.quotaBytes)}`}
        />
      </div>

      <Card className="card-pad" pad>
        <CardHead
          title="License summary"
          sub="Display-only. Nothing is charged or capacity-enforced in this preview."
          right={<StatusBadge status={license.status} />}
        />
        <div className="kv" style={{ marginTop: 12 }}>
          <div className="between vcenter">
            <span className="muted fs-13">Seats</span>
            <span className="mono tnum fs-13">
              {license.seatsUsed} / {license.seats}
            </span>
          </div>
          <Meter value={seatPct} thin />
          <div className="between vcenter" style={{ marginTop: 12 }}>
            <span className="muted fs-13">Storage quota</span>
            <span className="mono tnum fs-13">
              {bytes(license.quotaUsedBytes)} / {bytes(license.quotaBytes)}
            </span>
          </div>
          <Meter value={quotaPct} color="var(--ok)" thin />
        </div>
      </Card>

      <Card className="card-pad" pad>
        <CardHead
          title="Tenants"
          sub={`${tenants.length} tenant${tenants.length === 1 ? "" : "s"} — no per-tenant invoicing in Sprint 1`}
          right={<Icon name="tenants" size={16} className="muted" />}
        />
        <DataTable columns={tenantCols} rows={tenants} getKey={(t) => t.id} />
      </Card>
    </>
  );
}

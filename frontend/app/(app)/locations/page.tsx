import { getApi } from "@/lib/api";
import { Card, DataTable, Meter, PageHeader, type Column } from "@/components/ui";
import type { LocationSite } from "@/lib/types";

export default async function LocationsPage() {
  const locations = await getApi().getLocations();

  const cols: Column<LocationSite>[] = [
    { header: "Name", render: (l) => <span className="cell-strong">{l.name}</span> },
    { header: "Devices", align: "right", render: (l) => <span className="tnum">{l.deviceCount}</span> },
    {
      header: "Health",
      render: (l) => (
        <div className="vcenter" style={{ gap: 8 }}>
          <Meter value={l.health} />
          <span className="tnum fs-12">{l.health}</span>
        </div>
      ),
    },
  ];

  return (
    <>
      <PageHeader title="Locations" sub={`${locations.length} sites · mock data`} />
      <Card pad={false}>
        <div style={{ padding: "var(--pad)" }}>
          <DataTable columns={cols} rows={locations} getKey={(l) => l.id} />
          <div className="muted fs-11" style={{ marginTop: 10 }}>
            <code className="mono">location_id</code> is an advisory, server-authoritative scope (ADR-006) — it is
            never certificate-bound.
          </div>
        </div>
      </Card>
    </>
  );
}

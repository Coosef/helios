import { getApi } from "@/lib/api";
import { Banner, Card, DataTable, Meter, PageHeader, StatusBadge, bytes, type Column } from "@/components/ui";
import type { StorageTarget } from "@/lib/types";

export default async function StoragePage() {
  const targets = await getApi().getStorageTargets();

  const cols: Column<StorageTarget>[] = [
    { header: "Name", render: (t) => <span className="cell-strong">{t.name}</span> },
    { header: "Kind", render: (t) => <span className="mono fs-12">{t.kind}</span> },
    { header: "Used", align: "right", render: (t) => <span className="tnum">{bytes(t.usedBytes)}</span> },
    { header: "Capacity", align: "right", render: (t) => <span className="tnum">{bytes(t.capacityBytes)}</span> },
    {
      header: "Usage",
      render: (t) => (
        <div style={{ width: 120 }}>
          <Meter value={t.capacityBytes > 0 ? (t.usedBytes / t.capacityBytes) * 100 : 0} />
        </div>
      ),
    },
    { header: "Status", render: (t) => <StatusBadge status={t.status} /> },
  ];

  return (
    <>
      <PageHeader title="Storage" sub={`${targets.length} storage targets · mock data`} />
      <Banner kind="pending">
        Storage targets are design-preview — the storage layer lands in Sprint 7.
      </Banner>
      <Card pad={false}>
        <div style={{ padding: "var(--pad)" }}>
          <DataTable columns={cols} rows={targets} getKey={(t) => t.id} />
        </div>
      </Card>
    </>
  );
}

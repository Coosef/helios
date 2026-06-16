import Link from "next/link";
import { getApi } from "@/lib/api";
import { Card, DataTable, PageHeader, StatusBadge, type Column } from "@/components/ui";
import type { Device } from "@/lib/types";

export default async function DevicesPage() {
  const devices = await getApi().getDevices();

  const cols: Column<Device>[] = [
    { header: "Host", render: (d) => <Link className="cell-strong mono" href={`/devices/${d.id}`}>{d.host}</Link> },
    { header: "OS", render: (d) => (d.os === "linux" ? <span title="Linux is prep-only in Sprint 1">linux · prep-only</span> : "windows") },
    { header: "Role", render: (d) => <span className="muted fs-12">{d.role}</span> },
    { header: "Enrollment", render: (d) => <StatusBadge status={d.enrollment} /> },
    { header: "Presence", render: (d) => <StatusBadge status={d.presence} /> },
    { header: "Agent", render: (d) => <span className="mono fs-12">{d.agentVersion}</span> },
    { header: "Update", render: (d) => <StatusBadge status={d.updateStatus} /> },
  ];

  return (
    <>
      <PageHeader title="Devices" sub={`${devices.length} enrolled-or-known endpoints · mock data`} />
      <Card pad={false}>
        <div style={{ padding: "var(--pad)" }}>
          <DataTable columns={cols} rows={devices} getKey={(d) => d.id} />
          <div className="muted fs-11" style={{ marginTop: 10 }}>
            Linux agents cross-compile and ship a systemd unit, but enrollment is <b>prep-only</b> until the
            Sprint-8 Linux secret protector exists (the protector currently fails closed).
          </div>
        </div>
      </Card>
    </>
  );
}

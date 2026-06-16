import Link from "next/link";
import { notFound } from "next/navigation";
import { getApi } from "@/lib/api";
import { Card, CardHead, DataTable, PageHeader, StatusBadge, type Column } from "@/components/ui";
import type { AuditEvent } from "@/lib/types";

export default async function DeviceDetailsPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const api = getApi();
  const device = await api.getDevice(id);
  if (!device) notFound();
  const audit = (await api.getAuditEvents()).filter((e) => e.deviceId === id);

  const fields: Array<[string, React.ReactNode]> = [
    ["Host", <span className="mono" key="h">{device.host}</span>],
    ["OS", device.os === "linux" ? "linux · prep-only" : "windows"],
    ["Role", device.role],
    ["Enrollment", <StatusBadge status={device.enrollment} key="e" />],
    ["Presence", <StatusBadge status={device.presence} key="p" />],
    ["Agent version", <span className="mono" key="a">{device.agentVersion}</span>],
    ["Update status", <StatusBadge status={device.updateStatus} key="u" />],
    ["SPKI fingerprint", <span className="mono fs-12" key="f">{device.fingerprint}</span>],
    ["Last seen", <span className="mono fs-12" key="l">{new Date(device.lastSeen).toLocaleString()}</span>],
  ];

  const cols: Column<AuditEvent>[] = [
    { header: "Seq", align: "right", render: (e) => <span className="mono fs-12">{e.seq}</span> },
    { header: "Event", render: (e) => <span className="mono fs-12">{e.eventType}</span> },
    { header: "Outcome", render: (e) => <StatusBadge status={e.outcome === "success" ? "healthy" : e.outcome === "denied" ? "warning" : "failed"} label={e.outcome} /> },
    { header: "When", align: "right", render: (e) => <span className="mono fs-11">{new Date(e.tsLocal).toLocaleTimeString()}</span> },
  ];

  return (
    <>
      <PageHeader
        title={device.host}
        sub="Device details · mock data"
        actions={<Link className="btn btn-sm" href="/devices">← Devices</Link>}
      />

      <div className="cols-detail">
        <Card>
          <CardHead title="Agent" />
          <div className="kv" style={{ marginTop: 8 }}>
            {fields.map(([k, v]) => (
              <div className="between" key={k} style={{ padding: "7px 0", borderBottom: "1px solid var(--border-soft)" }}>
                <span className="muted fs-12">{k}</span>
                <span className="fs-13">{v}</span>
              </div>
            ))}
          </div>
        </Card>

        <Card pad={false}>
          <CardHead title="Audit chain (this device)" sub="BLAKE3 hash-chained · tamper-evident (DR-06)" />
          <div style={{ padding: "0 var(--pad) var(--pad)" }}>
            <DataTable columns={cols} rows={audit} getKey={(e) => e.id} />
          </div>
        </Card>
      </div>
    </>
  );
}

import Link from "next/link";
import { getApi } from "@/lib/api";
import {
  Badge, Card, DataTable, Meter, PageHeader, StatCard, StatusBadge, cx, type Column,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import { deviceHealth } from "@/lib/derive";
import type { Device } from "@/lib/types";

export default async function DevicesPage() {
  const api = getApi();
  const [devices, locations] = await Promise.all([api.getDevices(), api.getLocations()]);

  const online = devices.filter((d) => d.presence === "online").length;
  const stale = devices.filter((d) => d.presence === "stale").length;
  const offline = devices.filter((d) => d.presence === "offline").length;
  const needAttention = devices.filter((d) => d.presence !== "online" || d.enrollment === "degraded").length;
  const updatesAvailable = devices.filter((d) => d.updateStatus === "update_available").length;

  const statusChips = [
    { label: "All", count: devices.length, active: true },
    { label: "Online", count: online, active: false },
    { label: "Stale", count: stale, active: false },
    { label: "Offline", count: offline, active: false },
  ];

  const cols: Column<Device>[] = [
    { header: "Host", render: (d) => <Link className="cell-strong mono" href={`/devices/${d.id}`}>{d.host}</Link> },
    { header: "OS · Agent", render: (d) => (
      <span className="fs-12">
        {d.os === "linux" ? <span title="Linux is prep-only in Sprint 1">linux · prep-only</span> : "windows"}
        <span className="muted mono"> · {d.agentVersion}</span>
      </span>
    ) },
    { header: "Role", render: (d) => <span className="muted fs-12">{d.role}</span> },
    { header: "Enrollment", render: (d) => <StatusBadge status={d.enrollment} /> },
    { header: "Presence", render: (d) => <StatusBadge status={d.presence} /> },
    { header: "Update", render: (d) => <StatusBadge status={d.updateStatus} /> },
    { header: "Health", render: (d) => {
      const h = deviceHealth(d);
      return (
        <div className="vcenter" style={{ gap: 8, minWidth: 120 }}>
          <Meter value={h.score} color={h.color} thin /><span className="mono fs-12">{h.score}</span>
        </div>
      );
    } },
    { header: "Secure", align: "center", render: (d) => {
      const ok = d.enrollment === "enrolled" && d.updateStatus !== "rolled_back";
      return <span style={{ color: ok ? "var(--ok)" : "var(--warn)" }} title={ok ? "mTLS identity + signed updates" : "needs attention"}><Icon name="shield" size={15} /></span>;
    } },
  ];

  return (
    <div className="stack">
      <PageHeader title="Devices" sub={`${devices.length} enrolled-or-known endpoints · mock data`} />

      <div className="stat-grid">
        <StatCard icon="devices" label="Endpoints" value={devices.length} sub="enrolled or known" />
        <StatCard icon="check" tint="var(--ok)" label="Online" value={online} sub={`${Math.round((online / devices.length) * 100)}% reachable`} />
        <StatCard icon="warning" tint="var(--warn)" label="Need attention" value={needAttention} sub="offline / stale / degraded" />
        <StatCard icon="update" tint="var(--info)" label="Updates available" value={updatesAvailable} sub="agent build offered" />
      </div>

      {/* Filter + search bar — design preview (decorative, not wired to a backend) */}
      <Card>
        <div className="between wrap" style={{ gap: 12 }}>
          <div className="vcenter wrap" style={{ gap: 6 }} aria-label="Status filter (design preview)">
            {statusChips.map((c) => (
              <span key={c.label} className={cx("chip", c.active && "active")}>{c.label} <span className="cnt">{c.count}</span></span>
            ))}
          </div>
          <div className="vcenter wrap" style={{ gap: 8 }}>
            <span className="search" aria-label="Search devices (design preview)">
              <Icon name="search" size={15} className="muted" />
              <input className="input" placeholder="Search devices…" disabled style={{ minWidth: 160 }} />
            </span>
            <select className="input" defaultValue="all" disabled aria-label="Site filter (design preview)" style={{ width: "auto" }}>
              <option value="all">All sites</option>
              {locations.map((l) => <option key={l.id} value={l.id}>{l.name}</option>)}
            </select>
            <select className="input" defaultValue="all" disabled aria-label="Platform filter (design preview)" style={{ width: "auto" }}>
              <option value="all">All platforms</option>
              <option value="windows">Windows</option>
              <option value="linux">Linux</option>
            </select>
          </div>
        </div>
        <div className="vcenter muted fs-11" style={{ gap: 6, marginTop: 10 }}>
          <Icon name="warning" size={13} /> Filtering, search and bulk actions are a design preview — not yet wired to the backend.
        </div>
      </Card>

      {/* Devices table */}
      <Card pad={false}>
        <div className="scroll-x" style={{ padding: "var(--pad)" }}>
          <DataTable columns={cols} rows={devices} getKey={(d) => d.id} />
          <div className="muted fs-11" style={{ marginTop: 10 }}>
            Linux agents cross-compile and ship a systemd unit, but enrollment is <b>prep-only</b> until the
            Sprint-8 Linux secret protector exists (the protector currently fails closed).
          </div>
        </div>
      </Card>

      {/* Bulk action bar — static design preview (not a live selection bar) */}
      <Card>
        <div className="between wrap" style={{ gap: 12 }}>
          <span className="vcenter fs-13" style={{ gap: 10 }}>
            <Badge tone="muted">0 selected</Badge>
            <span className="muted">Bulk actions · design preview</span>
          </span>
          <div className="vcenter wrap" style={{ gap: 8 }}>
            <button className="btn btn-sm" disabled><Icon name="play" size={14} /> <span className="hide-sm">Run backup</span></button>
            <button className="btn btn-sm" disabled><Icon name="update" size={14} /> <span className="hide-sm">Update agent</span></button>
            <button className="btn btn-sm" disabled><Icon name="shield" size={14} /> <span className="hide-sm">Assign policy</span></button>
          </div>
        </div>
      </Card>
    </div>
  );
}

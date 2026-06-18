import Link from "next/link";
import { notFound } from "next/navigation";
import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, DataTable, Meter, PageHeader, StatCard, StatusBadge,
  bytes, type Column,
} from "@/components/ui";
import { Gauge } from "@/components/charts";
import { Icon } from "@/components/icons";
import { deviceHealth } from "@/lib/derive";
import type { AuditEvent, Job } from "@/lib/types";

// Which storage target a site's devices land on (mock mapping; real per-device storage
// telemetry is backend-pending).
const SITE_TARGET: Record<string, string> = {
  loc_ist: "st_ist_qnap", loc_belek: "st_belek_vault", loc_ams: "st_ams_minio", loc_sto: "st_helios_eu",
};

export default async function DeviceDetailsPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const api = getApi();
  const device = await api.getDevice(id);
  if (!device) notFound(); // unknown id ⇒ 404, never crash

  const [jobs, auditAll, agentVersions, targets, locations, restore] = await Promise.all([
    api.getJobs(), api.getAuditEvents(), api.getAgentVersions(), api.getStorageTargets(),
    api.getLocations(), api.getRestoreCenter(),
  ]);

  const health = deviceHealth(device);
  const deviceJobs = jobs.filter((j) => j.deviceId === id);
  const audit = auditAll.filter((e) => e.deviceId === id);
  const protectedBytes = deviceJobs.filter((j) => j.status === "success").reduce((s, j) => s + j.sizeBytes, 0);
  const site = locations.find((l) => l.id === device.siteId);
  const target = targets.find((t) => t.id === SITE_TARGET[device.siteId]);
  const agentVer = agentVersions.find((v) => v.version === device.agentVersion) ?? agentVersions[0];
  const newer = agentVersions.find((v) => v.channel === "beta") ?? null;
  const restorePoints = restore.points.filter((p) => p.deviceId === id);
  const latestPoint = restorePoints[0];
  const trusted = device.enrollment === "enrolled";
  const isLinuxPrep = device.os === "linux";

  const jobCols: Column<Job>[] = [
    { header: "Type", render: (j) => <span className="fs-13">{j.type}</span> },
    { header: "Status", render: (j) => <StatusBadge status={j.status} /> },
    { header: "Started", render: (j) => <span className="mono fs-12">{new Date(j.startedAt).toISOString().slice(0, 16).replace("T", " ")}</span> },
    { header: "Duration", align: "right", render: (j) => <span className="mono fs-12">{j.durationSec ? `${Math.round(j.durationSec / 60)}m` : "—"}</span> },
    { header: "Size", align: "right", render: (j) => <span className="tnum">{j.sizeBytes ? bytes(j.sizeBytes) : "—"}</span> },
  ];

  const auditCols: Column<AuditEvent>[] = [
    { header: "Seq", align: "right", render: (e) => <span className="mono fs-12">{e.seq}</span> },
    { header: "Event", render: (e) => <span className="mono fs-12">{e.eventType}</span> },
    { header: "Actor", render: (e) => <span className="fs-12">{e.actor}</span> },
    { header: "Outcome", render: (e) => <StatusBadge status={e.outcome === "success" ? "healthy" : e.outcome === "denied" ? "warning" : "failed"} label={e.outcome} /> },
    { header: "When", align: "right", render: (e) => <span className="mono fs-11">{new Date(e.tsLocal).toLocaleTimeString()}</span> },
  ];

  return (
    <div className="stack">
      <PageHeader
        title={device.host}
        sub="Device details · mock data"
        actions={<Link className="btn btn-sm" href="/devices">← Devices</Link>}
      />

      {isLinuxPrep && (
        <Banner kind="pending">
          Linux agent is prep-only in Sprint 1 — enrollment stays fail-closed until the Sprint-8 secret protector ships.
        </Banner>
      )}

      {/* Scoreboard */}
      <div className="stat-grid">
        <StatCard icon="activity" tint={health.color} label="Device health" value={`${health.score}/100`} sub={`${health.grade} · illustrative`} />
        <StatCard icon="restore" tint="var(--ai)" label="Restore readiness"
          value={latestPoint ? `${restore.confidenceScore}/100` : "—"}
          sub={latestPoint ? "verified point · illustrative" : "no points · backend-pending"} />
        <StatCard icon="storage" tint="var(--accent-2)" label="Protected data" value={protectedBytes > 0 ? bytes(protectedBytes) : "—"} sub="successful jobs · mock" />
        <StatCard icon="shield" tint={trusted ? "var(--ok)" : "var(--warn)"} label="Agent trust" value={trusted ? "Trusted" : "Stale"} sub="mTLS · SPKI pinned" />
      </div>

      {/* Enrollment + agent/updater */}
      <div className="cols-2">
        <Card>
          <CardHead title="Enrollment" sub="Control-plane identity & presence" right={<StatusBadge status={device.enrollment} />} />
          <dl className="kv" style={{ marginTop: 12 }}>
            <dt>State</dt><dd><StatusBadge status={device.enrollment} /></dd>
            <dt>Presence</dt><dd><StatusBadge status={device.presence} /></dd>
            <dt>Site</dt><dd>{site?.name ?? device.siteId}</dd>
            <dt>Last seen</dt><dd className="mono">{new Date(device.lastSeen).toLocaleString()}</dd>
            <dt>SPKI fingerprint</dt><dd className="mono fs-12">{device.fingerprint}</dd>
          </dl>
        </Card>

        <Card>
          <CardHead title="Agent & updater" sub="Self-update posture" right={<StatusBadge status={device.updateStatus} />} />
          <dl className="kv" style={{ marginTop: 12 }}>
            <dt>Installed</dt><dd className="mono">{device.agentVersion}</dd>
            <dt>Channel</dt><dd>{agentVer ? <Badge tone="muted">{agentVer.channel}</Badge> : "—"}</dd>
            <dt>Released</dt><dd className="mono">{agentVer?.releasedAt ?? "—"}</dd>
            <dt>Latest available</dt><dd className="mono">{device.updateStatus === "update_available" && newer ? newer.version : device.agentVersion}</dd>
          </dl>
          {agentVer && (
            <div style={{ marginTop: 12 }}>
              <div className="between fs-12" style={{ marginBottom: 6 }}><span className="muted">Channel rollout</span><span className="mono">{agentVer.rolloutPct}%</span></div>
              <Meter value={agentVer.rolloutPct} thin />
            </div>
          )}
        </Card>
      </div>

      {/* Backup/restore readiness + storage usage */}
      <div className="cols-2">
        <Card pad={false}>
          <CardHead title="Backup & restore readiness" sub="Recovery confidence · illustrative" />
          {latestPoint ? (
            <div className="hero-split" style={{ padding: "0 var(--pad) var(--pad)" }}>
              <Gauge value={restore.confidenceScore} max={restore.maxScore} size={132}
                color={restore.confidenceScore >= 85 ? "var(--ok)" : "var(--warn)"} label={restore.confidenceScore} sub="confidence" />
              <dl className="kv" style={{ width: "100%" }}>
                <dt>Latest point</dt><dd>{latestPoint.kind}</dd>
                <dt>Captured</dt><dd className="mono">{new Date(latestPoint.at).toISOString().slice(0, 10)}</dd>
                <dt>Size</dt><dd className="mono">{bytes(latestPoint.sizeBytes)}</dd>
                <dt>Integrity</dt><dd>{latestPoint.verified ? <Badge tone="ok">verified</Badge> : <Badge tone="warn">unverified</Badge>}</dd>
              </dl>
            </div>
          ) : (
            <div className="muted fs-13" style={{ padding: "0 var(--pad) var(--pad)" }}>
              No verified restore points for this device yet — restore engine is backend-pending.
            </div>
          )}
        </Card>

        <Card>
          <CardHead title="Storage & protected data" sub="Where this device's backups land" />
          {target ? (
            <div className="stack" style={{ marginTop: 12 }}>
              <div className="between fs-13">
                <span className="vcenter" style={{ gap: 8 }}><Icon name="storage" size={15} className="muted" />{target.name}</span>
                {target.immutable && <Badge tone="ok">WORM</Badge>}
              </div>
              <div className="between fs-12"><span className="muted">Target usage</span><span className="mono">{bytes(target.usedBytes)} / {bytes(target.capacityBytes)}</span></div>
              <Meter value={target.capacityBytes > 0 ? (target.usedBytes / target.capacityBytes) * 100 : 0} color={target.status === "warning" ? "var(--warn)" : "var(--accent)"} />
              <dl className="kv" style={{ marginTop: 4 }}>
                <dt>Region</dt><dd>{target.region ?? "—"}</dd>
                <dt>Encryption</dt><dd className="fs-12">{target.encryption ?? "—"}</dd>
                <dt>This device</dt><dd className="mono">{protectedBytes > 0 ? bytes(protectedBytes) : "—"}</dd>
              </dl>
            </div>
          ) : <div className="muted fs-13" style={{ marginTop: 12 }}>No storage target mapped.</div>}
        </Card>
      </div>

      {/* Latest backup jobs */}
      <Card pad={false}>
        <CardHead title="Latest backup jobs" sub="For this device · mock data" />
        <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
          {deviceJobs.length > 0
            ? <DataTable columns={jobCols} rows={deviceJobs} getKey={(j) => j.id} />
            : <div className="muted fs-13" style={{ paddingTop: 8 }}>No backup jobs for this device yet.</div>}
        </div>
      </Card>

      {/* Audit chain */}
      <Card pad={false}>
        <CardHead title="Audit chain (this device)" sub="BLAKE3 hash-chained · tamper-evident (DR-06)" />
        <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
          {audit.length > 0
            ? <DataTable columns={auditCols} rows={audit} getKey={(e) => e.id} />
            : <div className="muted fs-13" style={{ paddingTop: 8 }}>No audit events recorded for this device yet.</div>}
        </div>
      </Card>
    </div>
  );
}

import { getApi } from "@/lib/api";
import { Badge, Banner, Card, DataTable, Meter, PageHeader, type Column, type Tone } from "@/components/ui";
import { Icon, type IconKey } from "@/components/icons";
import type { AgentVersion } from "@/lib/types";

const STEPS: { label: string; icon: IconKey }[] = [
  { label: "verify", icon: "shield" },
  { label: "decide", icon: "update" },
  { label: "stage", icon: "update" },
  { label: "swap", icon: "update" },
  { label: "health gate", icon: "check" },
  { label: "rollback", icon: "shield" },
];

const CHANNEL_TONE: Record<AgentVersion["channel"], Tone> = {
  stable: "ok",
  beta: "info",
  dev: "warn",
};

export default async function UpdatesPage() {
  const [versions, devices] = await Promise.all([
    getApi().getAgentVersions(),
    getApi().getDevices(),
  ]);

  const cols: Column<AgentVersion>[] = [
    { header: "Version", render: (v) => <span className="mono fw-6">{v.version}</span> },
    { header: "Channel", render: (v) => <Badge tone={CHANNEL_TONE[v.channel]}>{v.channel}</Badge> },
    { header: "Released", render: (v) => <span className="muted fs-12">{v.releasedAt}</span> },
    { header: "Devices", align: "right", render: (v) => <span className="tnum">{v.devices}</span> },
    { header: "Rollout", render: (v) => <Meter value={v.rolloutPct} /> },
  ];

  return (
    <>
      <PageHeader title="Agent Updates" sub={`${versions.length} versions · ${devices.length} known devices`} />
      <Banner kind="preview">Reflects the real updater chain (T21–T27).</Banner>

      <Card className="card-pad">
        <div className="muted fs-12 fw-6" style={{ marginBottom: 10 }}>Updater pipeline</div>
        <div className="vcenter" style={{ gap: 6, flexWrap: "wrap" }}>
          {STEPS.map((s, i) => (
            <span key={s.label} className="vcenter" style={{ gap: 6 }}>
              <span className="vcenter muted fs-12" style={{ gap: 6, padding: "6px 10px", border: "1px solid var(--line)", borderRadius: 8 }}>
                <Icon name={s.icon} size={14} />
                {s.label}
              </span>
              {i < STEPS.length - 1 && <span className="muted fs-13">→</span>}
            </span>
          ))}
        </div>
      </Card>

      <div style={{ marginTop: 14 }}>
      <Card pad={false}>
        <div style={{ padding: "var(--pad)" }}>
          <DataTable columns={cols} rows={versions} getKey={(v) => v.version} />
          <div className="muted fs-11" style={{ marginTop: 10 }}>
            Anti-rollback + Ed25519/JCS manifest + 90s health gate enforced by the agent.
          </div>
        </div>
      </Card>
      </div>
    </>
  );
}

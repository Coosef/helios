import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, DataTable, Meter, PageHeader, StatCard, Swatch, cx,
  type Column, type Tone,
} from "@/components/ui";
import { AreaChart, CapacityBar } from "@/components/charts";
import { DonutBreakdown } from "@/components/panels";
import { Icon, type IconKey } from "@/components/icons";
import type { AgentVersion } from "@/lib/types";

const STEP_ICON: Record<string, IconKey> = {
  verify: "shield", decide: "activity", stage: "update", swap: "update", "health gate": "check", rollback: "restore",
};
const TONE_ICON: Record<string, IconKey> = { ok: "check", info: "update", warn: "warning", crit: "x" };
const CHANNEL_TONE: Record<string, Tone> = { stable: "ok", beta: "info", dev: "warn" };
const RISK_TONE: Record<string, Tone> = { Low: "ok", Medium: "warn", High: "crit" };

export default async function UpdatesPage() {
  const api = getApi();
  const [o, versions] = await Promise.all([api.getUpdatesOverview(), api.getAgentVersions()]);

  const cols: Column<AgentVersion>[] = [
    { header: "Version", render: (v) => <span className="mono fw-6">{v.version}</span> },
    { header: "Channel", render: (v) => <Badge tone={CHANNEL_TONE[v.channel]}>{v.channel}</Badge> },
    { header: "Released", render: (v) => <span className="muted fs-12">{v.releasedAt}</span> },
    { header: "Devices", align: "right", render: (v) => <span className="tnum">{v.devices}</span> },
    { header: "Rollout", render: (v) => <div className="vcenter" style={{ gap: 8, minWidth: 120 }}><Meter value={v.rolloutPct} thin /><span className="mono fs-11">{v.rolloutPct}%</span></div> },
  ];

  return (
    <div className="stack">
      <PageHeader title="Agent Update Center" sub={`${versions.length} versions · ${o.kpis.fleetDevices} devices · mock data`} />
      <Banner kind="preview">
        Reflects the real updater chain (DR-06). Metrics are illustrative and the staged-rollout control is a preview — no real publishing in Sprint 1.
      </Banner>

      {/* KPI row */}
      <div className="stat-grid">
        <StatCard icon="shield" tint="var(--ok)" label="On current version" value={`${o.kpis.onCurrentPct}%`} sub="stable channel · illustrative" />
        <StatCard icon="update" tint="var(--accent)" label="Update available" value={o.kpis.updateAvailable} sub="agent build offered" />
        <StatCard icon="warning" tint="var(--warn)" label="Rolled back" value={o.kpis.rolledBack} sub="health-gate failures" />
        <StatCard icon="shield" tint="var(--ok)" label="Signature failures" value={o.kpis.signatureFailures} sub="Ed25519 verify" />
      </div>

      {/* Updater chain */}
      <Card>
        <CardHead title="Updater chain" sub="verify → decide → stage → swap → health gate → rollback · mapped to the DR-06 audit taxonomy" />
        <div className="vcenter wrap" style={{ gap: 8, marginTop: 12 }}>
          {o.chain.map((s, i) => (
            <span key={s.step} className="vcenter" style={{ gap: 8 }}>
              <span className="vcenter" style={{ gap: 8, padding: "8px 12px", border: "1px solid var(--border)", borderRadius: 9 }}>
                <span style={{ color: `var(--${s.tone})` }}><Icon name={STEP_ICON[s.step]} size={15} /></span>
                <span className="fs-13 fw-6">{s.step}</span>
              </span>
              {i < o.chain.length - 1 && <span className="muted fs-13">→</span>}
            </span>
          ))}
        </div>
        <div className="list-rows" style={{ marginTop: 14, border: "1px solid var(--border)", borderRadius: 10 }}>
          {o.chain.map((s) => (
            <div key={s.step} className="between wrap" style={{ gap: 8 }}>
              <span className="fs-13 fw-6" style={{ textTransform: "capitalize" }}>{s.step}</span>
              <span className="vcenter wrap" style={{ gap: 6 }}>
                {s.auditEvents.map((e) => <Badge key={e} tone="muted">{e}</Badge>)}
              </span>
            </div>
          ))}
        </div>
      </Card>

      {/* Staged rollout + trust */}
      <div className="cols-2">
        <Card>
          <CardHead title="Staged rollout" sub="Canary → Early Adopters → Production" right={<Badge tone="muted">Preview · disabled</Badge>} />
          <div style={{ marginTop: 12 }}>
            <CapacityBar segments={o.rings.map((r) => ({ pct: r.pct, color: r.color, label: r.name }))} />
            <div className="list-rows" style={{ marginTop: 12 }}>
              {o.rings.map((r) => (
                <div key={r.name} className="stack" style={{ gap: 6 }}>
                  <div className="between wrap fs-13">
                    <span className="vcenter" style={{ gap: 8 }}><Swatch color={r.color} /><span className="fw-6">{r.name}</span><Badge tone={r.status === "done" ? "ok" : r.status === "active" ? "info" : "muted"}>{r.status}</Badge></span>
                    <span className="muted fs-12"><span className="mono">{r.pct}%</span> · {r.devices} device{r.devices === 1 ? "" : "s"}</span>
                  </div>
                  <div className="between fs-12">
                    <span className="muted">Risk <Badge tone={RISK_TONE[r.risk]}>{r.risk}</Badge></span>
                    <span className="muted">{r.devices > 0 ? <>success <span className="mono">{r.successPct}%</span> · rollbacks <span className="mono">{r.rollbacks}</span></> : "not started"}</span>
                  </div>
                </div>
              ))}
            </div>
            <button className="btn btn-sm" disabled style={{ marginTop: 12 }}><Icon name="play" size={14} /> Promote ring — preview</button>
            <div className="muted fs-11" style={{ marginTop: 8 }}>Staged publishing lands in a later sprint; this control performs no action.</div>
          </div>
        </Card>

        <Card pad={false}>
          <CardHead title="Signature & trust" sub="Verified before every install" right={<Badge tone="ok">{o.trust.filter((t) => t.ok).length}/{o.trust.length}</Badge>} />
          <div className="list-rows">
            {o.trust.map((t) => (
              <div key={t.label} className="vcenter" style={{ gap: 12, alignItems: "flex-start" }}>
                <span style={{ color: t.ok ? "var(--ok)" : "var(--warn)", marginTop: 2 }}><Icon name={t.ok ? "check" : "warning"} size={16} /></span>
                <div><div className="fs-13 fw-6">{t.label}</div><div className="muted fs-12">{t.detail}</div></div>
              </div>
            ))}
          </div>
        </Card>
      </div>

      {/* Adoption trend (wide) + channels & adoption (narrow) */}
      <div className="cols-2">
        <Card>
          <CardHead title="Version adoption trend" sub="Devices per version · last 14 days · illustrative" right={
            <span className="vcenter fs-12" style={{ gap: 12 }}>
              {o.adoptionTrend.series.map((s) => <span key={s.version} className="vcenter" style={{ gap: 6 }}><Swatch color={s.color} />{s.version}</span>)}
            </span>
          } />
          <div style={{ marginTop: 12 }}>
            <AreaChart series={o.adoptionTrend.series.map((s) => ({ data: s.data, color: s.color, name: s.version }))} labels={o.adoptionTrend.labels} h={200} />
          </div>
        </Card>

        <Card>
          <CardHead title="Release channels & adoption" sub="By channel and version" />
          <div className="hero-split" style={{ marginTop: 8 }}>
            <DonutBreakdown segments={o.adoption} size={140} centerMain={o.kpis.fleetDevices} centerSub="agents" />
            <div className="stack" style={{ width: "100%" }}>
              {o.channels.map((c) => (
                <div key={c.channel} className="between fs-12">
                  <span className="vcenter" style={{ gap: 8 }}><Swatch color={c.color} /><span className="fw-6" style={{ textTransform: "capitalize" }}>{c.channel}</span></span>
                  <span className="muted">{c.versions} ver · <span className="mono">{c.devices}</span> devices</span>
                </div>
              ))}
            </div>
          </div>
        </Card>
      </div>

      {/* Rollbacks + event timeline */}
      <div className="cols-2">
        <Card pad={false}>
          <CardHead title="Health-gate failures & rollbacks" sub="Auto-rollback restores the previous signed build" right={<Badge tone="warn">{o.rollbacks.length}</Badge>} />
          <div className="list-rows">
            {o.rollbacks.length > 0 ? o.rollbacks.map((r) => (
              <div key={r.deviceHost} className="between wrap" style={{ gap: 8 }}>
                <span className="vcenter" style={{ gap: 8 }}><Icon name="warning" size={15} className="muted" /><span className="mono fs-13">{r.deviceHost}</span></span>
                <span className="vcenter wrap" style={{ gap: 8 }}><span className="muted fs-12">{r.reason}</span><Badge tone="muted">{r.auditEvent}</Badge></span>
              </div>
            )) : <div className="muted fs-13">No rollbacks in the current sample.</div>}
          </div>
        </Card>

        <Card pad={false}>
          <CardHead title="Update events (DR-06)" sub="Updater audit taxonomy · illustrative" />
          <div className="tl" style={{ margin: "14px var(--pad)" }}>
            {o.eventTimeline.map((e) => (
              <div key={e.id} className={cx("tl-item", e.tone)}>
                <div className="between wrap" style={{ gap: 8 }}>
                  <span className="vcenter" style={{ gap: 8 }}><span style={{ color: `var(--${e.tone})` }}><Icon name={TONE_ICON[e.tone]} size={14} /></span><span className="mono fs-12 fw-6">{e.eventType}</span></span>
                  <span className="muted mono fs-11">{new Date(e.at).toLocaleTimeString()}</span>
                </div>
                <div className="muted fs-12">{e.detail}</div>
              </div>
            ))}
          </div>
        </Card>
      </div>

      {/* All versions table */}
      <Card pad={false}>
        <CardHead title="All versions" sub={`${versions.length} agent builds · mock data`} />
        <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
          <DataTable columns={cols} rows={versions} getKey={(v) => v.version} />
          <div className="muted fs-11" style={{ marginTop: 10 }}>
            Ed25519 / JCS manifest verified on-device before install; anti-rollback blocks downgrades; rollback restores the previous build after a health-gate failure.
          </div>
        </div>
      </Card>
    </div>
  );
}

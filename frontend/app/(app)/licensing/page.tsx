import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, DataTable, Meter, PageHeader, StatCard, StatusBadge,
  bytes, cx, type Column,
} from "@/components/ui";
import { AreaChart } from "@/components/charts";
import { DonutBreakdown } from "@/components/panels";
import { Icon } from "@/components/icons";
import type { LicensingOverview } from "@/lib/types";

type Entitlement = LicensingOverview["entitlements"][number];
type StatusEntry = LicensingOverview["statusCatalog"][number];

/** Advisory consumption meter — threshold markers are surfaced, never enforced. */
function ThresholdMeter({ label, used, total, pct, thresholds }: { label: string; used: string; total: string; pct: number; thresholds: number[] }) {
  return (
    <div className="stack" style={{ gap: 6 }}>
      <div className="between fs-12"><span className="muted">{label}</span><span className="mono">{used} / {total} · {pct}%</span></div>
      <Meter value={pct} color={pct >= 90 ? "var(--warn)" : "var(--ok)"} />
      <div className="vcenter wrap fs-11" style={{ gap: 8 }}>
        {thresholds.map((t) => <Badge key={t} tone={pct >= t ? "warn" : "muted"}>{t}%{pct >= t ? " · advisory" : ""}</Badge>)}
        <span className="muted">surfaced, never enforced</span>
      </div>
    </div>
  );
}

export default async function LicensingPage() {
  const api = getApi();
  const [o, license] = await Promise.all([api.getLicensingOverview(), api.getLicense()]);

  const entCols: Column<Entitlement>[] = [
    { header: "Feature", render: (e) => <span className="cell-strong">{e.feature}</span> },
    { header: "Limit", render: (e) => <span className="fs-13">{e.limit}</span> },
    { header: "In use", render: (e) => <span className="mono fs-12">{e.used}</span> },
    { header: "Enforcement", align: "right", render: () => <Badge tone="muted">Advisory</Badge> },
  ];

  return (
    <div className="stack">
      <PageHeader title="Licensing" sub="Advisory license posture — verified, parsed, audited; never enforced (S1-T17)." />
      <Banner kind="preview">
        Advisory only in Sprint 1 (S1-T17): the license signature is verified fail-closed, but expiry / seats / quota / tenant are PARSED and AUDITED, never enforced.
      </Banner>

      {/* KPI row */}
      <div className="stat-grid">
        <StatCard icon="license" label="Plan" value={o.kpis.plan} sub="entitlement tier" />
        <StatCard icon="users" tint="var(--info)" label="Seats" value={`${o.kpis.seatsUsed}/${o.kpis.seats}`} sub={`${o.kpis.seatPct}% allocated — not enforced`} />
        <StatCard icon="storage" tint="var(--accent-2)" label="Quota" value={`${o.kpis.quotaPct}%`} sub={`${bytes(o.kpis.quotaUsedBytes)} / ${bytes(o.kpis.quotaBytes)} — advisory`} />
        <StatCard icon="shield" tint="var(--ok)" label="Status" value={<StatusBadge status={o.kpis.status} />} sub={`renews in ${o.kpis.daysToExpiry}d (advisory)`} />
      </div>

      {/* Meters + seat breakdown */}
      <div className="cols-2">
        <Card>
          <CardHead title="Consumption — visibility only" sub="Over-seat / over-quota does NOT block any backup." />
          <div className="stack" style={{ marginTop: 14, gap: 16 }}>
            <ThresholdMeter label="Seats allocated" used={String(o.kpis.seatsUsed)} total={String(o.kpis.seats)} pct={o.kpis.seatPct} thresholds={o.warningThresholds} />
            <ThresholdMeter label="Storage quota" used={bytes(o.kpis.quotaUsedBytes)} total={bytes(o.kpis.quotaBytes)} pct={o.kpis.quotaPct} thresholds={o.warningThresholds} />
          </div>
        </Card>

        <Card>
          <CardHead title="Seat allocation" sub="Pooled entitlement · advisory" />
          <div className="hero-split" style={{ marginTop: 8 }}>
            <DonutBreakdown segments={o.seatBreakdown} size={140} centerMain={`${o.kpis.seatPct}%`} centerSub="allocated" />
            <div className="stack" style={{ width: "100%" }}>
              {o.seatBreakdown.map((s) => (
                <div key={s.label} className="between fs-12"><span className="vcenter" style={{ gap: 8 }}><span style={{ width: 9, height: 9, borderRadius: 3, background: s.color, display: "inline-block" }} />{s.label}</span><span className="mono">{s.value}</span></div>
              ))}
              <div className="muted fs-11">Over-allocation is surfaced to audit, never blocked.</div>
            </div>
          </div>
        </Card>
      </div>

      {/* Quota trend + renewal timeline */}
      <div className="cols-2">
        <Card>
          <CardHead title="Quota consumption trend" sub="% of quota used · illustrative" />
          <div style={{ marginTop: 12 }}>
            <AreaChart series={[{ data: o.quotaTrend.data, color: "var(--accent-2)", name: "Quota %" }]} labels={o.quotaTrend.labels} yMax={100} h={190} />
          </div>
        </Card>

        <Card>
          <CardHead title="Renewal & expiry" sub="Expiry is advisory — surfaced, never enforced" right={<Badge tone={o.renewal.autoRenew ? "ok" : "muted"}>{o.renewal.autoRenew ? "Auto-renew on" : "Auto-renew off"}</Badge>} />
          <div style={{ marginTop: 12 }}>
            <div className="between fs-12" style={{ marginBottom: 6 }}><span className="muted">Term elapsed</span><span className="mono">{o.kpis.daysToExpiry} days remaining</span></div>
            <Meter value={38} />
            <div className="tl" style={{ marginTop: 16 }}>
              {o.renewalTimeline.map((m) => (
                <div key={m.label} className={cx("tl-item", m.state === "done" ? "ok" : m.state === "current" ? "info" : undefined)}>
                  <div className="between wrap" style={{ gap: 8 }}>
                    <span className="fs-13 fw-6">{m.label}</span>
                    <span className="muted mono fs-11">{m.at}</span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </Card>
      </div>

      {/* Status catalog */}
      <Card>
        <CardHead title="License status catalog" sub="What the parser detects — none of these block operations in Sprint 1." />
        <div className="stat-grid" style={{ marginTop: 12 }}>
          {o.statusCatalog.map((s: StatusEntry) => (
            <div key={s.status} className="card card-pad" style={{ border: s.active ? "1px solid rgba(var(--accent-rgb),.4)" : "1px solid var(--border)" }}>
              <div className="between" style={{ marginBottom: 8 }}>
                <StatusBadge status={s.status} />
                {s.active ? <Badge tone="ok">Current</Badge> : <Badge tone="muted">Illustrative</Badge>}
              </div>
              <div className="muted fs-12">{s.advisoryAction}</div>
              {s.auditEvent && <div className="mono fs-11" style={{ marginTop: 8, color: "var(--text-2)" }}>{s.auditEvent}</div>}
            </div>
          ))}
        </div>
      </Card>

      {/* Entitlements + license audit trail */}
      <div className="cols-2">
        <Card pad={false}>
          <CardHead title="Entitlements" sub="Plan features & limits — advisory caps, not enforced quotas" />
          <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
            <DataTable columns={entCols} rows={o.entitlements} getKey={(e) => e.feature} />
            <div className="muted fs-11" style={{ marginTop: 10 }}>Limits are advisory in Sprint 1 — surfaced and audited, never enforced.</div>
          </div>
        </Card>

        <Card pad={false}>
          <CardHead title="License audit trail" sub="Anomalies are written to the BLAKE3 hash-chained audit log only" />
          <div className="tl" style={{ margin: "14px var(--pad)" }}>
            {o.auditTimeline.map((e) => (
              <div key={e.id} className={cx("tl-item", e.severity)}>
                <div className="between wrap" style={{ gap: 8 }}>
                  <span className="vcenter" style={{ gap: 8 }}><Icon name="audit" size={14} className="muted" /><span className="mono fs-12 fw-6">{e.eventType}</span><Badge tone={e.outcome === "success" ? "ok" : e.outcome === "denied" ? "warn" : "crit"}>{e.outcome}</Badge></span>
                  <span className="muted mono fs-11">{new Date(e.at).toLocaleDateString()}</span>
                </div>
                <div className="muted fs-12">{e.detail}</div>
              </div>
            ))}
          </div>
        </Card>
      </div>

      {/* History + license details */}
      <div className="cols-2">
        <Card>
          <CardHead title="License history" sub="Tenant entitlement timeline" />
          <div className="tl" style={{ marginTop: 14 }}>
            {o.history.map((h) => (
              <div key={h.at + h.event} className="tl-item info">
                <div className="between wrap" style={{ gap: 8 }}><span className="fs-13 fw-6">{h.event}</span><span className="muted mono fs-11">{h.at}</span></div>
                <div className="muted fs-12">{h.detail}</div>
              </div>
            ))}
          </div>
        </Card>

        <Card>
          <CardHead title="License details" sub="Parsed from the signed token" right={<StatusBadge status={license.status} />} />
          <dl className="kv" style={{ marginTop: 12 }}>
            <dt>License ID</dt><dd className="mono fs-12">{license.licenseId}</dd>
            <dt>Tenant</dt><dd className="mono fs-12">{license.tenantId}</dd>
            <dt>Plan</dt><dd className="fw-6">{license.plan}</dd>
            <dt>Not after</dt><dd className="mono fs-12">{new Date(license.notAfter).toISOString().slice(0, 10)}</dd>
            <dt>Status</dt><dd><StatusBadge status={license.status} /></dd>
          </dl>
          <div className="muted fs-12" style={{ marginTop: 12 }}>
            Nothing on this page blocks operations. Expiry, seat, quota, and tenant mismatches are surfaced and written to the audit log only.
          </div>
        </Card>
      </div>
    </div>
  );
}

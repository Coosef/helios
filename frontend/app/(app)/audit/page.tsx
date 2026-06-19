import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, DataTable, PageHeader, StatCard, StatusBadge, cx,
  type Column,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import type { AuditEvent } from "@/lib/types";

function outcomeStatus(o: AuditEvent["outcome"]): string {
  if (o === "success") return "healthy";
  if (o === "denied") return "warning";
  return "failed";
}

const SEVERITY_CHIPS = ["All", "Critical", "Warning", "Info"];

export default async function AuditPage() {
  const api = getApi();
  const [events, o] = await Promise.all([api.getAuditEvents(), api.getAuditOverview()]);
  const d = o.selectedDetail;

  const columns: Column<AuditEvent>[] = [
    { header: "Seq", align: "right", render: (e) => <span className="mono tnum">{e.seq}</span> },
    { header: "Event", render: (e) => <span className="mono fs-12">{e.eventType}</span> },
    { header: "Outcome", render: (e) => <StatusBadge status={outcomeStatus(e.outcome)} label={e.outcome} /> },
    { header: "Actor", render: (e) => <span className="fs-12">{e.actor}</span> },
    { header: "Tenant", render: (e) => <span className="mono fs-11">{e.tenantId}</span> },
    { header: "When", align: "right", render: (e) => <span className="muted fs-11">{new Date(e.tsLocal).toLocaleTimeString()}</span> },
  ];

  return (
    <div className="stack">
      <PageHeader
        title="Audit Logs"
        sub="Tamper-evident, hash-chained event trail · mock data"
        actions={<Badge tone="info" lg><Icon name="shield" size={14} /> Tamper-evident</Badge>}
      />
      <Banner kind="preview">
        Audit events use the frozen DR-06 taxonomy and are BLAKE3 hash-chained (tamper-evident); server-side ingestion lands in Sprint 2.
      </Banner>

      {/* KPI row */}
      <div className="stat-grid">
        <StatCard icon="audit" label="Events today" value={o.kpis.eventsToday} sub="across the fleet · illustrative" />
        <StatCard icon="warning" tint="var(--crit)" label="Critical" value={o.kpis.critical} sub="denied / failed outcomes" />
        <StatCard icon="shield" tint="var(--ok)" label="Integrity OK" value={`${o.kpis.integrityOkPct}%`} sub="chain verified" />
        <StatCard icon="clock" tint="var(--info)" label="Retention" value={`${o.kpis.retentionYears} yr`} sub="compliance hold" />
      </div>

      {/* Integrity panel */}
      <Card>
        <CardHead
          title="Integrity chain"
          sub="Hash-chained · tamper-evident"
          right={<Badge tone="ok"><Icon name="check" size={12} /> Verified · preview</Badge>}
        />
        <div className="cols-2" style={{ marginTop: 12 }}>
          <dl className="kv">
            <dt>Algorithm</dt><dd className="mono">{o.integrity.algorithm}</dd>
            <dt>Tamper-evident</dt><dd>{o.integrity.tamperEvident ? <Badge tone="ok">yes</Badge> : <Badge tone="crit">no</Badge>}</dd>
            <dt>Chain status</dt><dd>{o.integrity.chainIntact ? <Badge tone="ok">intact</Badge> : <Badge tone="crit">broken</Badge>}</dd>
            <dt>Verified blocks</dt><dd className="mono">{o.integrity.verifiedBlocks}</dd>
            <dt>Last verified</dt><dd className="mono">{o.integrity.lastVerified}</dd>
          </dl>
          <div className="stack">
            <div className="muted fs-12">Per-event hashes are chained so any tampering breaks the chain. Verification is a design preview.</div>
            <div className="scroll-x">
              <div className="vcenter" style={{ gap: 6 }}>
                {o.timeline.map((t, i) => (
                  <span key={t.id} className="vcenter" style={{ gap: 6 }}>
                    <span className="mono fs-11" style={{ padding: "4px 8px", border: "1px solid var(--border)", borderRadius: 7, color: "var(--ok)" }}>#{t.seq}</span>
                    {i < o.timeline.length - 1 && <span className="muted">→</span>}
                  </span>
                ))}
              </div>
            </div>
            <button className="btn btn-sm" disabled style={{ alignSelf: "flex-start" }}><Icon name="update" size={14} /> Verify chain — preview</button>
          </div>
        </div>
      </Card>

      {/* Filter bar — mock UI */}
      <Card>
        <div className="between wrap" style={{ gap: 12 }}>
          <div className="vcenter wrap" style={{ gap: 6 }} aria-label="Severity filter (design preview)">
            {SEVERITY_CHIPS.map((c, i) => <span key={c} className={cx("chip", i === 0 && "active")} style={{ cursor: "default" }}>{c}</span>)}
          </div>
          <div className="vcenter wrap" style={{ gap: 8 }}>
            <select className="input" defaultValue="all" disabled aria-label="Actor filter (design preview)" style={{ width: "auto" }}><option value="all">All actors</option></select>
            <select className="input" defaultValue="all" disabled aria-label="Action filter (design preview)" style={{ width: "auto" }}><option value="all">All actions</option></select>
            <select className="input" defaultValue="7d" disabled aria-label="Date range (design preview)" style={{ width: "auto" }}><option value="7d">Last 7 days</option></select>
            <span className="search" aria-label="Search audit (design preview)">
              <Icon name="search" size={15} className="muted" />
              <input className="input" placeholder="Search actor, action…" disabled style={{ minWidth: 160 }} />
            </span>
          </div>
        </div>
        <div className="vcenter muted fs-11" style={{ gap: 6, marginTop: 10 }}>
          <Icon name="warning" size={13} /> Filtering and search are a design preview — not yet wired to the backend.
        </div>
      </Card>

      {/* Timeline + static detail drawer */}
      <div className="cols-2">
        <Card>
          <CardHead title="Recent events" sub="Most significant activity · derived from the audit trail" />
          <div className="tl" style={{ marginTop: 14 }}>
            {o.timeline.map((t) => (
              <div key={t.id} className={cx("tl-item", t.severity)}>
                <div className="between wrap" style={{ gap: 8 }}>
                  <span className="vcenter" style={{ gap: 8 }}>
                    <span className="fw-6 fs-13">{t.action}</span>
                    <Badge tone="muted">{t.category}</Badge>
                  </span>
                  <span className="muted mono fs-11">{new Date(t.at).toLocaleTimeString()}</span>
                </div>
                <div className="muted fs-12">
                  {t.actor}{t.deviceHost ? <> · <span className="mono">{t.deviceHost}</span></> : null} · <span className="mono">{t.ip}</span>
                </div>
              </div>
            ))}
          </div>
        </Card>

        <Card>
          <CardHead title="Event detail" sub="Static preview — not a live flyout" right={<StatusBadge status={d.result === "Denied" ? "warning" : "healthy"} label={d.result} />} />
          <div className="stack" style={{ marginTop: 12 }}>
            <dl className="kv">
              <dt>Event</dt><dd className="mono fs-12">{d.id}</dd>
              <dt>Signature</dt><dd>{d.signatureValid ? <Badge tone="ok">valid</Badge> : <Badge tone="crit">invalid</Badge>}</dd>
              <dt>Chain</dt><dd>{d.chainIntact ? <Badge tone="ok">intact</Badge> : <Badge tone="crit">broken</Badge>}</dd>
              <dt>User agent</dt><dd className="fs-12">{d.userAgent}</dd>
            </dl>
            <div className="muted fs-11" style={{ textTransform: "uppercase", letterSpacing: ".06em" }}>Cryptographic chain</div>
            <div style={{ padding: "10px 12px", border: "1px solid var(--border)", borderRadius: 9 }}>
              <div className="muted fs-11">Event hash</div>
              <div className="mono fs-12" style={{ color: "var(--ok)", marginTop: 3, wordBreak: "break-all" }}>{d.eventHash}</div>
            </div>
            <div style={{ padding: "10px 12px", border: "1px solid var(--border)", borderRadius: 9 }}>
              <div className="muted fs-11">Previous hash</div>
              <div className="mono fs-12 muted" style={{ marginTop: 3, wordBreak: "break-all" }}>{d.previousHash}</div>
            </div>
          </div>
        </Card>
      </div>

      {/* Full audit table */}
      <Card pad={false}>
        <CardHead title="All events" sub={`${events.length} in the current sample · chain integrity verified (preview)`} />
        <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
          <DataTable columns={columns} rows={events} getKey={(e) => e.id} />
        </div>
      </Card>
    </div>
  );
}

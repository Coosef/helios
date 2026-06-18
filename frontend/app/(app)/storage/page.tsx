import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, DataTable, Meter, PageHeader, StatCard, StatusBadge,
  Swatch, bytes, type Column,
} from "@/components/ui";
import { CapacityBar } from "@/components/charts";
import { DonutBreakdown } from "@/components/panels";
import { Icon } from "@/components/icons";
import type { StorageTarget } from "@/lib/types";

function usagePct(t: StorageTarget): number {
  return t.capacityBytes > 0 ? Math.round((t.usedBytes / t.capacityBytes) * 100) : 0;
}
function usageColor(pct: number): string {
  return pct >= 90 ? "var(--crit)" : pct >= 75 ? "var(--warn)" : "var(--accent)";
}

export default async function StoragePage() {
  const { kpis, coverage, tiers, targets } = await getApi().getStorageOverview();
  const warningTargets = targets.filter((t) => t.status !== "healthy");

  const cols: Column<StorageTarget>[] = [
    { header: "Name", render: (t) => <span className="cell-strong">{t.name}</span> },
    { header: "Kind", render: (t) => <span className="mono fs-12">{t.kind}</span> },
    { header: "Region", render: (t) => <span className="muted fs-12">{t.region ?? "—"}</span> },
    { header: "Used", align: "right", render: (t) => <span className="tnum">{bytes(t.usedBytes)}</span> },
    { header: "Capacity", align: "right", render: (t) => <span className="tnum">{bytes(t.capacityBytes)}</span> },
    { header: "Usage", render: (t) => <div style={{ width: 120 }}><Meter value={usagePct(t)} color={usageColor(usagePct(t))} thin /></div> },
    { header: "Status", render: (t) => <StatusBadge status={t.status} /> },
  ];

  return (
    <div className="stack">
      <PageHeader title="Storage" sub={`${targets.length} storage targets · mock data`} />
      <Banner kind="pending">
        Storage engine is backend-pending — capacity, tiers and immutability figures below are illustrative mock data.
      </Banner>

      {/* KPI row */}
      <div className="stat-grid">
        <StatCard icon="storage" label="Storage used" value={`${kpis.usagePct}%`} sub={`${bytes(kpis.usedBytes)} / ${bytes(kpis.capacityBytes)}`} />
        <StatCard icon="shield" tint="var(--ok)" label="Immutable coverage" value={`${kpis.immutablePct}%`} sub="WORM-locked · illustrative" />
        <StatCard icon="activity" tint="var(--accent-2)" label="Data reduction" value={`${kpis.reductionRatio}×`} sub="dedup + compression" />
        <StatCard icon="clock" tint="var(--warn)" label="Forecast runway" value={`${kpis.runwayDays}d`} sub="at current growth · illustrative" />
      </div>

      {/* Immutability coverage + tier breakdown */}
      <div className="cols-2">
        <Card>
          <CardHead title="Immutability & resilience coverage" sub="Share of protected data by protection class" right={<Badge tone="ok">Recovery assured</Badge>} />
          <div style={{ marginTop: 14 }}>
            <CapacityBar segments={coverage} />
            <div className="vcenter wrap" style={{ gap: 16, marginTop: 12 }}>
              {coverage.map((c) => (
                <span key={c.label} className="vcenter fs-12" style={{ gap: 6 }}>
                  <Swatch color={c.color} /><span className="muted">{c.label}</span><span className="mono">{c.pct}%</span>
                </span>
              ))}
            </div>
            <div className="muted fs-11" style={{ marginTop: 12 }}>Object-lock retention 90 days on WORM targets (illustrative).</div>
          </div>
        </Card>

        <Card>
          <CardHead title="Storage tiers" sub="Lifecycle distribution · illustrative" />
          <DonutBreakdown segments={tiers} size={132} centerMain={tiers.length} centerSub="tiers" />
        </Card>
      </div>

      {/* Target cards */}
      <div className="grid-auto" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 300px), 1fr))" }}>
        {targets.map((t) => {
          const pct = usagePct(t);
          return (
            <Card key={t.id}>
              <div className="between" style={{ marginBottom: 10 }}>
                <div className="vcenter" style={{ gap: 8 }}>
                  <Icon name={t.kind === "helios_cloud" ? "cloud" : "storage"} size={16} className="muted" />
                  <div>
                    <div className="fw-6 fs-13">{t.name}</div>
                    <div className="muted fs-12">{t.kind} · {t.region ?? "—"}</div>
                  </div>
                </div>
                <StatusBadge status={t.status} />
              </div>

              <div className="between fs-12" style={{ marginBottom: 6 }}>
                <span className="muted">{pct}% used</span><span className="mono">{bytes(t.usedBytes)} / {bytes(t.capacityBytes)}</span>
              </div>
              <CapacityBar segments={[{ pct, color: usageColor(pct), label: `${bytes(t.usedBytes)} used` }]} />

              <div className="vcenter wrap" style={{ gap: 8, marginTop: 12 }}>
                {t.immutable && <Badge tone="ok">WORM immutable</Badge>}
                <Badge tone="muted">{t.encryption ?? "encrypted"}</Badge>
                {t.throughput && <span className="muted fs-11">{t.throughput}</span>}
              </div>
            </Card>
          );
        })}
      </div>

      {/* Headroom / health */}
      <Card>
        <CardHead title="Headroom & health" sub="Fleet capacity outlook · illustrative" />
        <div className="stack" style={{ marginTop: 12 }}>
          <div className="between fs-12"><span className="muted">Fleet headroom</span><span className="mono">{100 - kpis.usagePct}% free · ~{kpis.runwayDays}d runway</span></div>
          <Meter value={kpis.usagePct} color={usageColor(kpis.usagePct)} />
          {warningTargets.length > 0 ? (
            <div className="muted fs-12">
              {warningTargets.length} target{warningTargets.length > 1 ? "s" : ""} need attention:{" "}
              {warningTargets.map((t) => `${t.name} (${usagePct(t)}%)`).join(", ")}.
            </div>
          ) : <div className="muted fs-12">All targets within healthy capacity.</div>}
        </div>
      </Card>

      {/* Retained table */}
      <Card pad={false}>
        <CardHead title="All storage targets" sub={`${targets.length} targets`} />
        <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
          <DataTable columns={cols} rows={targets} getKey={(t) => t.id} />
        </div>
      </Card>
    </div>
  );
}

import { getApi } from "@/lib/api";
import {
  Badge, Card, CardHead, DataTable, Meter, PageHeader, StatCard, StatusBadge, bytes,
  type Column,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import type { SiteRollup } from "@/lib/types";

function healthColor(h: number): string {
  return h >= 96 ? "var(--ok)" : h >= 90 ? "var(--accent-2)" : h >= 80 ? "var(--warn)" : "var(--crit)";
}

function Dot({ color }: { color: string }) {
  return <span style={{ width: 8, height: 8, borderRadius: "50%", background: color, display: "inline-block" }} />;
}

export default async function LocationsPage() {
  const { sites, groups, kpis } = await getApi().getLocationsOverview();

  const cols: Column<SiteRollup>[] = [
    { header: "Location", render: (s) => (
      <span className="cell-strong vcenter" style={{ gap: 8 }}><Dot color={s.tenantColor} />{s.name}</span>
    ) },
    { header: "City", render: (s) => <span className="muted fs-12">{s.city}</span> },
    { header: "Status", render: (s) => <StatusBadge status={s.storageStatus} /> },
    { header: "Health", render: (s) => (
      <div className="vcenter" style={{ gap: 8, minWidth: 120 }}>
        <Meter value={s.health} color={healthColor(s.health)} thin /><span className="mono fs-12">{s.health}</span>
      </div>
    ) },
    { header: "Devices", align: "right", render: (s) => <span className="tnum">{s.deviceCount}</span> },
  ];

  return (
    <div className="stack">
      <PageHeader title="Locations" sub={`${kpis.siteCount} sites · ${kpis.deviceCount} devices · mock data`} />

      <div className="stat-grid">
        <StatCard icon="pin" label="Locations" value={kpis.siteCount} sub="sites in scope" />
        <StatCard icon="devices" tint="var(--ok)" label="Protected devices" value={kpis.deviceCount} sub="across all sites" />
        <StatCard icon="globe" tint="var(--accent-2)" label="Cities" value={kpis.cityCount} sub="regions" />
        <StatCard icon="activity" tint="var(--warn)" label="Avg. health" value={kpis.avgHealth} sub="site protection score" />
      </div>

      {/* Regional grouping (no map dependency — schematic only) */}
      <Card>
        <CardHead title="By region" sub="Sites grouped by city · schematic (no map)" />
        <div className="stack" style={{ marginTop: 12 }}>
          {groups.map((g) => (
            <div key={g.city} className="stack" style={{ gap: 6 }}>
              <div className="between wrap fs-13">
                <span className="vcenter" style={{ gap: 8 }}><Icon name="globe" size={15} className="muted" /><span className="fw-6">{g.city}</span><Badge tone="muted">{g.sites.length} site{g.sites.length > 1 ? "s" : ""}</Badge></span>
                <span className="muted fs-12">{g.deviceCount} devices · <span className="mono">{g.avgHealth}</span> avg health</span>
              </div>
              <Meter value={g.avgHealth} color={healthColor(g.avgHealth)} thin />
            </div>
          ))}
        </div>
      </Card>

      {/* Per-site cards */}
      <div className="grid-auto" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(300px, 1fr))" }}>
        {sites.map((s) => (
          <Card key={s.id}>
            <div className="between" style={{ marginBottom: 10 }}>
              <div className="vcenter" style={{ gap: 8 }}>
                <Dot color={s.tenantColor} />
                <div>
                  <div className="fw-6 fs-13">{s.name}</div>
                  <div className="muted fs-12">{s.tenantName} · {s.city}</div>
                </div>
              </div>
              <StatusBadge status={s.storageStatus} />
            </div>

            <div className="between fs-12" style={{ marginBottom: 6 }}>
              <span className="muted">Site health</span><span className="mono">{s.health}</span>
            </div>
            <Meter value={s.health} color={healthColor(s.health)} />

            <div className="vcenter wrap fs-12" style={{ gap: 12, marginTop: 12 }}>
              <span className="vcenter" style={{ gap: 6 }}><Dot color="var(--ok)" />{s.online} online</span>
              {s.warning > 0 && <span className="vcenter" style={{ gap: 6 }}><Dot color="var(--warn)" />{s.warning} warning</span>}
              {s.offline > 0 && <span className="vcenter" style={{ gap: 6 }}><Dot color="var(--crit)" />{s.offline} offline</span>}
              <span className="muted">· {s.deviceCount} devices</span>
            </div>
            {s.linuxPrepOnly > 0 && (
              <div className="muted fs-11" style={{ marginTop: 6 }}>
                {s.linuxPrepOnly} Linux device prep-only — agent lands in a later sprint.
              </div>
            )}

            <div className="between fs-12" style={{ marginTop: 12, borderTop: "1px solid var(--border)", paddingTop: 10 }}>
              <span className="vcenter muted" style={{ gap: 8 }}><Icon name="storage" size={14} />{s.storageName}</span>
              <span className="mono">{s.protectedBytes > 0 ? bytes(s.protectedBytes) : "—"}</span>
            </div>
          </Card>
        ))}
      </div>

      {/* Detailed table */}
      <Card pad={false}>
        <CardHead title="All locations" sub={`${sites.length} sites`} />
        <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
          <DataTable columns={cols} rows={sites} getKey={(s) => s.id} />
          <div className="muted fs-11" style={{ marginTop: 10, lineHeight: 1.6 }}>
            <code className="mono">location_id</code> is an advisory, server-authoritative scope (ADR-006) — it is
            never certificate-bound. Linux sites (e.g. the Amsterdam plant&rsquo;s app server) are enrolled
            prep-only; the Linux agent lands in a later sprint.
          </div>
        </div>
      </Card>
    </div>
  );
}

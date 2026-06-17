import Link from "next/link";
import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, DataTable, Meter, PageHeader, StatusBadge, bytes, cx,
  type Column,
} from "@/components/ui";
import { Gauge } from "@/components/charts";
import { Icon } from "@/components/icons";
import type { FileNode, RestoreActivityEntry, RestorePoint } from "@/lib/types";

const READINESS_ICON = { pass: "check", pending: "clock", fail: "warning" } as const;
const READINESS_TONE = { pass: "ok", pending: "warn", fail: "crit" } as const;

/** Static, fully-expanded file-browser mock (server-safe — no client state). */
function FileTreeView({ nodes, depth = 0, path = "" }: { nodes: FileNode[]; depth?: number; path?: string }) {
  if (depth > 16) return null; // defensive guard against pathological nesting
  return (
    <>
      {nodes.map((n) => {
        const key = `${path}/${n.name}`;
        return (
          <div key={key}>
            <div className={cx("tree-row", n.selected && "sel")} style={{ paddingLeft: 8 + depth * 18 }}>
              {n.kind === "dir"
                ? <span className="tree-caret open"><Icon name="chevron" size={14} /></span>
                : <span style={{ width: 16, flex: "none" }} />}
              <Icon name={n.kind === "dir" ? "globe" : "audit"} size={15} className="muted" />
              <span className={n.selected ? "fw-6" : undefined}>{n.name}</span>
              {n.kind === "file" && n.sizeBytes != null && (
                <span className="muted mono fs-11" style={{ marginLeft: "auto" }}>{bytes(n.sizeBytes)}</span>
              )}
            </div>
            {n.kind === "dir" && (n.children && n.children.length > 0
              ? <FileTreeView nodes={n.children} depth={depth + 1} path={key} />
              : <div className="muted fs-12" style={{ padding: "4px 8px", paddingLeft: 8 + (depth + 1) * 18 + 22 }}>Empty folder</div>)}
          </div>
        );
      })}
    </>
  );
}

function collectSelected(nodes: FileNode[], out: FileNode[] = []): FileNode[] {
  for (const n of nodes) {
    if (n.selected) out.push(n);
    if (n.children) collectSelected(n.children, out);
  }
  return out;
}

function pointTone(p: RestorePoint): "ok" | "warn" {
  return p.verified ? "ok" : "warn";
}

export default async function RestorePage() {
  const rc = await getApi().getRestoreCenter();
  const selected = collectSelected(rc.tree);
  const selectedBytes = selected.reduce((s, f) => s + (f.sizeBytes ?? 0), 0);
  // Illustrative recovery-time estimate at ~75 MB/s effective throughput.
  const estMin = selectedBytes / (75 * 1024 * 1024) / 60;
  const estLabel = estMin < 1 ? "< 1 min" : `~${Math.round(estMin)} min`;

  const activityCols: Column<RestoreActivityEntry>[] = [
    { header: "Item", render: (a) => <span className="mono fs-12">{a.item}</span> },
    { header: "Type", render: (a) => <Badge tone="muted">{a.type}</Badge> },
    { header: "Destination", render: (a) => <span className="muted fs-12">{a.destination}</span> },
    { header: "By", render: (a) => <span className="fs-12">{a.by}</span> },
    {
      header: "Status",
      render: (a) => (
        <div className="vcenter" style={{ gap: 8 }}>
          <StatusBadge status={a.status} />
          {a.status === "running" && a.progressPct != null && (
            <span className="vcenter" style={{ gap: 6, minWidth: 90 }}>
              <Meter value={a.progressPct} thin /><span className="mono fs-11">{a.progressPct}%</span>
            </span>
          )}
        </div>
      ),
    },
    { header: "When", align: "right", render: (a) => <span className="muted fs-11">{a.when}</span> },
  ];

  return (
    <div className="stack">
      <PageHeader
        title="Restore Center"
        sub="Recover files, folders, machines or app items from any verified recovery point · design-preview"
        actions={<Badge tone="ai" lg><Icon name="shield" size={14} /> Restore Confidence {rc.confidenceScore}/{rc.maxScore}</Badge>}
      />

      <Banner kind="pending">
        Restore is a design-preview — the restore engine lands in a later sprint. The timeline, file browser and
        controls below are illustrative; no restore is actually performed.
      </Banner>

      {/* Recovery point-in-time timeline */}
      <Card>
        <CardHead
          title="Recovery timeline"
          sub={<>Source <span className="mono">{rc.sourceHost}</span> · most recent recovery points (mock)</>}
          right={<span className="vcenter fs-12 muted" style={{ gap: 12 }}>
            <span className="vcenter" style={{ gap: 6 }}><span style={{ width: 8, height: 8, borderRadius: "50%", background: "var(--ok)", display: "inline-block" }} />Verified</span>
            <span className="vcenter" style={{ gap: 6 }}><span style={{ width: 8, height: 8, borderRadius: "50%", background: "var(--warn)", display: "inline-block" }} />Unverified</span>
          </span>}
        />
        <div className="tl" style={{ marginTop: 14 }}>
          {rc.points.map((p, i) => (
            <div key={p.id} className={cx("tl-item", pointTone(p))}>
              <div className="between wrap" style={{ gap: 8 }}>
                <span className="vcenter" style={{ gap: 8 }}>
                  <span className="fw-6 fs-13">{p.kind}</span>
                  {i === 0 && <Badge tone="info">Selected</Badge>}
                  {!p.verified && <Badge tone="warn">Unverified</Badge>}
                </span>
                <span className="muted mono fs-12">{new Date(p.at).toISOString().slice(0, 10)} · {bytes(p.sizeBytes)}</span>
              </div>
              <div className="muted fs-12">{p.chainOk ? "Chain to last full image validated" : "Chain incomplete"}</div>
            </div>
          ))}
        </div>
      </Card>

      {/* File browser + restore selection / readiness rail */}
      <div className="cols-2">
        <Card>
          <CardHead title="File browser" sub="Expand folders and pick items to recover (illustrative)" />
          <div style={{ marginTop: 10 }}>
            <FileTreeView nodes={rc.tree} />
          </div>
        </Card>

        <div className="stack">
          <Card>
            <CardHead title="Restore selection" sub="Design-preview — nothing is restored" />
            <div className="stack" style={{ marginTop: 12 }}>
              {selected.length === 0
                ? <div className="muted fs-13">Select files or folders to recover.</div>
                : selected.map((f, i) => (
                    <div key={`${f.name}-${i}`} className="between fs-13">
                      <span className="vcenter" style={{ gap: 8 }}><Icon name="audit" size={14} className="muted" /> {f.name}</span>
                      <span className="muted mono fs-12">{bytes(f.sizeBytes ?? 0)}</span>
                    </div>
                  ))}
              <dl className="kv" style={{ marginTop: 4 }}>
                <dt>Total size</dt><dd>{bytes(selectedBytes)}</dd>
                <dt>Est. recovery</dt><dd>{estLabel}</dd>
                <dt>Destination</dt><dd>Original location</dd>
              </dl>
              <button className="btn btn-primary btn-sm" disabled style={{ marginTop: 4 }}>
                Restore ({selected.length}) — preview
              </button>
              <div className="muted fs-11">
                Source backup is immutable and is never modified; recovery is fully audited and verified before any
                byte is written. A restore that cannot prove correctness fails closed.
              </div>
            </div>
          </Card>

          <Card pad={false}>
            <CardHead title="Recovery readiness" sub="Verification summary · illustrative" right={<Badge tone="ok">{rc.confidenceScore}/{rc.maxScore}</Badge>} />
            <div className="hero-split" style={{ padding: "0 var(--pad) 8px" }}>
              <Gauge value={rc.confidenceScore} max={rc.maxScore} size={132}
                color={rc.confidenceScore >= 85 ? "var(--ok)" : "var(--warn)"} label={rc.confidenceScore} sub="confidence" />
              <div className="muted fs-12">Restore Confidence is a composite of the checks below — recovery validation, integrity, RTO and immutability.</div>
            </div>
            <div className="list-rows">
              {rc.readiness.map((c) => (
                <div key={c.label} className="vcenter" style={{ gap: 12, alignItems: "flex-start" }}>
                  <span style={{ color: `var(--${READINESS_TONE[c.status]})`, marginTop: 2 }}>
                    <Icon name={READINESS_ICON[c.status]} size={16} />
                  </span>
                  <div style={{ flex: 1 }}>
                    <div className="between">
                      <span className="fs-13 fw-6">{c.label}</span>
                      <Badge tone={READINESS_TONE[c.status]}>{c.status}</Badge>
                    </div>
                    <div className="muted fs-12">{c.detail}</div>
                  </div>
                </div>
              ))}
            </div>
          </Card>
        </div>
      </div>

      {/* Recent restore activity */}
      <Card pad={false}>
        <CardHead title="Recent restore activity" sub="Restore sessions across the fleet (mock)" right={<Link className="btn btn-sm" href="/jobs">View jobs</Link>} />
        <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
          <DataTable columns={activityCols} rows={rc.activity} getKey={(a) => a.id} />
        </div>
      </Card>
    </div>
  );
}

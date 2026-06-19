import Link from "next/link";
import { getApi } from "@/lib/api";
import {
  Badge, Banner, Card, CardHead, DataTable, Meter, PageHeader, Sparkline, StatCard,
  StatusBadge, Swatch, bytes, type Column,
} from "@/components/ui";
import { AreaChart } from "@/components/charts";
import { DonutBreakdown } from "@/components/panels";
import type { Job } from "@/lib/types";

export default async function JobsPage() {
  const api = getApi();
  const [jobs, o] = await Promise.all([api.getJobs(), api.getJobsOverview()]);
  const recentFailed = jobs.filter((j) => j.status === "failed");

  const cols: Column<Job>[] = [
    { header: "Host", render: (j) => <Link className="cell-strong mono" href={`/jobs/${j.id}`}>{j.deviceHost}</Link> },
    { header: "Type", render: (j) => <span className="fs-13">{j.type}</span> },
    { header: "Status", render: (j) => <StatusBadge status={j.status} /> },
    { header: "Progress", render: (j) => (
      j.status === "running"
        ? <div className="vcenter" style={{ gap: 8, minWidth: 110 }}><Meter value={64} thin /><span className="mono fs-11">64%</span></div>
        : <span className="muted fs-12">{j.status === "success" ? "100%" : "—"}</span>
    ) },
    { header: "Size", align: "right", render: (j) => <span className="tnum">{j.sizeBytes ? bytes(j.sizeBytes) : "—"}</span> },
    { header: "Started", align: "right", render: (j) => <span className="mono fs-11">{new Date(j.startedAt).toISOString().slice(0, 16).replace("T", " ")}</span> },
  ];

  return (
    <div className="stack">
      <PageHeader title="Backup Jobs" sub={`${jobs.length} jobs · mock data`} />
      <Banner kind="pending">
        Backup engine is design-preview — the metrics below are illustrative mock data; the engine lands in Sprints 3–7.
      </Banner>

      {/* KPI row */}
      <div className="stat-grid">
        <StatCard icon="jobs" label="Total jobs" value={o.kpis.total.toLocaleString("en-US")} spark={o.trend.completed} />
        <StatCard icon="activity" tint="var(--info)" label="Running" value={o.kpis.running} sub="active sessions" />
        <StatCard icon="check" tint="var(--ok)" label="Success rate" value={`${o.kpis.successRatePct}%`} sub="trailing 30 days · illustrative" />
        <StatCard icon="warning" tint="var(--crit)" label="Failed today" value={o.kpis.failedToday} sub="needs review" />
      </div>

      {/* Pipeline + 14-day trend */}
      <div className="cols-2">
        <Card>
          <CardHead title="Job pipeline" sub="Distribution by current state · illustrative" />
          <DonutBreakdown
            segments={o.pipeline}
            size={150}
            centerMain={o.kpis.total.toLocaleString("en-US")}
            centerSub="jobs"
          />
        </Card>

        <Card>
          <CardHead title="14-day job outcomes" sub="Completed vs failed · illustrative" right={
            <span className="vcenter fs-12" style={{ gap: 14 }}>
              <span className="vcenter" style={{ gap: 6 }}><Swatch color="var(--ok)" />Completed</span>
              <span className="vcenter" style={{ gap: 6 }}><Swatch color="var(--crit)" />Failed</span>
            </span>
          } />
          <div style={{ marginTop: 12 }}>
            <AreaChart
              series={[
                { data: o.trend.completed, color: "var(--ok)", name: "Completed" },
                { data: o.trend.failed, color: "var(--crit)", name: "Failed" },
              ]}
              labels={o.trend.labels}
              h={200}
            />
          </div>
        </Card>
      </div>

      {/* Throughput + failure analysis */}
      <div className="cols-2">
        <Card>
          <CardHead title="Throughput" sub="Protected volume moved · illustrative" />
          <div className="hero-split" style={{ marginTop: 12 }}>
            <div>
              <div className="display" style={{ fontSize: 30, fontWeight: 600, lineHeight: 1 }}>{o.throughput.perDayTB} TB<span className="muted fs-13">/day</span></div>
              <div className="muted fs-13" style={{ marginTop: 6 }}>{o.throughput.perWeekTB} TB / week</div>
            </div>
            <Sparkline data={o.throughput.spark} color="var(--accent)" w={260} h={64} />
          </div>
        </Card>

        <Card>
          <CardHead title="Failure analysis" sub="Recent failures & top causes · illustrative" right={<Badge tone="crit">{o.kpis.failedToday} today</Badge>} />
          <div className="stack" style={{ marginTop: 12 }}>
            {recentFailed.length > 0 ? recentFailed.map((j) => (
              <div key={j.id} className="between fs-13">
                <span className="vcenter" style={{ gap: 8 }}><StatusBadge status="failed" /><span className="mono">{j.deviceHost}</span></span>
                <span className="muted fs-12">{j.type}</span>
              </div>
            )) : <div className="muted fs-13">No failed jobs in the current sample.</div>}
            <div style={{ borderTop: "1px solid var(--border)", paddingTop: 10 }}>
              {o.topFailureReasons.map((r) => (
                <div key={r.reason} style={{ marginBottom: 8 }}>
                  <div className="between fs-12" style={{ marginBottom: 4 }}><span className="muted">{r.reason}</span><span className="mono">{r.count} · {r.pct}%</span></div>
                  <Meter value={r.pct} color="var(--crit)" thin />
                </div>
              ))}
            </div>
          </div>
        </Card>
      </div>

      {/* All jobs table */}
      <Card pad={false}>
        <CardHead title="All jobs" sub={`${jobs.length} in the current sample · mock data`} />
        <div className="scroll-x" style={{ padding: "0 var(--pad) var(--pad)" }}>
          <DataTable columns={cols} rows={jobs} getKey={(j) => j.id} />
        </div>
      </Card>
    </div>
  );
}

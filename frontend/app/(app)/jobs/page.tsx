import Link from "next/link";
import { getApi } from "@/lib/api";
import { Banner, Card, DataTable, PageHeader, StatusBadge, bytes, type Column } from "@/components/ui";
import type { Job } from "@/lib/types";

export default async function JobsPage() {
  const jobs = await getApi().getJobs();

  const cols: Column<Job>[] = [
    { header: "Host", render: (j) => <Link className="cell-strong mono" href={`/jobs/${j.id}`}>{j.deviceHost}</Link> },
    { header: "Type", render: (j) => j.type },
    { header: "Status", render: (j) => <StatusBadge status={j.status} /> },
    { header: "Started", render: (j) => <span className="mono fs-12">{new Date(j.startedAt).toLocaleString()}</span> },
    { header: "Size", align: "right", render: (j) => bytes(j.sizeBytes) },
  ];

  return (
    <>
      <PageHeader title="Backup Jobs" sub={`${jobs.length} jobs · mock data`} />
      <Banner kind="pending">Backup engine is design-preview — backend lands in Sprints 3–7.</Banner>
      <Card pad={false}>
        <div style={{ padding: "var(--pad)" }}>
          <DataTable columns={cols} rows={jobs} getKey={(j) => j.id} />
        </div>
      </Card>
    </>
  );
}

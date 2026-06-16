import Link from "next/link";
import { notFound } from "next/navigation";
import { getApi } from "@/lib/api";
import { Banner, Card, CardHead, PageHeader, StatusBadge, bytes } from "@/components/ui";

export default async function JobDetailsPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const job = await getApi().getJob(id);
  if (!job) notFound();

  const fields: Array<[string, React.ReactNode]> = [
    ["Device", <span className="mono" key="d">{job.deviceHost}</span>],
    ["Type", job.type],
    ["Status", <StatusBadge status={job.status} key="s" />],
    ["Started at", <span className="mono fs-12" key="t">{new Date(job.startedAt).toLocaleString()}</span>],
    ["Duration", <span className="tnum" key="dur">{job.durationSec}s</span>],
    ["Size", <span className="tnum" key="sz">{bytes(job.sizeBytes)}</span>],
  ];

  return (
    <>
      <PageHeader
        title={job.deviceHost}
        sub="Job details · mock data"
        actions={<Link className="btn btn-sm" href="/jobs">← Jobs</Link>}
      />

      <Banner kind="pending">Live job telemetry is pending backend integration.</Banner>

      <Card>
        <CardHead title="Job" sub={<span className="mono fs-12">{job.id}</span>} />
        <div className="kv" style={{ marginTop: 8 }}>
          {fields.map(([k, v]) => (
            <div className="between" key={k} style={{ padding: "7px 0", borderBottom: "1px solid var(--border-soft)" }}>
              <span className="muted fs-12">{k}</span>
              <span className="fs-13">{v}</span>
            </div>
          ))}
        </div>
      </Card>
    </>
  );
}

import { getApi } from "@/lib/api";
import type { AuditEvent } from "@/lib/types";
import {
  PageHeader,
  Banner,
  Card,
  DataTable,
  StatusBadge,
  type Column,
} from "@/components/ui";

function outcomeStatus(o: AuditEvent["outcome"]): string {
  if (o === "success") return "healthy";
  if (o === "denied") return "warning";
  return "failed";
}

export default async function Page() {
  const events = await getApi().getAuditEvents();

  const columns: Column<AuditEvent>[] = [
    {
      header: "Seq",
      align: "right",
      render: (e) => <span className="mono tnum">{e.seq}</span>,
    },
    {
      header: "Event",
      render: (e) => <span className="mono fs-12">{e.eventType}</span>,
    },
    {
      header: "Outcome",
      render: (e) => (
        <StatusBadge status={outcomeStatus(e.outcome)} label={e.outcome} />
      ),
    },
    { header: "Actor", render: (e) => e.actor },
    {
      header: "Tenant",
      render: (e) => <span className="mono fs-11">{e.tenantId}</span>,
    },
    {
      header: "When",
      render: (e) => (
        <span className="muted">
          {new Date(e.tsLocal).toLocaleTimeString()}
        </span>
      ),
    },
  ];

  return (
    <>
      <PageHeader title="Audit Logs" sub="Tamper-evident, hash-chained event trail" />
      <Banner kind="preview">
        Audit events use the frozen DR-06 taxonomy and are BLAKE3 hash-chained
        (tamper-evident); server-side ingestion lands in Sprint 2.
      </Banner>
      <Card>
        <DataTable columns={columns} rows={events} getKey={(e) => e.id} />
      </Card>
    </>
  );
}

import { Banner, Card, CardHead, PageHeader } from "@/components/ui";
import { Icon, type IconKey } from "@/components/icons";

interface ReportTile {
  icon: IconKey;
  title: string;
  sub: string;
}

const REPORTS: ReportTile[] = [
  { icon: "check", title: "Backup success rate", sub: "Completed vs. failed jobs across the fleet, trended over time." },
  { icon: "storage", title: "Storage growth", sub: "Capacity consumption per target with retention projections." },
  { icon: "shield", title: "Compliance summary", sub: "Coverage, retention, and policy adherence by tenant and site." },
  { icon: "activity", title: "Fleet health", sub: "Agent presence, update status, and alert volume at a glance." },
];

export default function Page() {
  return (
    <>
      <PageHeader title="Reports" sub="Operational and compliance reporting across the Helios fleet." />

      <Banner kind="pending">Reporting backend is not built yet — design preview.</Banner>

      <div className="grid-auto" style={{ marginTop: 16, gap: 16, gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))" }}>
        {REPORTS.map((r) => (
          <Card key={r.title}>
            <CardHead
              title={
                <span className="vcenter" style={{ gap: 8 }}>
                  <Icon name={r.icon} size={16} />
                  {r.title}
                </span>
              }
              sub={r.sub}
            />
            <div className="muted fs-12" style={{ marginTop: 12 }}>
              Available once the management API ships.
            </div>
          </Card>
        ))}
      </div>
    </>
  );
}

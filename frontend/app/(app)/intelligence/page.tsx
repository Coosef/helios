import { Card, CardHead, PageHeader, Banner } from "@/components/ui";
import { Icon } from "@/components/icons";
import type { IconKey } from "@/components/icons";

type Insight = {
  icon: IconKey;
  title: string;
  sub: string;
};

const INSIGHTS: Insight[] = [
  {
    icon: "sparkle",
    title: "Predictive disk-failure risk",
    sub: "Forecast drive health across protected devices before failures occur.",
  },
  {
    icon: "sparkle",
    title: "Anomalous backup duration",
    sub: "Surface jobs that run far slower or faster than their baseline.",
  },
  {
    icon: "sparkle",
    title: "Restore-readiness score",
    sub: "Estimate recovery confidence per tenant from coverage and recency.",
  },
  {
    icon: "sparkle",
    title: "Storage growth projection",
    sub: "Model capacity trends and anticipate target exhaustion windows.",
  },
  {
    icon: "sparkle",
    title: "Alert noise clustering",
    sub: "Group correlated alerts to highlight likely root causes.",
  },
  {
    icon: "sparkle",
    title: "Policy drift detection",
    sub: "Flag devices whose protection drifts from their assigned policy.",
  },
];

export default function Page() {
  return (
    <>
      <PageHeader
        title="Helios Intelligence"
        sub="Experimental analytics and forecasting across your backup estate."
      />

      <Banner kind="preview">Future preview — no AI backend exists yet.</Banner>

      <div className="grid-auto" style={{ marginTop: 16, gap: 16 }}>
        {INSIGHTS.map((insight) => (
          <Card key={insight.title}>
            <CardHead
              title={
                <span className="vcenter" style={{ gap: 8 }}>
                  <Icon name={insight.icon} size={16} />
                  {insight.title}
                </span>
              }
              sub={insight.sub}
            />
            <div className="muted fs-12" style={{ marginTop: 12 }}>
              Preview — not yet available
            </div>
          </Card>
        ))}
      </div>
    </>
  );
}

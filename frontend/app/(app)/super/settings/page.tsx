import { Banner, Card, CardHead, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";

export default function Page() {
  return (
    <>
      <PageHeader title="Platform Settings" sub="Control-plane configuration for the Helios platform." />
      <Banner kind="preview">Control-plane shell — mock.</Banner>

      <div className="grid-auto" style={{ marginTop: 16, gap: 16 }}>
        <Card>
          <CardHead title="Platform identity" sub="How the platform presents itself" />
          <div className="kv" style={{ marginTop: 12 }}>
            <span className="muted fs-12">Product</span>
            <span className="fw-6 vcenter" style={{ gap: 8 }}>
              <Icon name="sparkle" size={16} className="muted" />
              Helios
            </span>
            <span className="muted fs-12">Operator</span>
            <span className="mono fs-13">© Beyz System A.Ş.</span>
          </div>
        </Card>

        <Card>
          <CardHead
            title="Regions"
            sub="Multi-region control planes"
            right={<Icon name="globe" size={18} className="muted" />}
          />
          <div className="muted fs-13" style={{ marginTop: 12 }}>
            Configured in Sprint 2+
          </div>
        </Card>

        <Card>
          <CardHead
            title="Security"
            sub="Transport and update trust"
            right={<Icon name="shield" size={18} className="muted" />}
          />
          <div className="fs-13" style={{ marginTop: 12, lineHeight: 1.5 }}>
            SPKI pinning + Ed25519 update trust enforced by the agent
          </div>
        </Card>
      </div>
    </>
  );
}

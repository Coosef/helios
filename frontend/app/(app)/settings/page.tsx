import { Banner, Card, CardHead, PageHeader } from "@/components/ui";

export default function SettingsPage() {
  return (
    <>
      <PageHeader title="Settings" sub="Console preferences and platform information" />

      <div className="grid-auto" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(320px, 1fr))" }}>
        <Card>
          <CardHead title="Appearance" sub="Theme, density and language" />
          <p className="muted fs-13" style={{ margin: 0 }}>
            Theme and density are controlled from the top bar, and language from the
            sidebar. These preferences apply instantly across the console — there is
            nothing to save here.
          </p>
        </Card>

        <Card>
          <CardHead title="About" sub="Helios Data Protection Platform" />
          <div className="kv fs-13" style={{ display: "grid", gap: 6 }}>
            <div className="between">
              <span className="muted">Product</span>
              <span className="fw-6">Helios Data Protection Platform</span>
            </div>
            <div className="between">
              <span className="muted">Console version</span>
              <span className="mono">0.1.0</span>
            </div>
            <div className="between">
              <span className="muted">Copyright</span>
              <span>© Beyz System A.Ş.</span>
            </div>
          </div>
          <div className="muted fs-12" style={{ marginTop: 12 }}>
            UI Sprint 1 — product shell, mock data only
          </div>
        </Card>

        <Card>
          <CardHead title="Integrations" sub="External systems" />
          <Banner kind="pending">Management API integration lands in Sprint 2</Banner>
        </Card>
      </div>
    </>
  );
}

import { getApi } from "@/lib/api";
import { Badge, Banner, Card, CardHead, PageHeader, Swatch, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SettingsTabs, type SettingsTab } from "@/components/settings-tabs";

/** Read-only preview field: a labelled, disabled select showing the current mock value. */
function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="field">
      <label>{label}</label>
      <select className="input" disabled defaultValue="v" aria-label={`${label} (design preview)`}>
        <option value="v">{value}</option>
      </select>
    </div>
  );
}

/** Static (non-interactive) toggle, read-only preview. */
function StaticSwitch({ on }: { on: boolean }) {
  return <span className={cx("switch", on && "on")} aria-hidden="true" />;
}

export default async function SettingsPage() {
  const s = await getApi().getSettingsOverview();

  const tabs: SettingsTab[] = [
    {
      key: "general", label: "General", icon: "settings",
      content: (
        <Card>
          <CardHead title="General" sub="Locale & language" right={<Badge tone="ai">Preview</Badge>} />
          <div className="grid-auto" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", marginTop: 12 }}>
            <Field label="Timezone" value={s.general.timezone} />
            <Field label="Date format" value={s.general.dateFormat} />
            <Field label="Language" value={s.general.language} />
            <Field label="Organization" value={s.general.organization} />
          </div>
          <div className="muted fs-11" style={{ marginTop: 12 }}>Theme, density and language also live in the top bar / sidebar; these are a non-functional preview.</div>
        </Card>
      ),
    },
    {
      key: "security", label: "Security", icon: "shield",
      content: (
        <Card pad={false}>
          <CardHead title="Security" sub="Access & encryption posture" right={<Badge tone="ai">Preview</Badge>} />
          <div className="list-rows">
            <div className="between"><div><div className="fs-13 fw-6">Multi-factor authentication</div><div className="muted fs-12">Required for all users on every login.</div></div><StaticSwitch on={s.security.mfaEnforced} /></div>
            <div className="between"><div><div className="fs-13 fw-6">Session timeout</div><div className="muted fs-12">Idle sessions are signed out automatically.</div></div><span className="mono fs-12">{s.security.sessionTimeout}</span></div>
            <div className="between"><div><div className="fs-13 fw-6">Password policy</div><div className="muted fs-12">Complexity and rotation requirements.</div></div><span className="mono fs-12">{s.security.passwordPolicy}</span></div>
            <div className="between"><div><div className="fs-13 fw-6">Encryption (KMS)</div><div className="muted fs-12">Key management for data at rest.</div></div><Badge tone="ok">{s.security.encryptionKms}</Badge></div>
          </div>
        </Card>
      ),
    },
    {
      key: "notifications", label: "Notifications", icon: "bell",
      content: (
        <Card pad={false}>
          <CardHead title="Notifications" sub="Alert delivery channels" right={<Badge tone="ai">Preview</Badge>} />
          <div className="list-rows">
            {s.notifications.map((n) => (
              <div key={n.channel} className="between">
                <div className="vcenter" style={{ gap: 12 }}>
                  <StaticSwitch on={n.connected} />
                  <div><div className="fs-13 fw-6">{n.channel}</div><div className="muted fs-12">{n.detail}</div></div>
                </div>
                {n.connected ? <Badge tone="ok">Connected</Badge> : <button className="btn btn-sm" disabled>Connect</button>}
              </div>
            ))}
          </div>
        </Card>
      ),
    },
    {
      key: "branding", label: "Branding", icon: "sparkle",
      content: (
        <Card>
          <CardHead title="Branding" sub="Console appearance preview" right={<Badge tone="ai">Preview</Badge>} />
          <div className="grid-auto" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", marginTop: 12 }}>
            <div className="field">
              <label>Logo</label>
              <div className="vcenter" style={{ gap: 10, padding: "14px 16px", border: "1px solid var(--border)", borderRadius: 10 }}>
                <span style={{ width: 26, height: 26, borderRadius: 8, background: "var(--accent)", display: "grid", placeItems: "center" }}><Icon name="shield" size={15} /></span>
                <span className="fw-7">{s.branding.logoLabel}</span>
              </div>
            </div>
            <div className="field">
              <label>Theme</label>
              <div className="seg">
                <button type="button" className={cx(s.branding.theme === "Dark" && "active")} disabled>Dark</button>
                <button type="button" className={cx(s.branding.theme === "Light" && "active")} disabled>Light</button>
              </div>
            </div>
            <div className="field">
              <label>Accent · {s.branding.accentName}</label>
              <div className="vcenter wrap" style={{ gap: 10, paddingTop: 4 }}>
                {s.branding.accentSwatches.map((a) => (
                  <span key={a.name} className="vcenter" style={{ gap: 6 }} title={a.name}><Swatch color={a.color} size={16} /></span>
                ))}
              </div>
            </div>
          </div>
        </Card>
      ),
    },
    {
      key: "integrations", label: "Integrations", icon: "cloud",
      content: (
        <Card>
          <CardHead title="Integrations" sub="External systems" />
          <Banner kind="pending">Management API integration lands in Sprint 2</Banner>
        </Card>
      ),
    },
    {
      key: "about", label: "About", icon: "license",
      content: (
        <Card>
          <CardHead title="About" sub="Helios Data Protection Platform" />
          <dl className="kv" style={{ marginTop: 12 }}>
            <dt>Product</dt><dd className="fw-6">{s.about.product}</dd>
            <dt>Version</dt><dd className="mono">{s.about.version}</dd>
            <dt>Build</dt><dd className="mono">{s.about.build}</dd>
            <dt>Environment</dt><dd>{s.about.environment}</dd>
            <dt>Copyright</dt><dd>{s.about.copyright}</dd>
          </dl>
          <div className="muted fs-12" style={{ marginTop: 12 }}>UI Sprint 1 — product shell, mock data only.</div>
        </Card>
      ),
    },
  ];

  return (
    <div className="stack">
      <PageHeader title="Settings" sub="Console preferences and platform information" actions={<Badge tone="ai" lg>Preview · mock</Badge>} />
      <SettingsTabs tabs={tabs} />
    </div>
  );
}

import { Banner, Card, CardHead, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";

const RECOVERY_POINTS = [
  { kind: "Full image", at: "2026-06-16 03:00", size: "412 GB" },
  { kind: "Incremental", at: "2026-06-15 03:00", size: "8.4 GB" },
  { kind: "Incremental", at: "2026-06-14 03:00", size: "11.2 GB" },
  { kind: "Full image", at: "2026-06-09 03:00", size: "405 GB" },
];

export default function RestorePage() {
  return (
    <>
      <PageHeader title="Restore Center" sub="Recover protected data from verified recovery points · design-preview" />

      <Banner kind="pending">Restore is design-preview — the restore engine lands in Sprint 6.</Banner>

      <div className="grid-auto" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(300px, 1fr))" }}>
        <Card pad={false}>
          <CardHead title="Recovery points" sub="Most recent restore points (mock)" />
          <div style={{ padding: "0 var(--pad) var(--pad)" }}>
            {RECOVERY_POINTS.map((rp, i) => (
              <div key={i} className="between vcenter" style={{ padding: "10px 0", borderTop: i ? "1px solid var(--border)" : "none" }}>
                <div className="vcenter" style={{ gap: 10 }}>
                  <Icon name="clock" size={15} className="muted" />
                  <span className="fw-6 fs-13">{rp.kind}</span>
                  <span className="muted mono fs-12">· {rp.at}</span>
                </div>
                <span className="muted tnum fs-12">{rp.size}</span>
              </div>
            ))}
          </div>
        </Card>

        <Card>
          <div className="vcenter" style={{ gap: 8, marginBottom: 8 }}>
            <Icon name="shield" size={16} className="muted" />
            <span className="fw-7 fs-13">Verified before restore</span>
          </div>
          <p className="muted fs-12" style={{ lineHeight: 1.6 }}>
            Restore correctness is a core guarantee in Helios. Before any restore begins, the selected recovery
            point is integrity-checked end to end — chunk hashes (BLAKE3) are re-verified against the manifest and
            the chain back to the last full image is validated.
          </p>
          <p className="muted fs-12" style={{ lineHeight: 1.6, marginTop: 10 }}>
            A restore that cannot prove correctness fails closed and never overwrites live data. Decryption and
            target validation are exercised in a dry run first, so a recovery point that would not restore cleanly
            is rejected before a single byte is written.
          </p>
        </Card>
      </div>
    </>
  );
}

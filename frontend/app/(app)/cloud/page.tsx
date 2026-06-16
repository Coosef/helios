import { getApi } from "@/lib/api";
import {
  Banner,
  bytes,
  Card,
  CardHead,
  Meter,
  PageHeader,
  StatusBadge,
} from "@/components/ui";
import type { StorageTarget } from "@/lib/types";

export default async function HeliosCloudPage() {
  const targets = await getApi().getStorageTargets();
  const cloud: StorageTarget[] = targets.filter((t) => t.kind === "helios_cloud");

  return (
    <>
      <PageHeader
        title="Helios Cloud"
        sub={`${cloud.length} managed cloud storage target${cloud.length === 1 ? "" : "s"} · design preview`}
      />

      <Banner kind="pending">
        Helios Cloud backend is not built yet — design preview.
      </Banner>

      {cloud.length === 0 ? (
        <Card>
          <div className="muted fs-13">No Helios Cloud storage targets are configured.</div>
        </Card>
      ) : (
        <div className="grid-auto" style={{ gap: 16, marginTop: 16 }}>
          {cloud.map((t) => {
            const pct = t.capacityBytes > 0 ? (t.usedBytes / t.capacityBytes) * 100 : 0;
            const color =
              pct >= 90 ? "var(--crit)" : pct >= 75 ? "var(--warn)" : "var(--accent)";
            return (
              <Card key={t.id} pad={false}>
                <CardHead
                  title={t.name}
                  sub="Helios Cloud"
                  right={<StatusBadge status={t.status} />}
                />
                <div style={{ padding: "var(--pad)" }}>
                  <div className="between fs-12 muted" style={{ marginBottom: 6 }}>
                    <span>
                      <span className="fw-6 mono tnum" style={{ color: "var(--fg)" }}>
                        {bytes(t.usedBytes)}
                      </span>{" "}
                      used
                    </span>
                    <span className="mono tnum">{Math.round(pct)}%</span>
                  </div>
                  <Meter value={pct} color={color} />
                  <div className="muted fs-12" style={{ marginTop: 6 }}>
                    of {bytes(t.capacityBytes)} capacity
                  </div>
                </div>
              </Card>
            );
          })}
        </div>
      )}
    </>
  );
}

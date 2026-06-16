import { getApi } from "@/lib/api";
import {
  PageHeader,
  Banner,
  Card,
  CardHead,
  StatCard,
  StatusBadge,
  Meter,
  bytes,
} from "@/components/ui";

export default async function Page() {
  const license = await getApi().getLicense();

  const seatPct = license.seats > 0 ? (license.seatsUsed / license.seats) * 100 : 0;
  const quotaPct =
    license.quotaBytes > 0 ? (license.quotaUsedBytes / license.quotaBytes) * 100 : 0;

  return (
    <>
      <PageHeader
        title="Licensing"
        sub="Advisory license posture — verified, parsed, audited; never enforced (S1-T17)."
      />

      <Banner kind="preview">
        Advisory only in Sprint 1 (S1-T17): the license signature is verified
        fail-closed, but expiry/seats/quota/tenant are PARSED and AUDITED, never
        enforced.
      </Banner>

      <div className="grid-auto" style={{ marginTop: 16, gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))" }}>
        <StatCard icon="license" tint="var(--accent)" label="Plan" value={license.plan} />
        <StatCard
          icon="users"
          tint="var(--info)"
          label="Seats"
          value={`${license.seatsUsed}/${license.seats}`}
          sub={`${Math.round(seatPct)}% allocated — not enforced`}
        />
        <StatCard
          icon="storage"
          tint="var(--accent-2)"
          label="Quota"
          value={`${bytes(license.quotaUsedBytes)} / ${bytes(license.quotaBytes)}`}
          sub={`${Math.round(quotaPct)}% used — advisory`}
        />
        <StatCard
          icon="shield"
          tint="var(--warn)"
          label="Status"
          value={<StatusBadge status={license.status} />}
        />
      </div>

      <div style={{ marginTop: 16 }}>
        <Card>
          <CardHead
            title="Quota meter"
            sub="Displayed for visibility — over-quota does not block any backup."
          />
          <div style={{ marginTop: 12 }}>
            <Meter value={quotaPct} color={quotaPct >= 90 ? "var(--warn)" : "var(--ok)"} />
          </div>
        </Card>
      </div>

      <div style={{ marginTop: 16 }}>
        <Card>
          <CardHead title="License details" sub="Parsed from the signed token." />
          <div className="kv" style={{ marginTop: 12 }}>
            <span className="muted">License ID</span>
            <span className="mono fs-13">{license.licenseId}</span>
            <span className="muted">Tenant</span>
            <span className="mono fs-13">{license.tenantId}</span>
            <span className="muted">Plan</span>
            <span className="fw-6">{license.plan}</span>
            <span className="muted">Not after</span>
            <span className="mono fs-13">{license.notAfter}</span>
            <span className="muted">Status</span>
            <span>
              <StatusBadge status={license.status} />
            </span>
          </div>
          <p className="muted fs-12" style={{ marginTop: 12 }}>
            Nothing on this page blocks operations. Expiry, seat, quota, and tenant
            mismatches are surfaced and written to the audit log only.
          </p>
        </Card>
      </div>
    </>
  );
}

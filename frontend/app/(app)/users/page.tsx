import { getApi } from "@/lib/api";
import { Card, CardHead, DataTable, PageHeader, Badge, type Column, type Tone } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ROLES, capabilities } from "@/lib/rbac";
import type { User, Role } from "@/lib/types";

const ROLE_TONE: Record<Role, Tone> = { Owner: "ai", Admin: "ok", Operator: "info", Viewer: "muted" };
const CAPS = ["read", "write", "manage", "admin"] as const;

export default async function UsersPage() {
  const users = await getApi().getUsers();

  const cols: Column<User>[] = [
    { header: "Name", render: (u) => <span className="cell-strong">{u.name}</span> },
    { header: "Email", render: (u) => <span className="muted fs-12">{u.email}</span> },
    { header: "Role", render: (u) => <Badge tone={ROLE_TONE[u.role]}>{u.role}</Badge> },
    { header: "Last active", render: (u) => <span className="mono fs-11">{new Date(u.lastActive).toLocaleString()}</span> },
  ];

  return (
    <>
      <PageHeader title="User Management" sub={`${users.length} members · role-based access (UI gating)`} />

      <Card pad={false}>
        <div style={{ padding: "var(--pad)" }}>
          <DataTable columns={cols} rows={users} getKey={(u) => u.id} />
        </div>
      </Card>

      <Card>
        <CardHead title="Role capability matrix" sub="What each role can do across the console" />
        <table className="table">
          <thead>
            <tr>
              <th style={{ textAlign: "left" }}>Role</th>
              {CAPS.map((c) => (
                <th key={c} style={{ textAlign: "center", textTransform: "capitalize" }}>{c}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {ROLES.map((role) => {
              const caps = capabilities(role);
              return (
                <tr key={role}>
                  <td><Badge tone={ROLE_TONE[role]}>{role}</Badge></td>
                  {CAPS.map((c) => (
                    <td key={c} style={{ textAlign: "center" }}>
                      {caps[c] ? <Icon name="check" size={15} /> : <span className="muted">—</span>}
                    </td>
                  ))}
                </tr>
              );
            })}
          </tbody>
        </table>
        <div className="muted fs-11" style={{ marginTop: 10 }}>
          This gating is UI-only. The SaaS backend is the authoritative source of truth for authorization.
        </div>
      </Card>
    </>
  );
}

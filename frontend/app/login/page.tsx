"use client";

import { useRouter } from "next/navigation";
import { Icon } from "@/components/icons";

// Login is a mock screen (no auth backend in Sprint 1). "Sign in" simply enters the
// console; real session/OIDC wiring lands with the Sprint-2 SaaS backend.
export default function LoginPage() {
  const router = useRouter();

  return (
    <div style={{ minHeight: "100vh", display: "grid", placeItems: "center", padding: 24 }}>
      <div className="card card-pad fade-in" style={{ width: 380, maxWidth: "100%" }}>
        <div className="vcenter" style={{ gap: 12, marginBottom: 18 }}>
          <span className="sb-logo">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src="/assets/helios-mark.svg" alt="" width={22} height={22} />
          </span>
          <div>
            <div className="display fw-7" style={{ fontSize: 20 }}>Helios</div>
            <div className="muted fs-12">Data Protection Platform</div>
          </div>
        </div>

        <form
          onSubmit={(e) => { e.preventDefault(); router.push("/dashboard"); }}
          style={{ display: "grid", gap: 12 }}
        >
          <label className="field">
            <span className="muted fs-12">Email</span>
            <input className="input" type="email" defaultValue="s.delacroix@meridian.example" autoComplete="username" />
          </label>
          <label className="field">
            <span className="muted fs-12">Password</span>
            <input className="input" type="password" defaultValue="••••••••" autoComplete="current-password" />
          </label>
          <button className="btn btn-primary" type="submit" style={{ justifyContent: "center" }}>
            <Icon name="logout" size={15} /> Sign in
          </button>
        </form>

        <div className="muted fs-11" style={{ marginTop: 16, textAlign: "center" }}>
          Mock sign-in · no backend · © Beyz System A.Ş.
        </div>
      </div>
    </div>
  );
}

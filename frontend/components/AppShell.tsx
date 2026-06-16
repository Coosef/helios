"use client";

import { useEffect, useState, type ReactNode } from "react";
import { getApi } from "@/lib/api";
import type { Tenant } from "@/lib/types";
import { Sidebar } from "./Sidebar";
import { Topbar } from "./Topbar";

export function AppShell({ children }: { children: ReactNode }) {
  const [tenants, setTenants] = useState<Tenant[]>([]);

  useEffect(() => {
    let alive = true;
    getApi().getTenants().then((t) => { if (alive) setTenants(t); });
    return () => { alive = false; };
  }, []);

  return (
    <div className="app">
      <Sidebar tenants={tenants} />
      <div className="main">
        <Topbar />
        <main className="content page">{children}</main>
      </div>
    </div>
  );
}

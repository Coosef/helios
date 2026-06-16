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
        {/* .content is the full-width flex/scroll column; .page is the inner padded
            wrapper. They MUST be separate elements — .page has `margin: 0 auto`, and
            on the flex child that collapses it to its content width (the narrow-column
            bug). page-wide removes the max-width cap so dashboards use the full space. */}
        <main className="content">
          <div className="page page-wide">{children}</div>
        </main>
      </div>
    </div>
  );
}

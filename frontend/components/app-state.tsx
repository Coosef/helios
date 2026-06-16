"use client";

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import type { Role } from "@/lib/types";

type Theme = "dark" | "light";

interface AppState {
  role: Role;
  setRole: (r: Role) => void;
  theme: Theme;
  setTheme: (t: Theme) => void;
  tenantId: string;
  setTenantId: (id: string) => void;
}

const Ctx = createContext<AppState | null>(null);

export function AppStateProvider({ children }: { children: ReactNode }) {
  const [role, setRole] = useState<Role>("Owner");
  const [theme, setThemeState] = useState<Theme>("dark");
  const [tenantId, setTenantId] = useState<string>("tnt_meridian");

  useEffect(() => {
    const saved = localStorage.getItem("helios-theme") as Theme | null;
    if (saved === "dark" || saved === "light") setThemeState(saved);
  }, []);

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("helios-theme", theme);
  }, [theme]);

  return (
    <Ctx.Provider value={{ role, setRole, theme, setTheme: setThemeState, tenantId, setTenantId }}>
      {children}
    </Ctx.Provider>
  );
}

export function useAppState(): AppState {
  const c = useContext(Ctx);
  if (!c) throw new Error("useAppState must be used within AppStateProvider");
  return c;
}

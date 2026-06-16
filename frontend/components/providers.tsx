"use client";

import type { ReactNode } from "react";
import { I18nProvider } from "@/lib/i18n";
import { AppStateProvider } from "./app-state";

export function Providers({ children }: { children: ReactNode }) {
  return (
    <I18nProvider>
      <AppStateProvider>{children}</AppStateProvider>
    </I18nProvider>
  );
}

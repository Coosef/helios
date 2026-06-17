// Shared dashboard/executive composition panels. Server-safe (no hooks, no state) —
// they render the client chart primitives but are themselves plain components, so the
// resilience-hero and donut-breakdown markup lives in ONE place instead of being copied
// across the two marquee screens.

import type { ReactNode } from "react";
import type { Resilience } from "@/lib/types";
import { Meter, Swatch } from "./ui";
import { Donut, Gauge } from "./charts";

/** Score → tone color, shared by both screens' resilience gauges. */
export function resilienceColor(score: number): string {
  return score >= 85 ? "var(--ok)" : score >= 70 ? "var(--warn)" : "var(--crit)";
}

/** Resilience score gauge beside its weighted pillar breakdown. */
export function ResilienceHero({ resilience, gaugeSize = 160 }: { resilience: Resilience; gaugeSize?: number }) {
  return (
    <div className="hero-split" style={{ marginTop: 8 }}>
      <Gauge value={resilience.score} size={gaugeSize} color={resilienceColor(resilience.score)} label={resilience.score} sub={`Grade ${resilience.grade}`} />
      <div className="stack" style={{ width: "100%" }}>
        <div className="vcenter fs-12" style={{ gap: 8 }}>
          <span className={resilience.delta >= 0 ? "delta-up" : "delta-down"} style={{ fontWeight: 600 }}>
            {resilience.delta >= 0 ? "▲" : "▼"} {Math.abs(resilience.delta)} pts
          </span>
          <span className="muted">vs. last month</span>
        </div>
        {resilience.pillars.map((p) => (
          <div key={p.label} className="pillar">
            <span className="muted fs-12">{p.label}</span>
            <span className="mono fs-12">{p.score}</span>
            <div style={{ gridColumn: "1 / -1" }}><Meter value={p.score} color={p.color} thin /></div>
          </div>
        ))}
      </div>
    </div>
  );
}

export interface BreakdownSegment {
  label: string;
  value: number;
  color: string;
}

/** Donut with a centered headline value + a labeled legend listing each segment. */
export function DonutBreakdown({ segments, size = 132, centerMain, centerSub, centerColor }: {
  segments: BreakdownSegment[]; size?: number; centerMain: ReactNode; centerSub?: ReactNode; centerColor?: string;
}) {
  return (
    <div className="hero-split" style={{ marginTop: 8 }}>
      <Donut segments={segments.map((s) => ({ value: s.value, color: s.color, label: s.label }))} size={size}>
        <div>
          <div className="display" style={{ fontSize: 24, fontWeight: 600, lineHeight: 1, color: centerColor }}>{centerMain}</div>
          {centerSub && <div className="muted fs-11">{centerSub}</div>}
        </div>
      </Donut>
      <div className="stack" style={{ width: "100%" }}>
        {segments.map((s) => (
          <div key={s.label} className="between fs-12">
            <span className="vcenter" style={{ gap: 8 }}><Swatch color={s.color} /><span className="muted">{s.label}</span></span>
            <span className="mono">{s.value}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

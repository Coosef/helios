"use client";

// Hand-built SVG chart primitives ported from the design package (Backup/components.jsx).
// Pure presentational client components (no data fetching, no state, no window globals).
// Gradient IDs come from React useId() — deterministic + SSR-safe (no Math.random, so no
// hydration mismatch). Reference only; nothing is imported from Backup/ at runtime.

import { useId, type ReactNode } from "react";

function smoothPath(pts: Array<[number, number]>): string {
  if (pts.length < 2) return "";
  let d = `M ${pts[0][0]},${pts[0][1]}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i], p1 = pts[i + 1];
    const mx = (p0[0] + p1[0]) / 2;
    d += ` C ${mx},${p0[1]} ${mx},${p1[1]} ${p1[0]},${p1[1]}`;
  }
  return d;
}

export interface Series {
  data: number[];
  color: string;
  name?: string;
}

/** Multi-series area chart with gridlines + y-axis ticks + x labels. */
export function AreaChart({ series, w = 720, h = 220, labels = [], yMax }: {
  series: Series[]; w?: number; h?: number; labels?: string[]; yMax?: number;
}) {
  const uid = useId();
  const flat = series.flatMap((s) => s.data);
  // Guard the empty-series case: Math.max() → -Infinity slips past a plain `|| 1`.
  const max = yMax || (flat.length ? Math.max(...flat) * 1.15 : 1) || 1;
  const pad = { l: 38, r: 10, t: 12, b: 24 };
  const iw = w - pad.l - pad.r, ih = h - pad.t - pad.b;
  const n = series[0]?.data.length ?? 0;
  const X = (i: number) => pad.l + (n > 1 ? (i / (n - 1)) * iw : 0);
  const Y = (v: number) => pad.t + ih - (v / max) * ih;
  const ticks = 4;
  const labelStep = labels.length > 1 ? Math.floor((n - 1) / (labels.length - 1)) : 0;

  return (
    <svg width="100%" viewBox={`0 0 ${w} ${h}`} style={{ display: "block" }} role="img" aria-label="area chart">
      {Array.from({ length: ticks + 1 }).map((_, i) => {
        const y = pad.t + (i / ticks) * ih;
        const val = Math.round(max - (i / ticks) * max);
        return (
          <g key={i}>
            <line x1={pad.l} y1={y} x2={w - pad.r} y2={y} stroke="var(--grid-line)" />
            <text x={pad.l - 8} y={y + 3} fontSize="11" fill="var(--text-2)" textAnchor="end" className="mono">{val}</text>
          </g>
        );
      })}
      {labels.map((lb, i) => (
        <text key={i} x={X(i * labelStep)} y={h - 6} fontSize="11" fill="var(--text-2)" textAnchor="middle">{lb}</text>
      ))}
      {series.map((s, si) => {
        const pts = s.data.map((v, i) => [X(i), Y(v)] as [number, number]);
        const line = smoothPath(pts);
        const gid = `${uid}-ac-${si}`;
        return (
          <g key={si}>
            <defs>
              <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stopColor={s.color} stopOpacity={0.22} />
                <stop offset="1" stopColor={s.color} stopOpacity="0" />
              </linearGradient>
            </defs>
            <path d={`${line} L ${X(n - 1)},${pad.t + ih} L ${X(0)},${pad.t + ih} Z`} fill={`url(#${gid})`} />
            <path d={line} fill="none" stroke={s.color} strokeWidth={2.2} strokeLinecap="round" />
          </g>
        );
      })}
    </svg>
  );
}

export interface DonutSegment {
  value: number;
  color: string;
  label?: string;
}

/** Ring chart with optional centered children. */
export function Donut({ segments, size = 120, thickness = 14, children }: {
  segments: DonutSegment[]; size?: number; thickness?: number; children?: ReactNode;
}) {
  const total = segments.reduce((s, x) => s + x.value, 0) || 1;
  const r = (size - thickness) / 2;
  const c = 2 * Math.PI * r;
  let off = 0;
  return (
    <div className="gauge-wrap" style={{ width: size, height: size }}>
      <svg width={size} height={size} style={{ transform: "rotate(-90deg)" }}>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--track)" strokeWidth={thickness} />
        {segments.map((s, i) => {
          const len = (s.value / total) * c;
          const el = (
            <circle key={i} cx={size / 2} cy={size / 2} r={r} fill="none" stroke={s.color}
              strokeWidth={thickness} strokeDasharray={`${len} ${c - len}`} strokeDashoffset={-off} strokeLinecap="round" />
          );
          off += len;
          return el;
        })}
      </svg>
      <div className="gauge-center">{children}</div>
    </div>
  );
}

/** Semicircle score gauge. */
export function Gauge({ value, max = 100, size = 160, color = "var(--ok)", label, sub }: {
  value: number; max?: number; size?: number; color?: string; label?: ReactNode; sub?: ReactNode;
}) {
  const r = size / 2 - 14;
  const cx0 = size / 2, cy0 = size / 2 + 6;
  const a0 = Math.PI, a1 = 0;
  const frac = Math.max(0, Math.min(1, value / max));
  const ang = a0 + (a1 - a0) * frac;
  const pt = (a: number): [number, number] => [cx0 + r * Math.cos(a), cy0 + r * Math.sin(a) * -1];
  const [sx, sy] = pt(a0), [ex, ey] = pt(a1), [px, py] = pt(ang);
  // The gauge is a semicircle, so every value arc sweeps ≤ 180° → large-arc-flag is
  // always 0. The full track (exactly 180°) uses 1.
  const arc = (x1: number, y1: number, x2: number, y2: number, large: number) =>
    `M ${x1} ${y1} A ${r} ${r} 0 ${large} 1 ${x2} ${y2}`;
  return (
    <div className="gauge-wrap" style={{ width: size, height: size / 2 + 26 }}>
      <svg width={size} height={size / 2 + 26}>
        <path d={arc(sx, sy, ex, ey, 1)} fill="none" stroke="var(--track)" strokeWidth="11" strokeLinecap="round" />
        <path d={arc(sx, sy, px, py, 0)} fill="none" stroke={color} strokeWidth="11" strokeLinecap="round" />
      </svg>
      <div style={{ position: "absolute", inset: 0, top: 14, display: "grid", placeItems: "center", textAlign: "center" }}>
        <div>
          <div className="display" style={{ fontSize: 30, fontWeight: 600, lineHeight: 1, color }}>{label ?? value}</div>
          {sub && <div className="muted" style={{ fontSize: 11, marginTop: 4 }}>{sub}</div>}
        </div>
      </div>
    </div>
  );
}

export interface CapacitySegment {
  pct: number;
  color: string;
  label?: string;
}

/** Segmented horizontal capacity bar. */
export function CapacityBar({ segments }: { segments: CapacitySegment[] }) {
  return (
    <div className="cap-bar">
      {segments.map((s, i) => <span key={i} style={{ width: s.pct + "%", background: s.color }} title={s.label} />)}
    </div>
  );
}

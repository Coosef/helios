"use client";

// Route-segment error boundary. Without this, a client-side render/hydration crash
// in any page unmounts the React tree and the browser shows a BLANK page with only a
// console error. This turns that failure mode into a visible, recoverable message.

import { useEffect } from "react";

export default function Error({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  useEffect(() => {
    // Surface in the console for debugging (no secrets here — UI-only mock app).
    console.error("Helios console runtime error:", error);
  }, [error]);

  return (
    <div style={{ minHeight: "60vh", display: "grid", placeItems: "center", padding: 24 }}>
      <div className="card card-pad" style={{ maxWidth: 460, textAlign: "center" }}>
        <div className="display fw-7" style={{ fontSize: 18, marginBottom: 8 }}>Something went wrong</div>
        <div className="muted fs-13" style={{ marginBottom: 16 }}>
          A screen failed to render. This is the Helios console (UI Sprint 1, mock data) — no data was lost.
          {error?.digest && <span className="mono fs-11" style={{ display: "block", marginTop: 8 }}>ref: {error.digest}</span>}
        </div>
        <button className="btn btn-primary" onClick={reset} style={{ justifyContent: "center" }}>Try again</button>
      </div>
    </div>
  );
}

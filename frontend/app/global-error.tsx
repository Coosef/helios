"use client";

// Last-resort boundary for a crash in the root layout itself. It must render its own
// <html>/<body> because it replaces the root layout. Guarantees the user never sees a
// completely blank document with no explanation.

export default function GlobalError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <html lang="en" data-theme="dark">
      <body style={{ margin: 0, fontFamily: "system-ui, sans-serif", background: "#0b1220", color: "#eaf0f9" }}>
        <div style={{ minHeight: "100vh", display: "grid", placeItems: "center", padding: 24 }}>
          <div style={{ maxWidth: 460, textAlign: "center" }}>
            <div style={{ fontSize: 18, fontWeight: 700, marginBottom: 8 }}>Helios console failed to load</div>
            <div style={{ opacity: 0.7, fontSize: 13, marginBottom: 16 }}>
              The application shell crashed. Reload, or run a clean rebuild (see frontend/README.md).
              {error?.digest && <span style={{ display: "block", marginTop: 8, fontFamily: "monospace" }}>ref: {error.digest}</span>}
            </div>
            <button onClick={reset} style={{ padding: "8px 16px", borderRadius: 8, border: "1px solid #324460", background: "#1a2740", color: "#eaf0f9", cursor: "pointer" }}>
              Reload
            </button>
          </div>
        </div>
      </body>
    </html>
  );
}

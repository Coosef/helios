// Route smoke + hygiene tests. Static (no browser): verify the key route modules
// exist and export a default component, and that NO source file imports runtime code
// from the original design package (../Backup). The authoritative "does it render"
// gate is `npm run build` (prerenders all routes); this guards structure + the
// Backup-isolation rule cheaply on every `npm test`.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const FE = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

function read(rel: string): string {
  return readFileSync(join(FE, rel), "utf8");
}

const KEY_ROUTES = [
  "app/page.tsx", // homepage (redirects to /login)
  "app/login/page.tsx", // login
  "app/(app)/layout.tsx", // shell
  "app/(app)/dashboard/page.tsx", // dashboard
  "app/(app)/executive/page.tsx", // executive overview
];

test("key route modules exist and export a default", () => {
  for (const r of KEY_ROUTES) {
    const src = read(r);
    assert.ok(src.length > 0, `${r} is non-empty`);
    assert.match(src, /export default/, `${r} exports a default`);
  }
});

test("homepage redirects to /login", () => {
  assert.match(read("app/page.tsx"), /redirect\(\s*["']\/login["']\s*\)/);
});

// Walk app/ + components/ + lib/ and assert no source IMPORTS from ../Backup.
function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name === ".next") continue;
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (/\.(ts|tsx|css)$/.test(name)) out.push(p);
  }
  return out;
}

test("no source imports runtime code from the Backup/ design package", () => {
  const importRe = /\b(?:import|require)\b[^\n;]*['"][^'"]*Backup[^'"]*['"]/;
  for (const root of ["app", "components", "lib"]) {
    for (const file of walk(join(FE, root))) {
      const src = readFileSync(file, "utf8");
      assert.ok(!importRe.test(src), `${file} must not import from Backup/`);
    }
  }
});

test("error boundaries exist (no silent blank page on a client crash)", () => {
  assert.match(read("app/error.tsx"), /export default/);
  assert.match(read("app/global-error.tsx"), /export default/);
});

test("shell wraps content in a full-width .content > .page layout (no narrow-column collapse)", () => {
  const shell = read("components/AppShell.tsx");
  // .content (flex/scroll) and .page must be SEPARATE elements — combining them put
  // margin:auto on the flex child and collapsed it to content width.
  assert.match(shell, /className="content"/, "AppShell has a .content scroll container");
  assert.match(shell, /className="page page-wide"/, "AppShell wraps children in a full-width .page");
  assert.doesNotMatch(shell, /className="content page"/, "must NOT combine .content and .page on one element");
});

test("dashboard + executive use the responsive grid layout helpers", () => {
  assert.match(read("app/(app)/dashboard/page.tsx"), /stat-grid/);
  assert.match(read("app/(app)/executive/page.tsx"), /stat-grid|cols-2/);
});

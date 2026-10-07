import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const APP_DIR = join(process.cwd(), "src/app/[locale]/(app)");

function pages(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) return pages(full);
    return name === "page.tsx" ? [full] : [];
  });
}

describe("(app) route group", () => {
  const files = pages(APP_DIR);

  it("has pages to guard", () => {
    expect(files.length).toBeGreaterThanOrEqual(6);
  });

  // The (app) layout renders children without a session and does not redirect (it cannot know the
  // path), so each page is the only guard: it must call requireSession before anything else.
  it.each(files.map((f) => [f.slice(APP_DIR.length)]))("%s calls requireSession", (rel) => {
    const src = readFileSync(join(APP_DIR, rel), "utf8");
    expect(src).toMatch(/await requireSession\(/);
  });
});

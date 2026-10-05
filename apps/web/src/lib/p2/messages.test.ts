import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { DEMO_SAMPLES } from "../demo-samples";
import { errorMessageKey } from "./display";

type Tree = { [k: string]: string | Tree };
const load = (l: string) => JSON.parse(readFileSync(join(process.cwd(), `messages/${l}.json`), "utf8")) as Record<string, Tree>;

function leaves(t: Tree, prefix = ""): string[] {
  return Object.entries(t).flatMap(([k, v]) => (typeof v === "string" ? [`${prefix}${k}`] : leaves(v, `${prefix}${k}.`)));
}

describe("P2Invoices messages", () => {
  const en = load("en");
  const ar = load("ar");

  it("have identical key sets in Arabic and English", () => {
    expect(leaves(ar.P2Invoices).sort()).toEqual(leaves(en.P2Invoices).sort());
    expect(leaves(ar.Status).sort()).toEqual(leaves(en.Status).sort());
  });

  it("have no empty strings", () => {
    for (const m of [en, ar]) for (const k of leaves(m.P2Invoices)) expect(k).toBeTruthy();
    const flat = (t: Tree): string[] => Object.values(t).flatMap((v) => (typeof v === "string" ? [v] : flat(v)));
    for (const m of [en, ar]) for (const v of flat(m.P2Invoices)) expect(v.trim()).not.toBe("");
  });

  it("cover every demo sample, its expected outcome and every mapped error", () => {
    const keys = new Set(leaves(en.P2Invoices));
    for (const s of DEMO_SAMPLES) {
      expect(keys.has(`${s.i18nKey}.title`)).toBe(true);
      expect(keys.has(`${s.i18nKey}.description`)).toBe(true);
      expect(keys.has(`samples.expected.${s.expected}`)).toBe(true);
    }
    for (const c of ["stale_payload", "not_ready", "generic", "unauthenticated"]) expect(keys.has(`errors.${errorMessageKey(c)}`)).toBe(true);
  });
});

import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { DEMO_SAMPLES, demoSample } from "./index";

const DIR = join(process.cwd(), "src/lib/demo-samples");
const files = readdirSync(DIR).filter((f) => f.endsWith(".json")).sort();
const parsed = (f: string) => JSON.parse(readFileSync(join(DIR, f), "utf8")) as Record<string, unknown>;
const DECIMAL = /^-?\d+(\.\d+)?$/;

describe("demo samples", () => {
  it("ships the four canonical invoices", () => {
    expect(files).toEqual(["01-valid.json", "02-totals-mismatch.json", "03-bad-trn.json", "04-missing-fields.json"]);
    expect(DEMO_SAMPLES.map((s) => s.id)).toEqual(["valid", "totalsMismatch", "badTrn", "missingFields"]);
  });

  it("exposes each file's exact content, unmodified", () => {
    DEMO_SAMPLES.forEach((s, i) => expect(s.body).toEqual(parsed(files[i])));
  });

  const full = new Set(Object.keys(parsed("01-valid.json")));

  it.each(files)("%s is a JSON object with the keys api-go accepts", (f) => {
    const body = parsed(f);
    expect(typeof body).toBe("object");
    expect(Array.isArray(body)).toBe(false);
    // The API rejects unknown fields and requires decimal amounts; the official example is the full key set.
    for (const k of Object.keys(body)) expect(full.has(k), `${f}: unexpected key ${k}`).toBe(true);
    for (const k of ["total_amount", "vat_amount"]) expect(String(body[k])).toMatch(DECIMAL);
    expect(typeof body.invoice_number).toBe("string");
    expect(typeof body.seller_trn).toBe("string");
  });

  it("keeps the official example's keys complete and its date as it is", () => {
    for (const k of ["invoice_number", "issue_date", "seller_trn", "buyer_trn", "currency", "total_amount", "vat_amount", "seller", "buyer", "totals"]) {
      expect(full.has(k), k).toBe(true);
    }
    expect(DEMO_SAMPLES[0].body.issue_date).toBe("2025-02-06");
  });

  it("every sample has an i18n key and an expected outcome", () => {
    for (const s of DEMO_SAMPLES) {
      expect(s.i18nKey).toBe(`samples.${s.id}`);
      expect(["validated", "has_issues"]).toContain(s.expected);
    }
    expect(DEMO_SAMPLES[0].expected).toBe("validated");
    expect(demoSample("totalsMismatch")?.expectedRuleId).toBe("ibr-co-16");
    expect(demoSample("nope")).toBeUndefined();
  });
});

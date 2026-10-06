// Run: node --test e2e/seed/   (Node 22.18+ strips the types itself; no build step)
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { FIRM_A_CLIENTS, FIRM_B_CLIENTS, syntheticTrn } from "./data.ts";
import { buildBatch, planFirmA, planFirmB } from "./generate.ts";

const sample = JSON.parse(readFileSync(new URL("../../demo/samples/01-valid.json", import.meta.url), "utf8"));
const dec = (s: string): number => Math.round(Number(s) * 100);

test("TRNs are 15 digits, start with 1, end with 03, and are unique", () => {
  const all = [...FIRM_A_CLIENTS, ...FIRM_B_CLIENTS].map((c) => c.trn);
  for (const t of all) assert.match(t, /^1\d{12}03$/);
  assert.equal(new Set(all).size, all.length);
  assert.equal(syntheticTrn("x"), syntheticTrn("x"));
});

test("firm A plan: 30 unique invoices with the documented spread", () => {
  const plan = planFirmA(FIRM_A_CLIENTS);
  assert.equal(plan.length, 30);
  assert.equal(new Set(plan.map((p) => p.invoiceNumber)).size, 30);
  const count = (f: (p: (typeof plan)[number]) => boolean) => plan.filter(f).length;
  assert.equal(count((p) => p.kind === "valid"), 16);
  assert.equal(count((p) => p.kind === "mismatch"), 6);
  assert.equal(count((p) => p.kind === "bad_trn"), 4);
  assert.equal(count((p) => p.kind === "missing"), 4);
  assert.equal(count((p) => p.action === "approve" || p.action === "export"), 10);
  assert.equal(count((p) => p.action === "export"), 5);
  assert.equal(count((p) => p.action === "correct"), 2);
  assert.ok(plan.every((p) => p.issueDate.startsWith("2026-")));
});

test("generation is deterministic", () => {
  const a = buildBatch(sample, FIRM_A_CLIENTS, planFirmA(FIRM_A_CLIENTS));
  const b = buildBatch(sample, FIRM_A_CLIENTS, planFirmA(FIRM_A_CLIENTS));
  assert.deepEqual(a, b);
});

test("valid invoices have internally consistent totals", () => {
  for (const { spec, payload: p } of buildBatch(sample, FIRM_A_CLIENTS, planFirmA(FIRM_A_CLIENTS))) {
    if (spec.kind !== "valid") continue;
    const lea = p.lines.reduce((s: number, l: { net_amount: string }) => s + dec(l.net_amount), 0);
    assert.equal(dec(p.totals.line_extension_amount), lea, spec.invoiceNumber);
    const taxable = lea - dec(p.totals.allowance_total_amount) + dec(p.totals.charge_total_amount);
    assert.equal(dec(p.totals.tax_exclusive_amount), taxable, spec.invoiceNumber);
    assert.equal(dec(p.vat_amount), dec(p.tax_breakdown[0].tax_amount));
    assert.equal(dec(p.total_amount), taxable + dec(p.vat_amount));
    assert.equal(dec(p.totals.payable_amount), dec(p.total_amount) + dec(p.totals.rounding_amount));
    assert.equal(p.seller_trn, spec.client.trn);
  }
});

test("seeded problems break exactly what they claim", () => {
  for (const { spec, payload: p } of buildBatch(sample, FIRM_A_CLIENTS, planFirmA(FIRM_A_CLIENTS))) {
    if (spec.kind === "mismatch") {
      assert.notEqual(dec(p.totals.payable_amount), dec(p.total_amount) + dec(p.totals.rounding_amount));
    }
    if (spec.kind === "bad_trn") assert.doesNotMatch(p.seller_trn, /^\d{15}$/);
    if (spec.kind === "missing") assert.equal(p.buyer.name, "");
  }
});

test("firm B plan is small and shares no invoice numbers or TRNs with firm A", () => {
  const b = planFirmB(FIRM_B_CLIENTS);
  assert.equal(b.length, 3);
  const a = new Set(planFirmA(FIRM_A_CLIENTS).map((p) => p.invoiceNumber));
  assert.ok(b.every((p) => !a.has(p.invoiceNumber)));
  const aTrns = new Set(FIRM_A_CLIENTS.map((c) => c.trn));
  assert.ok(FIRM_B_CLIENTS.every((c) => !aTrns.has(c.trn)));
});

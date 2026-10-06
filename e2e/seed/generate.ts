/**
 * Deterministic invoice generator for scripts/seed-demo.sh.
 *
 * Every invoice is derived from the canonical sample demo/samples/01-valid.json (the official PINT-AE
 * example): the same structure, with the parties, dates, numbers, lines and totals replaced by values
 * computed here. Same input, same output: the random stream is seeded by the invoice number, and all
 * money is integer fils (cents) so the totals stay exactly consistent. Nothing here calls the network.
 */
import { createHash } from "node:crypto";
import { BUYERS, CITY, PRODUCTS } from "./data.ts";
import type { Emirate, SeedBuyer, SeedClient } from "./data.ts";

export type Kind = "valid" | "mismatch" | "bad_trn" | "missing";
/** What the seed does to the invoice after validation, through the real API. */
export type Action = "none" | "approve" | "export" | "correct";

export type InvoiceSpec = {
  invoiceNumber: string;
  client: SeedClient;
  buyer: SeedBuyer;
  kind: Kind;
  action: Action;
  issueDate: string;
};

type Json = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any

// ---- small deterministic helpers ---------------------------------------------------------------

function fnv1a(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h >>> 0;
}

/** mulberry32: a tiny seeded PRNG returning floats in [0, 1). */
export function rng(seed: string): () => number {
  let a = fnv1a(seed);
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const between = (r: () => number, lo: number, hi: number): number => lo + Math.floor(r() * (hi - lo + 1));
const pick = <T>(r: () => number, xs: readonly T[]): T => xs[Math.floor(r() * xs.length)];

/** Round-half-up of num/den for non-negative integers. */
const divRound = (num: number, den: number): number => Math.floor((2 * num + den) / (2 * den));

/** Integer fils to a decimal string without trailing zeros ("11175.5", "10486", "262.15"). */
export function money(fils: number): string {
  const sign = fils < 0 ? "-" : "";
  const abs = Math.abs(fils);
  const whole = Math.floor(abs / 100);
  const frac = String(abs % 100).padStart(2, "0").replace(/0+$/, "");
  return `${sign}${whole}${frac ? `.${frac}` : ""}`;
}

/** Tenths of a unit ("49" -> "4.9", "50" -> "5"). */
const tenths = (n: number): string => money(n * 10);

const pctString = (pctTenths: number): string => tenths(pctTenths);

/** `base * pct%`, where pct is in tenths of a percent, rounded to whole fils. */
const pctOf = (baseFils: number, pctTenths: number): number => divRound(baseFils * pctTenths, 1000);

function isoDate(base: string, plusDays: number): string {
  const d = new Date(`${base}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + plusDays);
  return d.toISOString().slice(0, 10);
}

function uuidFrom(seed: string): string {
  const h = createHash("sha1").update(`seed-demo|${seed}`).digest("hex");
  const variant = ((parseInt(h.slice(16, 18), 16) & 0x3f) | 0x80).toString(16).padStart(2, "0");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-5${h.slice(13, 16)}-${variant}${h.slice(18, 20)}-${h.slice(20, 32)}`;
}

const AUTHORITY: Record<Emirate, string> = {
  AUH: "Abu Dhabi Department of Economic Development",
  DXB: "Dubai Department of Economy and Tourism",
  SHJ: "Sharjah Economic Development Department",
  AJM: "Ajman Department of Economic Development",
  RAK: "Ras Al Khaimah Economic Zone",
  UAQ: "Umm Al Quwain Department of Economic Development",
  FUJ: "Fujairah Free Zone Authority",
};

const DIAL: Record<Emirate, string> = { AUH: "2", DXB: "4", SHJ: "6", AJM: "6", RAK: "7", UAQ: "6", FUJ: "9" };

// ---- plan -------------------------------------------------------------------------------------

/**
 * The 30-invoice plan for firm A: one entry per invoice, in a fixed order. Counts: 16 valid (5 approved and
 * exported, 5 approved only, 6 left validated), 6 totals mismatches (2 corrected through the API, 4 left with
 * issues), 4 bad seller TRNs and 4 with missing buyer fields. Codes: Vx approve+export, Va approve, V valid,
 * Mc mismatch+correct, M mismatch, T bad TRN, F missing fields.
 */
const PLAN_A = "Vx Va V Mc Vx F Va V T Va V Vx M V Va T Vx V Mc F V Va Vx M T F M F M T".split(" ");

const CODES: Record<string, { kind: Kind; action: Action }> = {
  Vx: { kind: "valid", action: "export" },
  Va: { kind: "valid", action: "approve" },
  V: { kind: "valid", action: "none" },
  Mc: { kind: "mismatch", action: "correct" },
  M: { kind: "mismatch", action: "none" },
  T: { kind: "bad_trn", action: "none" },
  F: { kind: "missing", action: "none" },
};

export function planFor(clients: SeedClient[], codes: string[]): InvoiceSpec[] {
  return codes.map((c, g) => {
    const client = clients[g % clients.length];
    const seq = Math.floor(g / clients.length);
    const r = rng(`${client.code}|${g}`);
    const number = 100 + seq * 17 + (g % clients.length) * 3;
    const { kind, action } = CODES[c];
    return {
      invoiceNumber: `${client.code}-2026-${String(number).padStart(4, "0")}`,
      client,
      buyer: BUYERS[(g * 5 + seq) % BUYERS.length],
      kind,
      action,
      issueDate: isoDate("2026-01-12", g * 8 + between(r, 0, 4)),
    };
  });
}

export const planFirmA = (clients: SeedClient[]): InvoiceSpec[] => planFor(clients, PLAN_A);
/** Firm B proves isolation only: two valid invoices (one approved) and one totals mismatch. */
export const planFirmB = (clients: SeedClient[]): InvoiceSpec[] => planFor(clients, ["Va", "M", "V"]);

// ---- invoice ----------------------------------------------------------------------------------

type Line = { net: number; json: Json };

function buildLine(r: () => number, id: number, product: (typeof PRODUCTS)[number], issueDate: string): Line {
  const qty = between(r, 1, 30) * 10; // multiples of 10 keep every product exact; totals land in the 1k-100k AED range
  const netTenths = pick(r, product.priceTenths);
  const discTenths = between(r, 1, 4);
  const base = qty * netTenths * 10;
  const allowPct = pick(r, [20, 30, 50]);
  const chargePct = pick(r, [50, 80, 100]);
  const allowance = pctOf(base, allowPct);
  const charge = pctOf(base, chargePct);
  const net = base - allowance + charge;
  const vat = pctOf(net, 50);
  const json: Json = {
    id: String(id),
    note: "Goods and services as agreed",
    quantity: String(qty),
    unit_code: product.unit,
    net_amount: money(net),
    order_reference: "PO-PLACEHOLDER",
    order_line_reference: String(id),
    period: { start_date: isoDate(issueDate, -14), end_date: isoDate(issueDate, -1) },
    allowances_charges: [
      {
        amount: money(allowance),
        base_amount: money(base),
        percentage: pctString(allowPct),
        reason: "Discount",
        reason_code: "95",
      },
      {
        is_charge: true,
        amount: money(charge),
        base_amount: money(base),
        percentage: pctString(chargePct),
        reason: "Technical Modification",
        reason_code: "AAC",
      },
    ],
    price: {
      net_price: tenths(netTenths),
      discount: tenths(discTenths),
      gross_price: tenths(netTenths + discTenths),
      base_quantity: "1",
      base_quantity_unit_code: product.unit,
    },
    tax: { code: "S", rate: "5", tax_scheme: "VAT" },
    amount_aed: money(net + vat),
    vat_amount_aed: money(vat),
    item: {
      name: product.name,
      description: product.description,
      seller_item_id: `SKU-${String(fnv1a(product.name) % 100000).padStart(5, "0")}`,
      buyer_item_id: `B-${String(fnv1a(product.description) % 10000).padStart(4, "0")}`,
      origin_country: "AE",
      attributes: [{ name: "Item details", value: product.description }],
    },
  };
  return { net, json };
}

/**
 * Builds one canonical invoice from the valid sample. `sample` is the parsed demo/samples/01-valid.json.
 * Valid kinds produce a document the validator accepts with no errors; the other kinds then break exactly
 * the thing their name says (see the Kind type).
 */
export function buildInvoice(sample: Json, spec: InvoiceSpec, productIndex: number): Json {
  const r = rng(spec.invoiceNumber);
  const inv: Json = structuredClone(sample);
  const { client, buyer } = spec;
  const issue = spec.issueDate;

  inv.invoice_number = spec.invoiceNumber;
  inv.issue_date = issue;
  inv.seller_trn = client.trn;
  inv.buyer_trn = buyer.trn;
  inv.uuid = uuidFrom(spec.invoiceNumber);
  inv.issue_time = `${String(between(r, 8, 17)).padStart(2, "0")}:${String(between(r, 0, 59)).padStart(2, "0")}:00+04:00`;
  inv.tax_point_date = isoDate(issue, -between(r, 1, 7));
  inv.payment_due_date = isoDate(issue, pick(r, [14, 30, 45]));
  inv.invoicing_period = { start_date: isoDate(issue, -14), end_date: isoDate(issue, -1) };

  const po = `PO-${client.emirate}-${between(r, 100, 999)}`;
  inv.references = {
    buyer_reference: po,
    project_reference: pick(r, ["Regular work", "Q1 supply", "Monthly retainer", "Site delivery"]),
    purchase_order_reference: po,
    sales_order_reference: `SO-${between(r, 1000, 9999)}`,
    despatch_advice_reference: `DN-${between(r, 1000, 9999)}`,
    tender_or_lot_reference: po,
    buyer_accounting_reference: "Regular sales",
  };
  inv.preceding_invoices = [{ id: `${client.code}-2025-${between(r, 100, 999)}`, issue_date: isoDate(issue, -40) }];
  inv.supporting_documents = [
    { reference: po, external_uri: `https://files.example/po/${po}.pdf` },
    { reference: po, attachment: { mime_code: "application/pdf", filename: `${po}.pdf` } },
  ];
  inv.payment_terms = [{ note: pick(r, ["Within a week", "Net 14 days", "Net 30 days", "Net 45 days"]) }];

  const licence = (s: string) => String(fnv1a(s) % 9000000 + 1000000);
  const party = (name: string, trn: string, city: string, emirate: Emirate, own: boolean): Json => ({
    name,
    trading_name: name.split(" ").slice(0, 2).join(" "),
    legal_registration: { id: licence(`${trn}|tl`), type: "TL", authority_name: AUTHORITY[emirate] },
    ...(own ? { additional_legal_information: "Merchant" } : {}),
    electronic_address: { id: trn, scheme_id: "0235" },
    postal_address: {
      line1: `${between(r, 1, 99)} ${pick(r, ["Sheikh Zayed Road", "Al Wasl Street", "Industrial Area 12", "Corniche Road", "Airport Road"])}`,
      city,
      country_subdivision: emirate,
      country_code: "AE",
    },
    contact: {
      name: "Accounts Department",
      telephone: `+971 ${DIAL[emirate]} ${between(r, 200, 899)} ${between(r, 1000, 9999)}`,
      email: `accounts@${own ? client.code.toLowerCase() : "buyer"}.example`,
    },
  });
  inv.seller = party(client.name, client.trn, CITY[client.emirate], client.emirate, true);
  inv.buyer = party(buyer.name, buyer.trn, buyer.city, buyer.emirate, false);
  delete inv.payee;
  inv.payment_instructions = [
    { means_code: "55", means_text: "Debit Card", card: { primary_account_number: "XXXXXXXXXXXX1234", holder_name: buyer.name, network_id: "VISA" } },
  ];

  // Lines and totals: all integer fils.
  const lineCount = between(r, 1, 3);
  const lines: Line[] = [];
  for (let i = 0; i < lineCount; i++) {
    lines.push(buildLine(r, i + 1, PRODUCTS[(productIndex + i * 3) % PRODUCTS.length], issue));
  }
  for (const l of lines) l.json.order_reference = po;
  inv.lines = lines.map((l) => l.json);

  const lea = lines.reduce((s, l) => s + l.net, 0);
  const hAllowPct = pick(r, [10, 20, 25, 50]);
  const hChargePct = pick(r, [10, 20, 40, 50]);
  const hAllow = pctOf(lea, hAllowPct);
  const hCharge = pctOf(lea, hChargePct);
  const taxable = lea - hAllow + hCharge;
  const vat = pctOf(taxable, 50);
  const total = taxable + vat;
  const payable = Math.round(total / 50) * 50; // nearest 0.50 AED, as in the official example
  const S = { code: "S", rate: "5", tax_scheme: "VAT" };
  inv.allowances_charges = [
    { amount: money(hAllow), base_amount: money(lea), percentage: pctString(hAllowPct), reason: "Special Rebate", reason_code: "100", tax_category: S },
    { is_charge: true, amount: money(hCharge), base_amount: money(lea), percentage: pctString(hChargePct), reason: "Rush Delivery", reason_code: "AAT", tax_category: S },
  ];
  inv.totals = {
    line_extension_amount: money(lea),
    allowance_total_amount: money(hAllow),
    charge_total_amount: money(hCharge),
    tax_exclusive_amount: money(taxable),
    rounding_amount: money(payable - total),
    payable_amount: money(payable),
  };
  inv.tax_breakdown = [{ taxable_amount: money(taxable), tax_amount: money(vat), category: S }];
  inv.total_amount = money(total);
  inv.vat_amount = money(vat);

  switch (spec.kind) {
    case "valid":
      break;
    case "mismatch": {
      // Like the canonical totals-mismatch sample: a rounded payable amount that does not follow
      // from the parts. The validator reports ibr-co-16 with the computed value as the suggestion.
      const off = pick(r, [5000, 12500, 30000, 75000]);
      inv.totals.payable_amount = money(payable - off);
      break;
    }
    case "bad_trn": {
      // A truncated TRN, as in the canonical bad-TRN sample, or a letter in the middle.
      inv.seller_trn = r() < 0.5 ? client.trn.slice(0, 14) : `${client.trn.slice(0, 7)}X${client.trn.slice(8)}`;
      break;
    }
    case "missing": {
      const mode = fnv1a(spec.invoiceNumber) % 3;
      inv.buyer.name = "";
      if (mode !== 1) delete inv.buyer.postal_address;
      if (mode === 0) delete inv.issue_date;
      break;
    }
  }
  return inv;
}

/** Builds the whole batch for a firm; the product theme follows the client's position. */
export function buildBatch(sample: Json, clients: SeedClient[], plan: InvoiceSpec[]): { spec: InvoiceSpec; payload: Json }[] {
  return plan.map((spec) => ({ spec, payload: buildInvoice(sample, spec, clients.indexOf(spec.client)) }));
}

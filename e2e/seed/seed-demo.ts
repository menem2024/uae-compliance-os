/**
 * Demo-data seeder. Run it through scripts/seed-demo.sh (see the "Seed demo data" section of README.md).
 *
 * It fills a demo firm with synthetic UAE data through the real architecture only: it signs in through
 * the app's normal Zitadel login in a headless browser, then calls the web app's own BFF routes
 * (/api/...), which forward to api-go with the session's access token. Invoices go through
 * POST /api/invoices (api-go -> NATS -> validator-rs), and the human steps use the same routes the UI
 * uses: corrections, approve, export. Nothing writes to the database directly.
 *
 * Idempotent: clients are matched by TRN and invoices by invoice number before anything is created, and
 * each human step runs only when the invoice is in the state that step needs. Nothing is ever deleted.
 */
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { chromium } from "@playwright/test";
import type { APIRequestContext, Browser, BrowserContext, Page } from "@playwright/test";
import { FIRM_A_CLIENTS, FIRM_B_CLIENTS } from "./data.ts";
import type { SeedClient } from "./data.ts";
import { buildBatch, planFirmA, planFirmB } from "./generate.ts";
import type { InvoiceSpec } from "./generate.ts";

type Json = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any

const env = (k: string, d?: string): string | undefined => (process.env[k] !== undefined && process.env[k] !== "" ? process.env[k] : d);

const BASE_URL = (env("BASE_URL", "http://localhost:3000") as string).replace(/\/+$/, "");
const LOCALE = env("SEED_LOCALE", "en") as string;
const EMAIL_A = env("SEED_EMAIL", "a@firm-a.test") as string;
const EMAIL_B = env("SEED_EMAIL_B", "b@firm-b.test") as string;
// The password comes from the environment only. The default is the documented dev-fixture password of
// the local stack; the public demo uses its own, which is passed in (never printed, never committed).
const PASSWORD_A = (env("SEED_PASSWORD") ?? env("SEED_USER_PASSWORD") ?? "Password1!") as string;
const PASSWORD_B = (env("SEED_PASSWORD_B") ?? PASSWORD_A) as string;
const SEED_FIRM_B = env("SEED_FIRM_B", "1") !== "0";
const SET_BRAND = env("SEED_BRAND", "1") !== "0";
const STEP_TIMEOUT = 60_000;

const args = process.argv.slice(2);
const dumpDir = args.includes("--dump") ? args[args.indexOf("--dump") + 1] : undefined;
const dryRun = args.includes("--dry-run") || dumpDir !== undefined;

const log = (m: string) => console.log(m);
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// ---- sign-in ----------------------------------------------------------------------------------

/**
 * Signs in through the app's own login (landing page -> Zitadel hosted login -> back to the app) and returns
 * the browser context that holds the session cookie. This mirrors e2e/lib/auth.ts but follows BASE_URL, so it
 * also works behind a public hostname.
 */
async function signIn(browser: Browser, email: string, password: string): Promise<BrowserContext> {
  // A cold Zitadel or a tunnel can fail the first page load once; one fresh attempt is enough in practice.
  for (let attempt = 1; ; attempt++) {
    try {
      return await signInOnce(browser, email, password);
    } catch (e) {
      if (attempt >= 3) throw e;
      log(`  sign-in attempt ${attempt} failed (${e instanceof Error ? e.message.split("\n")[0] : e}), retrying`);
    }
  }
}

async function signInOnce(browser: Browser, email: string, password: string): Promise<BrowserContext> {
  const context = await browser.newContext({ baseURL: BASE_URL, ignoreHTTPSErrors: false });
  const page = await context.newPage();
  const appHost = new URL(BASE_URL).host;
  await page.goto(`/${LOCALE}`, { timeout: STEP_TIMEOUT });
  await page.getByTestId("sign-in").click();
  await page.waitForURL((u) => u.host !== appHost, { timeout: STEP_TIMEOUT });

  await fillFirst(page, ['input[name="loginName"]', 'input[name="username"]', "#loginName", 'input[type="email"]', 'input[type="text"]'], email, "login name");
  await clickFirst(page, 'button[type="submit"], button:has-text("Next"), button:has-text("Continue")');
  await fillFirst(page, ['input[name="password"]', 'input[type="password"]'], password, "password");
  await clickFirst(page, 'button[type="submit"], button:has-text("Next"), button:has-text("Continue"), button:has-text("Log in")');
  await page.waitForURL((u) => u.host === appHost, { timeout: STEP_TIMEOUT });
  await page.close();
  return context;
}

async function fillFirst(page: Page, selectors: string[], value: string, what: string): Promise<void> {
  for (const sel of selectors) {
    const loc = page.locator(sel).first();
    try {
      await loc.waitFor({ state: "visible", timeout: 8_000 });
      await loc.fill(value);
      return;
    } catch {
      /* try the next selector */
    }
  }
  throw new Error(`sign-in: no ${what} field found on the Zitadel page (${page.url()}); the hosted login UI may have changed`);
}

async function clickFirst(page: Page, selector: string): Promise<void> {
  const btn = page.locator(selector).first();
  await btn.waitFor({ state: "visible", timeout: 15_000 });
  await btn.click();
}

// ---- API (through the web app's BFF) -----------------------------------------------------------

class ApiError extends Error {
  status: number;
  body: Json | null;
  constructor(method: string, path: string, status: number, body: Json | null) {
    super(`${method} ${path} -> ${status} ${body ? JSON.stringify(body) : ""}`);
    this.status = status;
    this.body = body;
  }
}

class Api {
  request: APIRequestContext;
  constructor(request: APIRequestContext) {
    this.request = request;
  }

  async call(method: "GET" | "POST" | "PATCH", path: string, body?: unknown, okStatuses: number[] = []): Promise<Json> {
    for (let attempt = 0; ; attempt++) {
      const res = await this.request.fetch(`${BASE_URL}${path}`, {
        method,
        headers: { "content-type": "application/json" },
        data: body === undefined ? undefined : JSON.stringify(body),
        timeout: STEP_TIMEOUT,
        maxRedirects: 0,
      });
      const status = res.status();
      const text = await res.text();
      let json: Json | null = null;
      try {
        json = text ? JSON.parse(text) : null;
      } catch {
        json = null;
      }
      if ((status === 429 || status === 502 || status === 503) && attempt < 8) {
        const wait = Math.min(Number(res.headers()["retry-after"] ?? 0) * 1000 || 5_000 * (attempt + 1), 65_000);
        log(`  ${method} ${path} -> ${status}, retrying in ${Math.round(wait / 1000)}s`);
        await sleep(wait);
        continue;
      }
      if (status >= 200 && status < 300) return json ?? {};
      if (okStatuses.includes(status)) return { __status: status, ...(json ?? {}) };
      if (status === 307 || status === 302 || status === 401) {
        throw new ApiError(method, path, status, { error: "not signed in (session missing or expired)" });
      }
      throw new ApiError(method, path, status, json);
    }
  }
  get = (p: string) => this.call("GET", p);
  post = (p: string, b?: unknown, ok?: number[]) => this.call("POST", p, b, ok);
  patch = (p: string, b?: unknown) => this.call("PATCH", p, b);

  async allPages(path: string): Promise<Json[]> {
    const out: Json[] = [];
    let cursor = "";
    do {
      const sep = path.includes("?") ? "&" : "?";
      const page = await this.get(`${path}${sep}limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`);
      out.push(...(page.items ?? []));
      cursor = page.next_cursor ?? "";
    } while (cursor);
    return out;
  }
}

// ---- seeding ----------------------------------------------------------------------------------

type Counters = Record<string, number>;
const bump = (c: Counters, k: string, n = 1) => (c[k] = (c[k] ?? 0) + n);

async function ensureBrand(api: Api, color: string, c: Counters): Promise<void> {
  if (!SET_BRAND) return;
  const firm = await api.get("/api/firm");
  if (firm.brand_color) return; // never overwrite a colour someone chose
  await api.patch("/api/firm", { brand_color: color });
  bump(c, "brand_color_set");
}

async function ensureClients(api: Api, clients: SeedClient[], c: Counters): Promise<void> {
  const existing = await api.allPages("/api/client-companies?status=all");
  const byTrn = new Map(existing.map((x) => [x.trn as string, x]));
  for (const cl of clients) {
    if (byTrn.has(cl.trn)) {
      bump(c, "clients_existing");
      continue;
    }
    const res = await api.post("/api/client-companies", { name: cl.name, name_ar: cl.nameAr, trn: cl.trn, emirate: cl.emirate }, [409]);
    if (res.__status === 409) bump(c, "clients_existing");
    else bump(c, "clients_created");
  }
}

type Row = { id: string; status: string; payload_version: number; invoice_number: string };

async function listRows(api: Api): Promise<Map<string, Row>> {
  const rows = await api.allPages("/api/invoices");
  return new Map(rows.map((r) => [r.invoice_number as string, r as Row]));
}

const SETTLED = new Set(["validated", "has_issues", "ready", "needs_review"]);

async function waitSettled(api: Api, ids: string[]): Promise<void> {
  // One list call per round (the list has its own, larger rate limit than the per-invoice read).
  const pending = new Set(ids);
  const deadline = Date.now() + 180_000;
  while (pending.size > 0) {
    for (const r of (await listRows(api)).values()) if (pending.has(r.id) && SETTLED.has(r.status)) pending.delete(r.id);
    if (pending.size === 0) break;
    if (Date.now() > deadline) throw new Error(`${pending.size} invoice(s) did not finish validation within 180s`);
    await sleep(1_500);
  }
}

async function auditActions(api: Api, id: string): Promise<string[]> {
  const a = await api.get(`/api/invoices/${id}/validation/audit`);
  return (a.items ?? []).map((i: Json) => i.action as string);
}

async function seedInvoices(api: Api, clients: SeedClient[], plan: InvoiceSpec[], sample: Json, c: Counters): Promise<void> {
  let rows = await listRows(api);
  const batch = buildBatch(sample, clients, plan);

  // 1. Create what is missing, then wait for the real validation to finish.
  const created: string[] = [];
  for (const { spec, payload } of batch) {
    if (rows.has(spec.invoiceNumber)) {
      bump(c, "invoices_existing");
      continue;
    }
    const res = await api.post("/api/invoices", payload);
    created.push(res.id as string);
    bump(c, "invoices_created");
  }
  if (created.length > 0) {
    log(`  validating ${created.length} new invoice(s) ...`);
    await waitSettled(api, created);
    rows = await listRows(api);
  }

  // 2. Human steps, each only when the invoice is in the state it needs.
  for (const { spec } of batch) {
    let row = rows.get(spec.invoiceNumber);
    if (!row) throw new Error(`invoice ${spec.invoiceNumber} is not in the list after creation`);

    if (spec.action === "correct" && row.status === "has_issues") {
      const detail = await api.get(`/api/invoices/${row.id}/validation`);
      const issue = (detail.latest_run?.issues ?? []).find((i: Json) => i.fixable && i.suggested_value && i.path);
      if (!issue) {
        log(`  ${spec.invoiceNumber}: no fixable issue to correct, left as is`);
      } else {
        const oldValue = String(valueAt(detail.payload, issue.path));
        await api.post(`/api/invoices/${row.id}/validation/corrections`, {
          payload_version: detail.payload_version,
          changes: [{ path: issue.path, old_value: oldValue, new_value: issue.suggested_value }],
          reason: "Applied the suggested value from the validation report (demo seed)",
        });
        bump(c, "corrected");
        row = { ...row, status: (await api.get(`/api/invoices/${row.id}`)).status };
        rows.set(spec.invoiceNumber, row);
      }
    }

    if ((spec.action === "approve" || spec.action === "export") && row.status === "validated") {
      const detail = await api.get(`/api/invoices/${row.id}/validation`);
      await api.post(`/api/invoices/${row.id}/validation/approve`, { payload_version: detail.payload_version });
      bump(c, "approved");
      row = { ...row, status: "ready" };
      rows.set(spec.invoiceNumber, row);
    }

    if (spec.action === "export" && row.status === "ready") {
      if (!(await auditActions(api, row.id)).includes("invoice.exported")) {
        await api.post("/api/exports", { invoice_id: row.id });
        bump(c, "exported");
      }
    }
  }
}

/** The value of a string field by api-go field path, as the correction's old_value guard needs it. */
function valueAt(payload: Json, path: string): string {
  let cur: unknown = payload;
  for (const part of path.split(".")) {
    const m = /^([a-z][a-z0-9_]*)(?:\[([0-9]+)\])?$/.exec(part);
    if (!m || typeof cur !== "object" || cur === null) return "";
    cur = (cur as Json)[m[1]];
    if (m[2] !== undefined) cur = Array.isArray(cur) ? cur[Number(m[2])] : undefined;
    if (cur === undefined || cur === null) return "";
  }
  return typeof cur === "string" || typeof cur === "number" || typeof cur === "boolean" ? String(cur) : "";
}

async function statusCounts(api: Api, plan: InvoiceSpec[]): Promise<Counters> {
  const rows = await listRows(api);
  const out: Counters = {};
  for (const p of plan) bump(out, rows.get(p.invoiceNumber)?.status ?? "missing");
  return out;
}

// ---- main -------------------------------------------------------------------------------------

async function main(): Promise<void> {
  const sample = JSON.parse(readFileSync(new URL("../../demo/samples/01-valid.json", import.meta.url), "utf8"));
  const planA = planFirmA(FIRM_A_CLIENTS);
  const planB = planFirmB(FIRM_B_CLIENTS);

  if (dryRun) {
    if (dumpDir) {
      mkdirSync(dumpDir, { recursive: true });
      for (const { spec, payload } of [...buildBatch(sample, FIRM_A_CLIENTS, planA), ...buildBatch(sample, FIRM_B_CLIENTS, planB)]) {
        writeFileSync(`${dumpDir}/${spec.kind}-${spec.invoiceNumber}.json`, JSON.stringify(payload, null, 2));
      }
    }
    log(`dry run: firm A ${FIRM_A_CLIENTS.length} clients, ${planA.length} invoices; firm B ${FIRM_B_CLIENTS.length} clients, ${planB.length} invoices`);
    for (const p of planA) log(`  ${p.invoiceNumber}  ${p.kind.padEnd(8)} ${p.action.padEnd(8)} ${p.issueDate}  ${p.client.name}`);
    return;
  }

  const started = Date.now();
  log(`seeding ${BASE_URL} as ${EMAIL_A}${SEED_FIRM_B ? ` and ${EMAIL_B}` : ""}`);
  const browser = await chromium.launch();
  try {
    const run = async (label: string, email: string, password: string, clients: SeedClient[], plan: InvoiceSpec[], color: string) => {
      log(`[${label}] signing in as ${email}`);
      const ctx = await signIn(browser, email, password);
      const api = new Api(ctx.request);
      const c: Counters = {};
      await ensureBrand(api, color, c);
      await ensureClients(api, clients, c);
      await seedInvoices(api, clients, plan, sample, c);
      log(`[${label}] done: ${JSON.stringify(c)}`);
      log(`[${label}] invoice statuses: ${JSON.stringify(await statusCounts(api, plan))}`);
      return { ctx, api };
    };

    const a = await run("firm A", EMAIL_A, PASSWORD_A, FIRM_A_CLIENTS, planA, "#0F766E");
    if (SEED_FIRM_B) {
      const b = await run("firm B", EMAIL_B, PASSWORD_B, FIRM_B_CLIENTS, planB, "#B45309");
      // Isolation: neither firm sees the other's invoices or clients, and a direct read of an id is a 404.
      const aRows = await listRows(a.api);
      const bRows = await listRows(b.api);
      const leak = [...bRows.keys()].filter((n) => aRows.has(n)).concat([...aRows.keys()].filter((n) => bRows.has(n)));
      const aOwn = aRows.get(planA[0].invoiceNumber);
      const cross = aOwn ? await b.api.call("GET", `/api/invoices/${aOwn.id}`, undefined, [403, 404]) : { __status: 0 };
      const bClients = new Set((await b.api.allPages("/api/client-companies?status=all")).map((x) => x.trn));
      const clientLeak = FIRM_A_CLIENTS.filter((x) => bClients.has(x.trn)).length;
      if (leak.length > 0 || clientLeak > 0 || ![403, 404].includes(cross.__status)) {
        throw new Error(`ISOLATION CHECK FAILED: shared invoices ${leak.length}, shared clients ${clientLeak}, cross read status ${cross.__status}`);
      }
      log(`isolation check: ok (firm B sees ${bRows.size} invoices, firm A ${aRows.size}; cross-firm read -> ${cross.__status})`);
      await b.ctx.close();
    }
    await a.ctx.close();
  } finally {
    await browser.close();
  }
  log(`finished in ${Math.round((Date.now() - started) / 1000)}s`);
}

main().catch((e) => {
  console.error(`seed failed: ${e instanceof Error ? e.message : String(e)}`);
  process.exit(1);
});

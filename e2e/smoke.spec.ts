import { expect, test, type Page } from "@playwright/test";
import { login } from "./lib/auth";
import { serviceNamesInTrace } from "./lib/tempo";

/**
 * Phase 0 walking-skeleton exit-criteria smoke (Story 8/10):
 *   AC1 — a logged-in FirmUser submits a demo invoice and the UI shows the ValidationRun result.
 *   AC2 — that one request is one connected trace across all 4 services in Grafana Tempo.
 *   AC3 — Firm B cannot read Firm A's invoice (RLS / tenancy isolation, exercised through the BFF).
 * Plus a carried concern from Story 7b (the mandate countdown clock check) and an RTL sanity
 * check. See plan.md Global Constraints for the fixed dev users/rules/service names.
 */

const FIRM_A_EMAIL = "a@firm-a.test";
const FIRM_B_EMAIL = "b@firm-b.test";
const INVALID_TRN = "123";
const VALID_TRN = "100000000000003";
const EXPECTED_SERVICES = ["web", "api-go", "ai-py", "validator-rs"] as const;

/** Fills the demo form's editable seller TRN and submits it; the other fields are fixed. */
async function submitDemoInvoice(page: Page, sellerTrn: string): Promise<{ traceId: string }> {
  await page.goto("/en/demo");
  await page.getByTestId("seller-trn").fill(sellerTrn);
  await page.getByTestId("submit").click();

  // Terminal status only: "has_issues" or "validated". The full pipeline (web -> api-go ->
  // NATS -> ai-py -> NATS -> api-go -> validator-rs -> back) can take a few seconds.
  await expect(page.getByTestId("status")).toHaveAttribute("data-status", /^(validated|has_issues)$/, {
    timeout: 60_000,
  });

  const traceId = (await page.getByTestId("trace-id").textContent())?.trim();
  if (!traceId) throw new Error('[smoke] the "trace-id" testid was empty after a successful submit');
  return { traceId };
}

/**
 * Reads the mandate countdown's "<N> days left" text from the dashboard. apps/web is out of
 * scope for this story (no new data-testid may be added there), so this selects by the
 * card's heading role/text instead, per the story brief.
 */
async function readCountdownDays(page: Page): Promise<number> {
  const heading = page.getByRole("heading", { name: "Countdown to the mandate" });
  await expect(heading).toBeVisible({ timeout: 20_000 });
  const card = page.locator("section").filter({ has: heading });
  const text = await card.innerText();
  const match = text.match(/([\d,]+)\s*days left/i);
  if (!match) {
    throw new Error(
      `[smoke] could not find "<N> days left" inside the countdown card. Card text was: ${JSON.stringify(text)}`,
    );
  }
  return Number(match[1].replace(/,/g, ""));
}

test("AC1+AC2: an invalid TRN flows through all 4 services and yields one connected trace", async ({ page }) => {
  test.setTimeout(180_000);

  await login(page, FIRM_A_EMAIL);
  const { traceId } = await submitDemoInvoice(page, INVALID_TRN);

  const status = page.getByTestId("status");
  await expect(status).toHaveAttribute("data-status", "has_issues");
  await expect(status).toContainText("has_issues");

  await expect(page.getByTestId("issue").first()).toContainText("AE-TRN-001", { timeout: 20_000 });

  expect(traceId, `trace-id must be a 32-char lowercase hex W3C trace id, got "${traceId}"`).toMatch(
    /^[0-9a-f]{32}$/,
  );

  const names = [...(await serviceNamesInTrace(traceId))].sort();
  for (const service of EXPECTED_SERVICES) {
    expect(names, `expected service "${service}" in Tempo trace ${traceId}; saw [${names.join(", ")}]`).toContain(
      service,
    );
  }
});

test("AC1: a fully valid TRN reaches status=validated", async ({ page }) => {
  test.setTimeout(120_000);

  await login(page, FIRM_A_EMAIL);
  await submitDemoInvoice(page, VALID_TRN);

  await expect(page.getByTestId("status")).toHaveAttribute("data-status", "validated");
});

test("AC3: firm B cannot read firm A's invoice via the BFF (404, not 200/403)", async ({ browser }) => {
  test.setTimeout(180_000);

  const contextA = await browser.newContext();
  const pageA = await contextA.newPage();
  await login(pageA, FIRM_A_EMAIL);

  const invoiceId = await pageA.evaluate(async () => {
    const res = await fetch("/api/invoices", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        invoice_number: `E2E-TENANCY-${Date.now()}`,
        issue_date: new Date().toISOString().slice(0, 10),
        seller_trn: "123",
        buyer_trn: "100000000000003",
        currency: "AED",
        total_amount: "1050.00",
        vat_amount: "50.00",
      }),
    });
    if (!res.ok) throw new Error(`POST /api/invoices as firm A failed: HTTP ${res.status}`);
    const body = (await res.json()) as { id: string };
    return body.id;
  });
  expect(invoiceId, "invoice id returned from firm A's submission").toBeTruthy();
  await contextA.close();

  const contextB = await browser.newContext();
  const pageB = await contextB.newPage();
  await login(pageB, FIRM_B_EMAIL);

  const statusCode = await pageB.evaluate(async (id: string) => {
    const res = await fetch(`/api/invoices/${encodeURIComponent(id)}`);
    return res.status;
  }, invoiceId);
  await contextB.close();

  expect(statusCode, "GET of firm A's invoice id, authenticated as firm B, must 404 (not leak via 403)").toBe(404);
});

test.describe("mandate countdown (carried concern from Story 7b)", () => {
  // Only this test needs a fixed viewer time zone; the rest of the suite runs in the default.
  test.use({ timezoneId: "Asia/Dubai" });

  test("shows 0 days at go-live and 277 days on 2026-09-27, both read in Asia/Dubai time", async ({ page }) => {
    test.setTimeout(150_000);

    await login(page, FIRM_A_EMAIL);

    // 2027-06-30T21:00:00Z = 2027-07-01 01:00 in Asia/Dubai (UTC+4): already past midnight on
    // the mandate's go-live calendar date, so the countdown must read 0.
    await page.clock.setFixedTime(new Date("2027-06-30T21:00:00.000Z"));
    await page.goto("/en/dashboard");
    // Poll: the SSR text shows the server date until hydration recomputes from the fixed clock.
    await expect.poll(() => readCountdownDays(page), { timeout: 20_000 }).toBe(0);

    // 2026-09-27T12:00:00Z = 2026-09-27 16:00 in Asia/Dubai: 277 whole calendar days before
    // the 2027-07-01 go-live date.
    await page.clock.setFixedTime(new Date("2026-09-27T12:00:00.000Z"));
    await page.goto("/en/dashboard");
    // Poll: the SSR text shows the server date until hydration recomputes from the fixed clock.
    await expect.poll(() => readCountdownDays(page), { timeout: 20_000 }).toBe(277);
  });
});

test("Arabic RTL sanity: /ar is dir=rtl, /en is dir=ltr", async ({ page }) => {
  await page.goto("/ar");
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");

  await page.goto("/en");
  await expect(page.locator("html")).toHaveAttribute("dir", "ltr");
});

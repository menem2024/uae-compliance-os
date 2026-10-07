import { expect, test, type Page } from "@playwright/test";
import { login } from "./lib/auth";

/**
 * Invoice review demo flow against the real stack (`make up` / scripts/demo.sh), never in CI:
 *   - sample 01 (the official PINT-AE example) validates, is approved by a human and exported as XML;
 *   - sample 02 (totals mismatch) raises `ibr-co-16`, the suggestion is applied and the invoice validates.
 * Selectors are the data-testid attributes of apps/web/src/components/invoices/*.
 */

const FIRM_A_EMAIL = "a@firm-a.test";
/** The whole pipeline (api-go -> NATS -> ai-py -> NATS -> api-go -> validator-rs) takes a few seconds. */
const PIPELINE = { timeout: 60_000 };

/** Opens /en/invoices, creates a demo invoice from a sample and waits for its review page. */
async function submitSample(page: Page, sample: "valid" | "totalsMismatch"): Promise<void> {
  await page.goto("/en/invoices");
  await expect(page.getByTestId("sample-picker")).toBeVisible();
  await page.getByTestId(`sample-submit-${sample}`).click();
  await page.waitForURL(/\/en\/invoices\/[0-9a-f-]{36}$/, PIPELINE);
}

const status = (page: Page) => page.getByTestId("review-status");

test.describe("invoice review", () => {
  test.beforeEach(async ({ page }) => {
    await login(page, FIRM_A_EMAIL);
  });

  test("a valid invoice is validated, approved and exported as PINT-AE XML", async ({ page }) => {
    await submitSample(page, "valid");
    await expect(status(page)).toHaveAttribute("data-status", "validated", PIPELINE);
    await expect(page.getByTestId("issue")).toHaveCount(0);
    await expect(page.getByTestId("trace-id")).not.toHaveText("—");

    await page.getByTestId("approve").click();
    await expect(status(page)).toHaveAttribute("data-status", "ready", PIPELINE);
    await expect(page.getByTestId("audit-event").filter({ hasText: "Approved" })).toHaveCount(1, PIPELINE);

    const [download] = await Promise.all([page.waitForEvent("download"), page.getByTestId("export").click()]);
    expect(download.suggestedFilename()).toMatch(/\.xml$/i);
    await expect(page.getByTestId("export-done")).toBeVisible();
    await expect(page.getByTestId("audit-event").filter({ hasText: "Exported" })).toHaveCount(1, PIPELINE);
  });

  test("a totals mismatch is fixed with the validator's suggestion", async ({ page }) => {
    await submitSample(page, "totalsMismatch");
    await expect(status(page)).toHaveAttribute("data-status", "has_issues", PIPELINE);

    const issue = page.locator('[data-testid="issue"][data-rule-id="ibr-co-16"]');
    await expect(issue).toBeVisible();
    await expect(page.getByTestId("approve")).toHaveCount(0);

    await issue.getByTestId("apply-suggestion").click();
    // The correction re-validates by itself; the status settles back from "fixed".
    await expect(status(page)).toHaveAttribute("data-status", "validated", PIPELINE);
    await expect(page.locator('[data-testid="issue"][data-rule-id="ibr-co-16"]')).toHaveCount(0);

    await page.getByTestId("revalidate").click();
    await expect(page.getByTestId("audit-event").filter({ hasText: "Validation requested" })).toHaveCount(1, PIPELINE);
    await expect(status(page)).toHaveAttribute("data-status", "validated");
    await expect(page.getByTestId("approve")).toBeEnabled();
  });
});

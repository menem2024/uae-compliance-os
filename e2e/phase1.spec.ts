import { expect, test } from "@playwright/test";
import { login } from "./lib/auth";

/**
 * Phase 1 (Track B) live-stack smoke: a Firm user creates a ClientCompany, changes the Firm's
 * accent colour, uploads a document through a presigned PUT straight to MinIO, and watches the
 * agent run that processed it on /agents (feed line, run row, run drawer with its DAG).
 *
 * Like smoke.spec.ts this runs only against the host-arbitrated compose stack (`make up`),
 * never in CI. It relies on the stack's defaults: ai-py runs the fake gateway (AI_GATEWAY
 * unset), so no real model is called. Every selector is one of the stable test ids from the
 * plan's Global Constraints, except the client dialog's own form controls (a dialog with
 * no test ids by design), which are reached by role inside the dialog.
 */

const FIRM_A_EMAIL = "a@firm-a.test";
/** Terminal upload states that mean the pipeline ran to the end (not failed, rejected or a duplicate). */
const PROCESSED = /^(extracted|needs_review|not_invoice)$/;
const BRAND_COLORS = ["#C8A45D", "#2F6FED"] as const;

/**
 * A tiny but valid one-page PDF (correct xref offsets). The text carries a per-run nonce so the
 * bytes, and therefore the sha256 the upload deduplicates on, are different on every run.
 */
function syntheticPdf(nonce: string): Buffer {
  const text = `Synthetic invoice fixture ${nonce}`;
  const stream = `BT /F1 14 Tf 72 720 Td (${text}) Tj ET`;
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
    `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
  ];
  let body = "%PDF-1.4\n";
  const offsets: number[] = [];
  objects.forEach((obj, i) => {
    offsets.push(body.length);
    body += `${i + 1} 0 obj\n${obj}\nendobj\n`;
  });
  const xrefAt = body.length;
  body += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  for (const off of offsets) body += `${String(off).padStart(10, "0")} 00000 n \n`;
  body += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xrefAt}\n%%EOF\n`;
  return Buffer.from(body, "latin1");
}

test("signed out, /en renders the landing with a sign-in button and not the dashboard", async ({ page }) => {
  await page.goto("/en");
  await expect(page.getByTestId("sign-in").first()).toBeVisible();
  await expect(page.getByTestId("open-workspace")).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "Countdown to the mandate" })).toHaveCount(0);
  expect(new URL(page.url()).pathname).not.toContain("/dashboard");
});

test("a Firm user creates a client, sets the brand colour, uploads a document and sees the agent run", async ({
  page,
}) => {
  test.setTimeout(240_000);
  const nonce = `${Date.now()}`;
  const clientName = `E2E Client ${nonce}`;

  await login(page, FIRM_A_EMAIL);

  // ClientCompany.
  await page.goto("/en/clients");
  await page.getByTestId("client-create").click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox").first().fill(clientName); // the English name field comes first
  await dialog.locator('button[type="submit"]').click();
  await expect(page.getByTestId("client-row").filter({ hasText: clientName })).toBeVisible();

  // Brand colour: pick whichever of two colours is not the current one, so a re-run still has a change to save.
  await page.goto("/en/settings");
  const brand = page.getByTestId("brand-color");
  await expect(brand).toBeVisible();
  const current = (await brand.inputValue()).trim().toUpperCase();
  await brand.fill(current === BRAND_COLORS[0] ? BRAND_COLORS[1] : BRAND_COLORS[0]);
  await page.getByTestId("settings-save").click();
  await expect(page.getByTestId("settings-save")).toBeDisabled(); // saved: nothing left to save

  // Upload: presigned PUT from the browser to MinIO's published S3 port, then the agents run.
  await page.goto("/en/documents");
  await page.getByTestId("client-picker").selectOption({ label: clientName });
  await page
    .getByTestId("dropzone")
    .locator('input[type="file"]')
    .setInputFiles({ name: `invoice-${nonce}.pdf`, mimeType: "application/pdf", buffer: syntheticPdf(nonce) });

  const uploadRow = page.getByTestId("upload-row").first();
  await expect(uploadRow).toBeVisible();
  await expect(uploadRow).toHaveAttribute("data-state", PROCESSED, { timeout: 120_000 });
  await expect(page.getByTestId("doc-row").filter({ hasText: `invoice-${nonce}.pdf` })).toBeVisible({
    timeout: 30_000,
  });

  // The agents page: the run, its drawer with at least one DAG node, and the live feed.
  await page.goto("/en/agents");
  const runRow = page.getByTestId("run-row").first();
  await expect(runRow).toBeVisible({ timeout: 60_000 });
  await expect(page.getByTestId("agents-feed")).toBeVisible();
  await expect(page.getByTestId("feed-line").first()).toBeVisible({ timeout: 60_000 });

  await runRow.click();
  await expect(page.getByTestId("run-drawer")).toBeVisible();
  await expect(page.getByTestId("dag-node").first()).toBeVisible({ timeout: 30_000 });
});

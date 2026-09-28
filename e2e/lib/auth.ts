import type { Locator, Page } from "@playwright/test";

/** Dev-only fixture password, shared by both seeded Zitadel users. Allowlisted in .gitleaks.toml. */
export const DEV_PASSWORD = "Password1!";

const ZITADEL_HOSTNAME = "zitadel.localhost";
/** Zitadel's hosted login is slow on a cold instance; each step gets its own generous budget. */
const STEP_TIMEOUT = 45_000;

/**
 * Tries each locator in order and fills the first one that becomes visible. Zitadel's hosted
 * login (pinned to v4.3.0, see deploy/compose/compose.yaml) may render its login-name/password
 * fields with slightly different labels or attributes across patch releases, so this tolerates
 * a few known variants instead of hard-coding exactly one selector.
 */
async function fillFirstMatch(candidates: Locator[], value: string, what: string): Promise<void> {
  for (const locator of candidates) {
    try {
      await locator.waitFor({ state: "visible", timeout: 5_000 });
      await locator.fill(value);
      return;
    } catch {
      // Try the next strategy.
    }
  }
  throw new Error(
    `[e2e/lib/auth] Could not find the Zitadel "${what}" field with any known selector. ` +
      `The pinned hosted-login UI may not match what this helper expects — inspect it with ` +
      `"npx playwright codegen http://zitadel.localhost:8085" and update e2e/lib/auth.ts.`,
  );
}

async function clickFirstMatch(candidates: Locator[], what: string): Promise<void> {
  for (const locator of candidates) {
    try {
      await locator.waitFor({ state: "visible", timeout: 5_000 });
      await locator.click();
      return;
    } catch {
      // Try the next strategy.
    }
  }
  throw new Error(
    `[e2e/lib/auth] Could not find the Zitadel "${what}" button with any known selector. ` +
      `Inspect the pinned hosted-login UI with "npx playwright codegen http://zitadel.localhost:8085" ` +
      `and update e2e/lib/auth.ts.`,
  );
}

/**
 * Logs in through Zitadel's hosted login starting from the signed-out landing at `/<locale>`,
 * and waits for the OIDC callback to land back on the web app. Flow: click the app's
 * `sign-in` button -> Zitadel login name -> Next -> password -> Next -> redirected back.
 *
 * Dev users (see plan.md Global Constraints): a@firm-a.test / b@firm-b.test, both Password1!.
 */
export async function login(page: Page, email: string, password: string = DEV_PASSWORD, locale = "en"): Promise<void> {
  await page.goto(`/${locale}`);
  await page.getByTestId("sign-in").click();

  await page.waitForURL((url) => url.hostname === ZITADEL_HOSTNAME, { timeout: STEP_TIMEOUT });

  await fillFirstMatch(
    [
      page.getByLabel(/login\s*name|username|e-?mail/i),
      page.locator('input[name="loginName"]'),
      page.locator('input[name="username"]'),
      page.locator("#loginName"),
      page.locator('input[type="email"]').first(),
      page.locator('input[type="text"]').first(),
    ],
    email,
    "login name",
  );
  await clickFirstMatch(
    [
      page.getByRole("button", { name: /^next$/i }),
      page.getByRole("button", { name: /next|weiter|continue|anmelden/i }),
      page.locator('button[type="submit"]'),
    ],
    "next (after login name)",
  );

  await fillFirstMatch(
    [
      page.getByLabel(/password/i),
      page.locator('input[name="password"]'),
      page.locator('input[type="password"]').first(),
    ],
    password,
    "password",
  );
  await clickFirstMatch(
    [
      page.getByRole("button", { name: /^next$/i }),
      page.getByRole("button", { name: /next|log ?in|weiter|continue|anmelden/i }),
      page.locator('button[type="submit"]'),
    ],
    "next (after password)",
  );

  await page.waitForURL((url) => url.hostname === "localhost" && url.port === "3000", { timeout: STEP_TIMEOUT });
}

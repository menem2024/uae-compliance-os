import { defineConfig, devices } from "@playwright/test";

/**
 * Phase 0 walking-skeleton smoke suite (Story 8/10). Exercises the compose stack (Tasks 6/7):
 * the web app is exposed on http://localhost:3000, Zitadel's hosted login on
 * http://zitadel.localhost:8085, and Tempo's HTTP API on http://localhost:3200.
 *
 * Zitadel's hosted login is slow to answer cold (first request per instance warms up several
 * internal services), so navigation/action timeouts here are generous on purpose. The suite
 * runs single-worker with no retries so a failure is deterministic and its trace/video/screenshot
 * (captured on first retry) point at one specific run.
 */
export default defineConfig({
  testDir: ".",
  timeout: 120_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: "http://localhost:3000",
    trace: "on-first-retry",
    screenshot: "only-on-failure",
    actionTimeout: 20_000,
    navigationTimeout: 45_000,
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});

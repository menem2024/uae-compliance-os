import { describe, expect, it } from "vitest";
import { dashboardPath, signInPath } from "./routes";

describe("routes", () => {
  it("builds the dashboard and sign-in paths", () => {
    expect(dashboardPath("ar")).toBe("/ar/dashboard");
    expect(signInPath("en", "/en/documents?x=1")).toBe("/en/sign-in?callbackUrl=%2Fen%2Fdocuments%3Fx%3D1");
  });
});

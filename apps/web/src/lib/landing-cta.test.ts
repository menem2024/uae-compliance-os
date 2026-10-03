import { describe, expect, it } from "vitest";
import { landingCta } from "./landing-cta";

describe("landingCta", () => {
  it("signed out: sign-in form target with the dashboard as the post-sign-in callback", () => {
    expect(landingCta(false, "en")).toEqual({
      testId: "sign-in",
      href: null,
      callbackUrl: "/en/dashboard",
    });
  });

  it("signed in: a direct link back into the workspace, no callback needed", () => {
    expect(landingCta(true, "ar")).toEqual({
      testId: "open-workspace",
      href: "/ar/dashboard",
    });
  });
});

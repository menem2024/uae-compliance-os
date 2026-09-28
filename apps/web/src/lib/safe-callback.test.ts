import { describe, expect, it } from "vitest";
import { safeCallbackPath } from "./safe-callback";

describe("safeCallbackPath", () => {
  it("keeps same-origin absolute paths", () => {
    expect(safeCallbackPath("/ar/demo", "ar")).toBe("/ar/demo");
    expect(safeCallbackPath("/en/demo?x=1", "ar")).toBe("/en/demo?x=1");
  });
  it("falls back to the locale home for anything that could leave the origin", () => {
    for (const bad of ["https://evil.test", "//evil.test", "/\\evil.test", "demo", "", undefined, null, "/%2F%2Fevil"]) {
      expect(safeCallbackPath(bad, "en")).toBe("/en");
    }
  });
  it("takes the first value when given an array (repeated query param)", () => {
    expect(safeCallbackPath(["/ar/demo", "//evil"], "ar")).toBe("/ar/demo");
  });
});

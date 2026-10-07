import { describe, expect, it } from "vitest";
import { MAX_POLLS } from "../demo";
import { detailRefetchInterval } from "./polling";

const idle = { dataUpdateCount: 1, errorUpdateCount: 0, errorStatus: null };

describe("detailRefetchInterval", () => {
  it("polls every second while the invoice is moving and stops once it settles", () => {
    for (const status of ["uploaded", "extracted", "fixed", undefined]) {
      expect(detailRefetchInterval({ ...idle, status })).toBe(1000);
    }
    for (const status of ["validated", "has_issues", "ready", "needs_review"]) {
      expect(detailRefetchInterval({ ...idle, status })).toBe(false);
    }
  });
  it("keeps polling through a 404 right after creation but stops on other errors", () => {
    expect(detailRefetchInterval({ ...idle, status: undefined, errorStatus: 404 })).toBe(1000);
    expect(detailRefetchInterval({ ...idle, status: undefined, errorStatus: 500 })).toBe(false);
  });
  it("stops at the shared poll cap", () => {
    expect(detailRefetchInterval({ dataUpdateCount: MAX_POLLS, errorUpdateCount: 0, status: "fixed" })).toBe(false);
  });
});

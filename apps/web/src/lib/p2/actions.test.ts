import { describe, expect, it } from "vitest";
import { availableActions, isSettledStatus } from "./actions";

describe("isSettledStatus", () => {
  it("is true only when the pipeline has stopped moving the invoice", () => {
    for (const s of ["validated", "has_issues", "ready", "needs_review"]) expect(isSettledStatus(s)).toBe(true);
    for (const s of ["uploaded", "classified", "extracted", "fixed", undefined]) expect(isSettledStatus(s)).toBe(false);
  });
});

describe("availableActions", () => {
  it("offers re-validate and corrections on an invoice with issues, but not approve", () => {
    expect(availableActions("has_issues", 2)).toEqual({ correct: true, revalidate: true, approve: false, export: false });
  });
  it("offers approve on a validated invoice without errors", () => {
    expect(availableActions("validated", 0)).toEqual({ correct: true, revalidate: true, approve: true, export: false });
  });
  it("never approves while errors remain", () => {
    expect(availableActions("validated", 1).approve).toBe(false);
  });
  it("offers export only once ready", () => {
    expect(availableActions("ready", 0)).toEqual({ correct: false, revalidate: true, approve: false, export: true });
  });
  it("offers nothing while the pipeline is still working", () => {
    for (const s of ["uploaded", "classified", "extracted", "fixed"]) {
      expect(availableActions(s, 0)).toEqual({ correct: false, revalidate: false, approve: false, export: false });
    }
  });
});

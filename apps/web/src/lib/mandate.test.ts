import { describe, expect, it } from "vitest";
import { daysToMandate } from "./mandate";

describe("daysToMandate", () => {
  it("counts calendar days to 2027-07-01", () => {
    expect(daysToMandate(new Date(2027, 5, 30, 23, 59))).toBe(1);
    expect(daysToMandate(new Date(2026, 6, 1, 8, 0))).toBe(365);
  });
  it("is zero on and after the mandate date", () => {
    expect(daysToMandate(new Date(2027, 6, 1))).toBe(0);
    expect(daysToMandate(new Date(2028, 0, 1))).toBe(0);
  });
});

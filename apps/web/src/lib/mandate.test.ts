import { afterEach, describe, expect, it, vi } from "vitest";
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

describe("daysToMandate in the viewer's time zone", () => {
  // 2027-06-30T21:30Z is 30 June in UTC but already 1 July 01:30 in Dubai (UTC+4).
  const lateUtc = new Date(Date.UTC(2027, 5, 30, 21, 30));

  it("counts from the Dubai calendar date after Dubai midnight", () => {
    expect(daysToMandate(lateUtc, "Asia/Dubai")).toBe(0);
  });
  it("counts from the UTC calendar date before UTC midnight", () => {
    expect(daysToMandate(lateUtc, "UTC")).toBe(1);
  });
  it("rolls over exactly at local midnight", () => {
    // 19:59:59Z = 23:59:59 in Dubai on 29 June; one second later it is 30 June there.
    expect(daysToMandate(new Date(Date.UTC(2027, 5, 29, 19, 59, 59)), "Asia/Dubai")).toBe(2);
    expect(daysToMandate(new Date(Date.UTC(2027, 5, 29, 20, 0, 0)), "Asia/Dubai")).toBe(1);
  });
  it("defaults to the runtime's local zone (the browser's, after hydration)", () => {
    expect(daysToMandate(lateUtc)).toBe(daysToMandate(lateUtc, Intl.DateTimeFormat().resolvedOptions().timeZone));
  });
});

describe("daysToMandate() with no argument (the client snapshot used after hydration)", () => {
  afterEach(() => vi.useRealTimers());

  it("reads the current clock on every call, never a cached build-time value", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2027, 5, 29, 12));
    expect(daysToMandate()).toBe(2);
    vi.setSystemTime(new Date(2027, 5, 30, 12));
    expect(daysToMandate()).toBe(1);
  });
});

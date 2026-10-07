import { describe, expect, it } from "vitest";
import { dateOnlyFormat, formatMoney, parseDateOnly } from "./format";

describe("parseDateOnly", () => {
  it("reads a plain date as the same UTC day", () => {
    expect(parseDateOnly("2026-01-12")?.toISOString()).toBe("2026-01-12T00:00:00.000Z");
    expect(parseDateOnly("2026-01-12T23:59:00Z")?.toISOString()).toBe("2026-01-12T00:00:00.000Z");
  });
  it("rejects empty and invalid input", () => {
    for (const s of ["", null, undefined, "x", "2026-13-01", "2026-02-31"]) expect(parseDateOnly(s)).toBeNull();
  });
  it("formats with Western digits in both locales and never shifts the day", () => {
    const d = parseDateOnly("2026-01-12") as Date;
    expect(dateOnlyFormat("en").format(d)).toBe("Jan 12, 2026");
    expect(dateOnlyFormat("ar").format(d)).toMatch(/12/);
    expect(dateOnlyFormat("ar").format(d)).toMatch(/2026/);
  });
});

describe("formatMoney", () => {
  it("groups thousands and pads to two decimals without rounding", () => {
    expect(formatMoney("268328.62")).toBe("268,328.62");
    expect(formatMoney("69648.6")).toBe("69,648.60");
    expect(formatMoney("232617")).toBe("232,617.00");
    expect(formatMoney("-1234.567")).toBe("-1,234.567");
    expect(formatMoney("12")).toBe("12.00");
  });
  it("leaves non-numbers alone", () => {
    expect(formatMoney("")).toBe("");
    expect(formatMoney("n/a")).toBe("n/a");
  });
});

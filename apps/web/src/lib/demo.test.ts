import { describe, expect, it } from "vitest";
import { demoInvoice, isTerminalStatus, localDateISO, pollInterval } from "./demo";

describe("localDateISO", () => {
  it("formats the viewer's local calendar date as YYYY-MM-DD with Western digits", () => {
    expect(localDateISO(new Date(2026, 8, 7, 23, 59))).toBe("2026-09-07");
  });
});

describe("demoInvoice", () => {
  it("builds the fixed demo invoice around the entered seller TRN", () => {
    expect(demoInvoice(" 123 ", new Date(2026, 8, 27, 10))).toEqual({
      invoice_number: "INV-DEMO-1",
      issue_date: "2026-09-27",
      seller_trn: "123",
      buyer_trn: "100000000000003",
      currency: "AED",
      total_amount: "1050.00",
      vat_amount: "50.00",
    });
  });
  it("keeps every field a string (money is never a float)", () => {
    for (const v of Object.values(demoInvoice("100000000000003", new Date()))) expect(typeof v).toBe("string");
  });
});

describe("isTerminalStatus / pollInterval", () => {
  it("stops polling once validation finished", () => {
    expect(isTerminalStatus("validated")).toBe(true);
    expect(isTerminalStatus("has_issues")).toBe(true);
    expect(pollInterval("validated")).toBe(false);
    expect(pollInterval("has_issues")).toBe(false);
  });
  it("polls every second while the invoice is in flight", () => {
    for (const s of ["uploaded", "extracted", undefined]) {
      expect(isTerminalStatus(s)).toBe(false);
      expect(pollInterval(s)).toBe(1000);
    }
  });
});

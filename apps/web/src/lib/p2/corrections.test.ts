import { describe, expect, it } from "vitest";
import { buildCorrection, valueAtPath } from "./corrections";

const payload = {
  invoice_number: "INV-1",
  seller: { name: "Al Waha", postal_address: { city: "Sharjah" } },
  lines: [{ id: "1", item: { name: "Pen" } }, { id: "2" }],
  is_charge: true,
  amount: 5,
};

describe("valueAtPath", () => {
  it("reads top-level, nested and indexed string fields", () => {
    expect(valueAtPath(payload, "invoice_number")).toBe("INV-1");
    expect(valueAtPath(payload, "seller.postal_address.city")).toBe("Sharjah");
    expect(valueAtPath(payload, "lines[0].item.name")).toBe("Pen");
    expect(valueAtPath(payload, "lines[1].id")).toBe("2");
  });
  it("is empty for an absent field, parent or index", () => {
    expect(valueAtPath(payload, "buyer.name")).toBe("");
    expect(valueAtPath(payload, "seller.trading_name")).toBe("");
    expect(valueAtPath(payload, "lines[5].id")).toBe("");
    expect(valueAtPath(payload, "lines[1].item.name")).toBe("");
  });
  it("renders booleans like the server and never invents a value for objects", () => {
    expect(valueAtPath(payload, "is_charge")).toBe("true");
    expect(valueAtPath(payload, "seller")).toBe("");
    expect(valueAtPath(payload, "amount")).toBe("5");
  });
  it("is empty for a path outside the grammar", () => {
    expect(valueAtPath(payload, "")).toBe("");
    expect(valueAtPath(payload, "seller..name")).toBe("");
    expect(valueAtPath(payload, "lines[x].id")).toBe("");
  });
});

describe("buildCorrection", () => {
  it("builds the request body with the current value as the concurrency guard", () => {
    expect(buildCorrection({ payload, payloadVersion: 3, path: "seller.name", value: "Al Waha LLC", reason: "r" })).toEqual({
      payload_version: 3,
      changes: [{ path: "seller.name", old_value: "Al Waha", new_value: "Al Waha LLC" }],
      reason: "r",
    });
  });
  it("uses an empty old value for an absent field and trims the path", () => {
    const b = buildCorrection({ payload, payloadVersion: 1, path: " buyer.name ", value: "Noor", reason: "" });
    expect(b?.changes).toEqual([{ path: "buyer.name", old_value: "", new_value: "Noor" }]);
  });
  it("is null for a blank path, a no-op or a bad payload version", () => {
    expect(buildCorrection({ payload, payloadVersion: 1, path: "  ", value: "x", reason: "" })).toBeNull();
    expect(buildCorrection({ payload, payloadVersion: 1, path: "invoice_number", value: "INV-1", reason: "" })).toBeNull();
    expect(buildCorrection({ payload, payloadVersion: 0, path: "invoice_number", value: "x", reason: "" })).toBeNull();
  });
});

import { describe, expect, it } from "vitest";
import { type FirmSettings, firmForm, normalizeHex, toFirmPatch } from "./firm-settings";

const firm: FirmSettings = { id: "f1", name: "Al Noor Accounting", brand_color: "#1F6FEB", updated_at: "x" };

describe("firm settings form", () => {
  it("normalises hex colours and rejects anything else", () => {
    expect(normalizeHex("c8a45d")).toBe("#C8A45D");
    expect(normalizeHex(" #1f6feb ")).toBe("#1F6FEB");
    expect(normalizeHex("#fff")).toBeNull();
    expect(normalizeHex("red;background:url(x)")).toBeNull();
  });

  it("builds a minimal patch", () => {
    expect(toFirmPatch(firm, firmForm(firm))).toEqual({});
    expect(toFirmPatch(firm, { name: "Al Noor", brandColor: "#1f6feb" })).toEqual({ name: "Al Noor" });
    expect(toFirmPatch(firm, { name: firm.name, brandColor: "" })).toEqual({ brand_color: null });
    expect(toFirmPatch({ ...firm, brand_color: null }, { name: firm.name, brandColor: "c8a45d" })).toEqual({
      brand_color: "#C8A45D",
    });
  });

  it("returns null for an invalid form", () => {
    expect(toFirmPatch(firm, { name: "  ", brandColor: "" })).toBeNull();
    expect(toFirmPatch(firm, { name: "X", brandColor: "#12" })).toBeNull();
  });
});

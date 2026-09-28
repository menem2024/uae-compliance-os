import { describe, expect, it } from "vitest";
import { parseMe, toShellFirm } from "./me";

describe("parseMe", () => {
  it("accepts the api-go /v1/me contract", () => {
    expect(parseMe({ firm_id: "f1", firm_name: "Demo Firm A", brand_color: "#0F6E6A" })).toEqual({
      firm_id: "f1",
      firm_name: "Demo Firm A",
      brand_color: "#0F6E6A",
    });
    expect(parseMe({ firm_id: "f1", firm_name: "A", brand_color: null })?.brand_color).toBeNull();
  });
  it("rejects malformed payloads", () => {
    expect(parseMe(null)).toBeNull();
    expect(parseMe({ firm_name: "A" })).toBeNull();
    expect(parseMe({ firm_id: "f", firm_name: 3, brand_color: null })).toBeNull();
  });
  it("drops a brand colour that is not #RRGGBB (CSS injection guard)", () => {
    expect(parseMe({ firm_id: "f", firm_name: "A", brand_color: "red;x:y" })?.brand_color).toBeNull();
  });
});

describe("toShellFirm", () => {
  it("maps /v1/me onto the shell Firm", () => {
    expect(toShellFirm({ firm_id: "f", firm_name: "Demo Firm A", brand_color: "#0F6E6A" }, "Your firm")).toEqual({
      name: "Demo Firm A",
      brandColor: "#0F6E6A",
    });
  });
  it("falls back to a neutral Firm when /v1/me is unavailable", () => {
    expect(toShellFirm(null, "Your firm")).toEqual({ name: "Your firm", brandColor: null });
  });
});

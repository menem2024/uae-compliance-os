import { describe, expect, it } from "vitest";
import { brandStyle, brandVars, contrastRatio } from "./brand";

describe("contrastRatio", () => {
  it("matches WCAG reference values", () => {
    expect(contrastRatio("#000000", "#FFFFFF")).toBeCloseTo(21, 0);
    expect(contrastRatio("#FFFFFF", "#FFFFFF")).toBeCloseTo(1, 5);
  });
});

describe("brandVars", () => {
  it("defaults to platform gold when the Firm has no brand colour", () => {
    expect(brandVars(null, "dark")["--brand"]).toBe("#C8A45D");
  });
  it("keeps a compliant Firm colour", () => {
    expect(brandVars("#0F6E6A", "light")["--brand"]).toBe("#0F6E6A");
  });
  it("darkens a too-light colour in light mode until brand ink on white reaches 4.5:1", () => {
    const v = brandVars("#F5E6A8", "light");
    expect(contrastRatio(v["--brand-ink"], "#FFFFFF")).toBeGreaterThanOrEqual(4.5);
  });
  it("lightens a too-dark colour in dark mode until brand ink on the dark panel reaches 4.5:1", () => {
    const v = brandVars("#1A1030", "dark");
    expect(contrastRatio(v["--brand-ink"], "#12151A")).toBeGreaterThanOrEqual(4.5);
  });
  it("picks a readable on-brand text colour", () => {
    const v = brandVars("#0F6E6A", "light");
    expect(contrastRatio(v["--brand-foreground"], "#0F6E6A")).toBeGreaterThanOrEqual(4.5);
  });
  it("rejects malformed input by falling back to the default", () => {
    expect(brandVars("red; background:url(x)", "dark")["--brand"]).toBe("#C8A45D");
  });
});

describe("brandStyle", () => {
  it("emits both modes' ink so CSS can switch theme without re-render", () => {
    const s = brandStyle("#1A1030");
    expect(s["--brand"]).toBe("#1A1030");
    expect(contrastRatio(s["--brand-ink-light"], "#FFFFFF")).toBeGreaterThanOrEqual(4.5);
    expect(contrastRatio(s["--brand-ink-dark"], "#12151A")).toBeGreaterThanOrEqual(4.5);
  });
  it("never lets malformed input through", () => {
    expect(Object.values(brandStyle("x;}body{display:none")).join()).not.toContain(";");
  });
});

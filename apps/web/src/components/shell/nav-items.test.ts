import { describe, expect, it } from "vitest";
import { NAV_ITEMS, activeNavItem } from "./nav-items";

describe("nav items", () => {
  it("have unique keys and full label paths", () => {
    expect(new Set(NAV_ITEMS.map((n) => n.key)).size).toBe(NAV_ITEMS.length);
    for (const n of NAV_ITEMS) expect(n.labelKey).toMatch(/^[A-Z][A-Za-z0-9]*\.[a-z.]+/);
  });
  it("resolve the active item by prefix and ignore unbuilt routes", () => {
    expect(activeNavItem("/dashboard")?.key).toBe("dashboard");
    expect(activeNavItem("/demo/x")?.key).toBe("demo");
    expect(activeNavItem("/")).toBeNull();
    expect(activeNavItem("/agents")).toBeNull(); // href null until Task 26
  });

  it("enable clients and settings with their page headings (Task 11)", () => {
    expect(activeNavItem("/clients")).toMatchObject({ key: "clients", titleKey: "P1Clients.title" });
    expect(activeNavItem("/settings")).toMatchObject({ key: "settings", titleKey: "P1Settings.title" });
  });

  it("enable documents with its page heading (Task 24)", () => {
    expect(activeNavItem("/documents")).toMatchObject({ key: "documents", titleKey: "P1Documents.title" });
  });
});

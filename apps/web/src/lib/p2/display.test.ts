import { describe, expect, it } from "vitest";
import { errorMessageKey, formatDurationUs, sortAudit } from "./display";
import type { AuditItem } from "./types";

describe("errorMessageKey", () => {
  it("maps the api-go codes the review flow can hit to their own message", () => {
    for (const c of ["stale_payload", "old_value_mismatch", "bad_path", "not_validated", "not_ready", "revalidation_required", "validator_unavailable", "exporter_unavailable", "not_found", "rate_limited"]) {
      expect(errorMessageKey(c)).toBe(c);
    }
  });
  it("maps the BFF and transport codes", () => {
    expect(errorMessageKey("unauthenticated")).toBe("unauthenticated");
    expect(errorMessageKey("upstream_unavailable")).toBe("upstream_unavailable");
  });
  it("falls back to a generic message for anything else", () => {
    expect(errorMessageKey("http_500")).toBe("generic");
    expect(errorMessageKey("")).toBe("generic");
    expect(errorMessageKey("toString")).toBe("generic");
  });
});

describe("formatDurationUs", () => {
  it("uses microseconds, milliseconds or seconds with Western digits", () => {
    expect(formatDurationUs(850)).toBe("850 µs");
    expect(formatDurationUs(1500)).toBe("1.5 ms");
    expect(formatDurationUs(42_000)).toBe("42 ms");
    expect(formatDurationUs(2_340_000)).toBe("2.34 s");
  });
  it("is a dash for a missing or invalid duration", () => {
    expect(formatDurationUs(Number.NaN)).toBe("—");
    expect(formatDurationUs(-1)).toBe("—");
  });
});

const ev = (id: string, at: string): AuditItem => ({
  id, occurred_at: at, actor_type: "user", actor_id: "u", agent: "", action: "a", entity_type: "invoice",
  changes: null, before: null, after: null, reason: "", trace_id: "",
});

describe("sortAudit", () => {
  it("orders oldest first, ties by id, without mutating the input", () => {
    const list = [ev("b", "2026-01-02T00:00:00Z"), ev("z", "2026-01-01T00:00:00Z"), ev("a", "2026-01-01T00:00:00Z")];
    expect(sortAudit(list).map((e) => e.id)).toEqual(["a", "z", "b"]);
    expect(list.map((e) => e.id)).toEqual(["b", "z", "a"]);
  });
});

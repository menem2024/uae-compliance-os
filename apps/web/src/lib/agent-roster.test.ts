import { describe, expect, it } from "vitest";
import { ROSTER, formatCostMicroUsd, formatDuration, rosterWithStats } from "./agent-roster";

describe("ROSTER", () => {
  it("has the four active Phase 1 agents and the three later ones", () => {
    expect(ROSTER.filter((r) => r.status === "active").map((r) => r.agent)).toEqual([
      "intake", "extraction", "verifier", "orchestrator",
    ]);
    expect(ROSTER.filter((r) => r.status !== "active").map((r) => r.agent)).toEqual(["fix", "legal", "regulation_watcher"]);
    expect(ROSTER.find((r) => r.agent === "verifier")?.model).toBe("Sonnet → Opus");
  });
});

describe("rosterWithStats", () => {
  it("joins the summary by agent, summing calls and weighting the average", () => {
    const out = rosterWithStats([
      { agent: "verifier", model: "sonnet", calls: 3, avg_ms: 100 },
      { agent: "verifier", model: "opus", calls: 1, avg_ms: 500 },
      { agent: "intake", model: "haiku", calls: 2, avg_ms: 40 },
    ]);
    expect(out.find((r) => r.agent === "verifier")).toMatchObject({ calls: 4, avg_ms: 200 });
    expect(out.find((r) => r.agent === "intake")).toMatchObject({ calls: 2, avg_ms: 40 });
    expect(out.find((r) => r.agent === "fix")).toMatchObject({ calls: 0, avg_ms: 0 });
  });
});

describe("formatters", () => {
  it("formats micro-USD with Western digits", () => {
    expect(formatCostMicroUsd(0)).toBe("$0.00");
    expect(formatCostMicroUsd(12_345)).toBe("$0.0123");
    expect(formatCostMicroUsd(2_500_000)).toBe("$2.50");
  });
  it("formats durations", () => {
    expect(formatDuration(0)).toBe("-");
    expect(formatDuration(850)).toBe("850 ms");
    expect(formatDuration(12_340)).toBe("12.3 s");
  });
});

/** The "orchestra": which agents exist, what they run on, and per-agent stats joined from /api/agents/summary. */

export type RosterStatus = "active" | "phase2" | "phase3";
export type RosterEntry = { agent: string; model: string; status: RosterStatus };

export const ROSTER: readonly RosterEntry[] = [
  { agent: "intake", model: "Haiku", status: "active" },
  { agent: "extraction", model: "Sonnet", status: "active" },
  { agent: "verifier", model: "Sonnet → Opus", status: "active" },
  { agent: "orchestrator", model: "rules", status: "active" },
  { agent: "fix", model: "Sonnet", status: "phase2" },
  { agent: "legal", model: "Opus", status: "phase2" },
  { agent: "regulation_watcher", model: "Sonnet", status: "phase3" },
];

export type AgentSummaryRow = { agent: string; model: string; calls: number; avg_ms: number };
export type RosterStats = RosterEntry & { calls: number; avg_ms: number };

/** One row per roster agent; a summary agent with several models is summed, its average weighted by calls. */
export function rosterWithStats(summary: readonly AgentSummaryRow[]): RosterStats[] {
  return ROSTER.map((r) => {
    const rows = summary.filter((s) => s.agent === r.agent);
    const calls = rows.reduce((sum, s) => sum + s.calls, 0);
    const weighted = rows.reduce((sum, s) => sum + s.calls * s.avg_ms, 0);
    return { ...r, calls, avg_ms: calls > 0 ? Math.round(weighted / calls) : 0 };
  });
}

/** Micro-USD as dollars, Western digits; amounts under a dollar keep four decimals so small spend stays visible. */
export function formatCostMicroUsd(micro: number): string {
  const usd = micro / 1_000_000;
  return usd > 0 && usd < 1 ? `$${usd.toFixed(4)}` : `$${usd.toFixed(2)}`;
}

export function formatDuration(ms: number): string {
  if (!(ms > 0)) return "-";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`;
  return `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`;
}

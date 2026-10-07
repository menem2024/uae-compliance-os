"use client";

import { useQuery } from "@tanstack/react-query";
import { requestJson } from "@/lib/api-client";
import type { AgentSummaryRow } from "@/lib/agent-roster";

export type AgentSummary = {
  runs: {
    runs: number; running: number; documents: number; llm_calls: number; response_cache_hits: number;
    cost_micro_usd: number;
  };
  agents: AgentSummaryRow[];
};

/** Today's totals for the header and the roster; the live feed does not carry aggregates, so poll. */
export function useAgentSummary() {
  return useQuery({
    queryKey: ["agents", "summary"],
    queryFn: ({ signal }) => requestJson<AgentSummary>("/api/agents/summary", { signal }),
    refetchInterval: 10_000,
  });
}

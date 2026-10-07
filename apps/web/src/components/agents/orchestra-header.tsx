"use client";

import { useTranslations } from "next-intl";
import { formatInt } from "@/lib/format";
import { formatCostMicroUsd } from "@/lib/agent-roster";
import { cn } from "@/lib/utils";
import type { Connection } from "./use-activity-stream";
import { useAgentSummary } from "./use-agent-summary";

const DOT: Record<Connection, string> = {
  live: "bg-ok animate-pulse",
  connecting: "bg-muted-foreground",
  offline: "bg-warn",
};

/** "The orchestra": today's totals plus the live connection state. */
export function OrchestraHeader({ connection, catchingUp }: { connection: Connection; catchingUp: boolean }) {
  const t = useTranslations("P1Agents");
  const summary = useAgentSummary();
  const r = summary.data?.runs;
  const tiles = [
    { key: "runs", value: r ? formatInt(r.runs) : "-" },
    { key: "running", value: r ? formatInt(r.running) : "-" },
    { key: "documents", value: r ? formatInt(r.documents) : "-" },
    { key: "llmCalls", value: r ? formatInt(r.llm_calls) : "-" },
    { key: "cacheHits", value: r ? formatInt(r.response_cache_hits) : "-" },
    { key: "cost", value: r ? formatCostMicroUsd(r.cost_micro_usd) : "-" },
  ];
  return (
    <section aria-label={t("header.title")} className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-base font-semibold">{t("header.title")}</h2>
        <span className="inline-flex items-center gap-2 text-xs text-muted-foreground" data-connection={connection}>
          <span className={cn("size-2 rounded-pill", DOT[connection])} aria-hidden />
          {catchingUp ? t("catchingUp") : t(`connection.${connection}`)}
        </span>
      </div>
      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
        {tiles.map((tile) => (
          <div key={tile.key} className="rounded-xl border bg-panel px-4 py-3">
            <dt className="text-xs text-muted-foreground">{t(`header.${tile.key}`)}</dt>
            <dd className="mt-1 text-xl font-semibold tabular-nums" dir="ltr">{tile.value}</dd>
          </div>
        ))}
      </dl>
      {summary.isError && <p className="text-sm text-bad">{t("header.loadError")}</p>}
    </section>
  );
}

"use client";

import { useTranslations } from "next-intl";
import { formatInt } from "@/lib/format";
import { formatDuration, rosterWithStats } from "@/lib/agent-roster";
import { cn } from "@/lib/utils";
import { useAgentSummary } from "./use-agent-summary";

/** The agent roster: the four live agents with today's calls and average time, and the ones still to come. */
export function RosterCards() {
  const t = useTranslations("P1Agents.roster");
  const summary = useAgentSummary();
  const roster = rosterWithStats(summary.data?.agents ?? []);
  return (
    <section aria-label={t("title")} className="flex flex-col gap-3">
      <h2 className="text-base font-semibold">{t("title")}</h2>
      <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {roster.map((a) => {
          const active = a.status === "active";
          return (
            <li
              key={a.agent}
              data-agent={a.agent}
              data-status={a.status}
              className={cn("flex flex-col gap-2 rounded-xl border bg-panel p-4", !active && "opacity-60")}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="font-semibold">{t(`names.${a.agent}`)}</span>
                <span
                  className={cn(
                    "rounded-pill px-2 py-0.5 text-xs font-semibold",
                    active ? "bg-ok-soft text-ok" : "bg-muted text-muted-foreground",
                  )}
                >
                  {t(a.status)}
                </span>
              </div>
              <p className="text-xs text-muted-foreground">{t(`roles.${a.agent}`)}</p>
              <p className="font-mono text-xs text-muted-foreground" dir="ltr">{a.model}</p>
              {active && (
                <dl className="mt-1 flex gap-4 text-xs">
                  <div>
                    <dt className="text-muted-foreground">{t("calls")}</dt>
                    <dd className="font-semibold tabular-nums" dir="ltr">{formatInt(a.calls)}</dd>
                  </div>
                  <div>
                    <dt className="text-muted-foreground">{t("avg")}</dt>
                    <dd className="font-semibold tabular-nums" dir="ltr">{formatDuration(a.avg_ms)}</dd>
                  </div>
                </dl>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

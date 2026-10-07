"use client";

import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useTranslations } from "next-intl";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { requestJson } from "@/lib/api-client";
import { formatCostMicroUsd, formatDuration } from "@/lib/agent-roster";
import type { ProposalRow, RunRow, StepRow } from "@/lib/agent-feed";
import { translateFeed } from "@/lib/feed-message";
import { formatInt } from "@/lib/format";
import { DagView } from "./dag-view";
import { ProposalsList } from "./proposals-list";
import { StatusBadge } from "./status-badge";

type Detail = { run: RunRow; steps: StepRow[] | null; proposals: ProposalRow[] | null };

/** Side drawer for one run: totals, the plan as a DAG, the step timeline and the run's proposals. */
export function RunDrawer({ runId, onClose }: { runId: string | null; onClose: () => void }) {
  const t = useTranslations();
  const detail = useQuery({
    queryKey: ["agents", "run", runId],
    enabled: runId !== null,
    queryFn: ({ signal }) => requestJson<Detail>(`/api/agents/runs/${encodeURIComponent(runId ?? "")}`, { signal }),
    refetchInterval: (q) => (q.state.data?.run.status === "running" ? 3000 : false),
  });
  const run = detail.data?.run;
  const steps = detail.data?.steps ?? [];
  const proposals = detail.data?.proposals ?? [];
  const started = run ? Date.parse(run.started_at) : NaN;
  const finished = run?.finished_at ? Date.parse(run.finished_at) : NaN;

  return (
    <Dialog open={runId !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        data-testid="run-drawer"
        className="inset-y-0 end-0 start-auto top-0 h-dvh max-h-dvh w-full max-w-none translate-x-0 translate-y-0 content-start overflow-y-auto rounded-none rtl:translate-x-0 sm:max-w-2xl"
      >
        <DialogTitle>{t("P1Agents.drawer.title")}</DialogTitle>
        <DialogDescription>{run?.workflow || t("P1Agents.drawer.description")}</DialogDescription>
        {detail.isPending && runId !== null && (
          <p className="flex items-center gap-2 text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> {t("P1Agents.drawer.loading")}
          </p>
        )}
        {detail.isError && <p className="text-bad">{t("P1Agents.drawer.error")}</p>}
        {run && (
          <div className="flex flex-col gap-5">
            <div className="flex flex-wrap items-center gap-3">
              <StatusBadge status={run.status} />
              {run.error_code && (
                <span className="font-mono text-xs text-bad" dir="ltr">{t("P1Agents.drawer.error_code")}: {run.error_code}</span>
              )}
            </div>
            <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              {[
                ["llmCalls", formatInt(run.totals.llm_calls)],
                ["tokens", `${formatInt(run.totals.input_tokens)} / ${formatInt(run.totals.output_tokens)}`],
                ["cost", formatCostMicroUsd(run.totals.cost_micro_usd)],
                ["duration", Number.isFinite(started) && Number.isFinite(finished) ? formatDuration(finished - started) : "-"],
              ].map(([k, v]) => (
                <div key={k} className="rounded-lg border px-3 py-2">
                  <dt className="text-xs text-muted-foreground">{t(`P1Agents.drawer.totals.${k}`)}</dt>
                  <dd className="text-sm font-semibold tabular-nums" dir="ltr">{v}</dd>
                </div>
              ))}
            </dl>
            <section className="flex flex-col gap-2">
              <h3 className="font-semibold">{t("P1Agents.drawer.plan")}</h3>
              <DagView plan={run.plan} steps={steps} />
            </section>
            <section className="flex flex-col gap-2">
              <h3 className="font-semibold">{t("P1Agents.drawer.steps")}</h3>
              {steps.length === 0 ? (
                <p className="text-sm text-muted-foreground">{t("P1Agents.drawer.noSteps")}</p>
              ) : (
                <ol className="flex flex-col divide-y rounded-lg border text-sm">
                  {steps.map((s) => (
                    <li key={s.id} className="flex items-center justify-between gap-3 px-3 py-2">
                      <span className="min-w-0 truncate" dir="auto">
                        {translateFeed(t, s.message_key || "step", {
                          agent: s.agent, action: s.action, status: s.status, ...(s.message_args ?? {}),
                        })}
                      </span>
                      <StatusBadge status={s.status} />
                    </li>
                  ))}
                </ol>
              )}
            </section>
            <ProposalsList rows={proposals} />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

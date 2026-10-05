"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Check, CircleAlert, Copy, Download, FileCheck2, Loader2, RefreshCw, ShieldCheck } from "lucide-react";
import { useTranslations } from "next-intl";
import { useState, type ReactNode } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { StatusPill, isInvoiceStatus } from "@/components/ui/status-pill";
import { Link } from "@/i18n/navigation";
import { ApiError } from "@/lib/api-client";
import { pollExhausted } from "@/lib/demo";
import {
  approveInvoice, createExport, downloadExport, getDetail, postCorrection, revalidate,
} from "@/lib/p2/api";
import { availableActions, isSettledStatus } from "@/lib/p2/actions";
import { buildCorrection, valueAtPath } from "@/lib/p2/corrections";
import { formatDurationUs } from "@/lib/p2/display";
import { detailRefetchInterval } from "@/lib/p2/polling";
import type { ExportRecord } from "@/lib/p2/types";
import { cn } from "@/lib/utils";
import { AuditTimeline } from "./audit-timeline";
import { IssueList } from "./issue-list";
import { useErrorText } from "./use-error-text";

type Action = "revalidate" | "correct" | "approve" | "export";

/** /invoices/[id]: the guided review. Real backend only; every step re-reads the invoice afterwards. */
export function ReviewView({ id }: { id: string }) {
  const t = useTranslations("P2Invoices");
  const errorText = useErrorText();
  const qc = useQueryClient();
  const detailKey = ["p2-invoice", id] as const;

  const detail = useQuery({
    queryKey: detailKey,
    queryFn: ({ signal }) => getDetail(id, signal),
    retry: false,
    staleTime: 0,
    refetchInterval: (q) =>
      detailRefetchInterval({
        dataUpdateCount: q.state.dataUpdateCount,
        errorUpdateCount: q.state.errorUpdateCount,
        status: q.state.data?.status,
        errorStatus: q.state.error ? (q.state.error instanceof ApiError ? q.state.error.status : 0) : null,
      }),
  });
  const inv = detail.data;
  const run = inv?.latest_run ?? null;
  const status = inv?.status;
  const settled = isSettledStatus(status);
  const timedOut =
    !!inv &&
    !settled &&
    pollExhausted({
      dataUpdateCount: qc.getQueryState(detailKey)?.dataUpdateCount ?? 0,
      errorUpdateCount: detail.errorUpdateCount,
    });
  const polling = !!inv && !settled && !timedOut && !detail.isError;

  const [actionError, setActionError] = useState<unknown>(null);
  const [exported, setExported] = useState<ExportRecord | null>(null);
  const [savingPath, setSavingPath] = useState<string | null>(null);

  const refresh = () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: detailKey }),
      qc.invalidateQueries({ queryKey: ["p2-audit", id] }),
      qc.invalidateQueries({ queryKey: ["p2-invoices"] }),
    ]);
  const handlers = {
    onMutate: () => setActionError(null),
    // Always re-read: a 409 (stale payload, wrong state) means what is on screen is out of date.
    onSettled: () => refresh(),
    onError: (e: unknown) => setActionError(e),
  };

  const validateM = useMutation({ mutationFn: () => revalidate(id), ...handlers });
  const correctM = useMutation<unknown, unknown, { path: string; value: string; reason: string }>({
    mutationFn: (input: { path: string; value: string; reason: string }) => {
      const body = buildCorrection({
        payload: inv?.payload,
        payloadVersion: inv?.payload_version ?? 0,
        path: input.path,
        value: input.value,
        reason: input.reason,
      });
      if (!body) return Promise.resolve(null);
      return postCorrection(id, body);
    },
    ...handlers,
    onMutate: (v) => {
      setActionError(null);
      setSavingPath(v.path);
    },
    onSettled: () => {
      setSavingPath(null);
      return refresh();
    },
  });
  const approveM = useMutation({
    mutationFn: () => approveInvoice(id, inv?.payload_version ?? 0),
    ...handlers,
  });
  const exportM = useMutation({
    mutationFn: async () => {
      const rec = await createExport(id);
      setExported(rec);
      await downloadExport(rec);
      return rec;
    },
    ...handlers,
  });

  const pendingAction: Action | null = validateM.isPending
    ? "revalidate"
    : correctM.isPending
      ? "correct"
      : approveM.isPending
        ? "approve"
        : exportM.isPending
          ? "export"
          : null;
  // A mutation stays pending until its refetch finished, so the buttons never flicker on the poll.
  const busy = pendingAction !== null;

  if (detail.isPending) {
    return (
      <p data-testid="review-loading" className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" aria-hidden /> {t("review.loading")}
      </p>
    );
  }
  if (!inv) {
    const notFound = detail.error instanceof ApiError && detail.error.status === 404;
    return (
      <div role="alert" data-testid="review-error" className="flex max-w-xl flex-col items-start gap-3 rounded-lg border border-bad/35 bg-bad-soft/70 p-4">
        <p className="flex items-start gap-2 text-sm font-medium">
          <CircleAlert className="mt-0.5 size-4 shrink-0 text-bad" aria-hidden />
          {notFound ? t("review.notFound") : `${t("review.loadError")} ${errorText(detail.error)}`}
        </p>
        <div className="flex gap-2">
          {!notFound && <Button variant="outline" size="sm" onClick={() => void detail.refetch()}>{t("review.retry")}</Button>}
          <Button variant="ghost" size="sm" asChild>
            <Link href="/invoices">{t("review.back")}</Link>
          </Button>
        </div>
      </div>
    );
  }

  const errors = run?.error_count ?? 0;
  const warnings = run?.warning_count ?? 0;
  const actions = availableActions(status, errors);
  const next = status && t.has(`review.next.${status}`) ? t(`review.next.${status}`) : null;
  const text = (path: string) => valueAtPath(inv.payload, path) || "—";
  const traceId = run?.trace_id || "";

  const onCorrect = (path: string, value: string, rule: { ruleId: string; suggestion: boolean }) =>
    correctM.mutate({
      path,
      value,
      reason: rule.suggestion ? `Applied validator suggestion for ${rule.ruleId}` : `Manual correction for ${rule.ruleId}`,
    });

  return (
    <div className="flex flex-col gap-5">
      <Link href="/invoices" className="inline-flex w-fit items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="size-4 rtl:-scale-x-100" aria-hidden />
        {t("review.back")}
      </Link>

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_380px]">
        <div className="flex min-w-0 flex-col gap-5">
          {/* Summary: status, ruleset, counts, trace. */}
          <section className="lift flex flex-col gap-4 rounded-xl border bg-panel p-[18px]">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="flex min-w-0 flex-col gap-1">
                <h2 className="truncate text-lg font-semibold" dir="auto" data-testid="invoice-number">
                  {text("invoice_number")}
                </h2>
                <p className="text-xs text-muted-foreground" dir="auto">
                  {t("review.seller")}: {text("seller.name")} · {t("review.buyer")}: {text("buyer.name")}
                </p>
                <p className="num text-xs text-muted-foreground" dir="ltr">
                  {t("review.total")}: {text("total_amount")} {text("currency")} · {t("review.version", { version: inv.payload_version })}
                </p>
              </div>
              <div className="flex flex-col items-end gap-2">
                <div data-testid="review-status" data-status={status} aria-live="polite" className="flex items-center gap-2">
                  {polling && <Loader2 className="size-4 animate-spin text-info" aria-label={t("review.live")} />}
                  {isInvoiceStatus(status) ? (
                    <StatusPill status={status} />
                  ) : (
                    <span className="rounded-pill bg-panel-2 px-2.5 py-1 text-xs font-semibold">{status}</span>
                  )}
                </div>
                <span
                  data-testid="ruleset-version"
                  className="rounded-sm bg-panel-2 px-2 py-0.5 font-mono text-[11px] text-muted-foreground"
                  dir="ltr"
                >
                  {t("review.ruleset")}: {inv.ruleset_version ?? run?.ruleset_version ?? "—"}
                </span>
              </div>
            </div>

            <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <Stat label={t("review.stats.errors")} testId="stat-errors" tone={errors > 0 ? "bad" : undefined}>{run ? errors : "—"}</Stat>
              <Stat label={t("review.stats.warnings")} testId="stat-warnings" tone={warnings > 0 ? "warn" : undefined}>{run ? warnings : "—"}</Stat>
              <Stat label={t("review.stats.rules")} testId="stat-rules">{run ? run.rules_evaluated : "—"}</Stat>
              <Stat label={t("review.stats.duration")} testId="stat-duration">{run ? formatDurationUs(run.duration_us) : "—"}</Stat>
            </dl>

            {!run && <p className="text-sm text-muted-foreground">{t("review.noRun")}</p>}
            {next && <p data-testid="review-next" className="text-sm leading-6">{next}</p>}
            {polling && <p className="text-xs text-muted-foreground">{t("review.pending")}</p>}
            {timedOut && (
              <p role="alert" data-testid="review-timeout" className="flex flex-wrap items-center gap-2 text-sm text-warn">
                {t("review.timeout")}
                <Button variant="outline" size="sm" onClick={() => void refresh()}>
                  <RefreshCw aria-hidden /> {t("review.refresh")}
                </Button>
              </p>
            )}
            {detail.isError && (
              <p role="alert" className="text-sm text-bad">{t("review.loadError")} {errorText(detail.error)}</p>
            )}
            {inv.approval && status === "ready" && (
              <p className="text-xs text-muted-foreground">{t("review.approvedBy", { who: inv.approval.approved_by })}</p>
            )}

            <dl className="grid gap-2 border-t pt-3.5 text-[13px]">
              <MetaRow label={t("review.invoiceId")} value={inv.id} testId="invoice-id" />
              <MetaRow label={t("review.traceId")} value={traceId} testId="trace-id" />
            </dl>
          </section>

          {/* Guided actions. */}
          <section className="lift flex flex-col gap-3 rounded-xl border bg-panel p-[18px]">
            <h2 className="text-[15px] font-semibold">{t("actions.title")}</h2>
            <div className="flex flex-wrap items-center gap-2.5">
              <Button
                variant="outline"
                data-testid="revalidate"
                disabled={!actions.revalidate || busy}
                onClick={() => validateM.mutate()}
              >
                {pendingAction === "revalidate" ? <Loader2 className="animate-spin" aria-hidden /> : <RefreshCw aria-hidden />}
                {pendingAction === "revalidate" ? t("actions.revalidating") : t("actions.revalidate")}
              </Button>
              {status === "validated" && (
                <Button data-testid="approve" disabled={!actions.approve || busy} onClick={() => approveM.mutate()}>
                  {pendingAction === "approve" ? <Loader2 className="animate-spin" aria-hidden /> : <ShieldCheck aria-hidden />}
                  {pendingAction === "approve" ? t("actions.approving") : t("actions.approve")}
                </Button>
              )}
              {status === "ready" && (
                <Button data-testid="export" disabled={!actions.export || busy} onClick={() => exportM.mutate()}>
                  {pendingAction === "export" ? <Loader2 className="animate-spin" aria-hidden /> : <FileCheck2 aria-hidden />}
                  {pendingAction === "export" ? t("actions.exporting") : t("actions.export")}
                </Button>
              )}
            </div>

            {actionError !== null && (
              <p role="alert" data-testid="action-error" className="flex items-start gap-2 text-sm text-bad">
                <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
                {errorText(actionError)}
              </p>
            )}

            {exported && (
              <div data-testid="export-done" className="flex flex-wrap items-center gap-3 rounded-lg border border-ok/35 bg-ok-soft/70 p-3.5">
                <Check className="size-5 shrink-0 text-ok" aria-hidden />
                <div className="flex min-w-0 grow flex-col gap-0.5">
                  <span className="truncate text-sm font-semibold text-ok" dir="ltr">
                    {t("actions.exported", { filename: exported.filename })}
                  </span>
                  <span className="truncate font-mono text-[11px] text-muted-foreground" dir="ltr">
                    {t("actions.exportMeta", { size: exported.size_bytes, sha: exported.sha256.slice(0, 16) })}
                  </span>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void downloadExport(exported).catch((e: unknown) => setActionError(e))}
                >
                  <Download aria-hidden /> {t("actions.download")}
                </Button>
              </div>
            )}
          </section>

          {/* Issues. */}
          <section className="lift flex flex-col gap-3.5 rounded-xl border bg-panel p-[18px]">
            <h2 className="flex items-center gap-2 text-[15px] font-semibold">
              {t("issues.title")}
              <span className="num rounded-pill bg-panel-2 px-2 py-px text-[11px] text-muted-foreground">
                {(run?.issues ?? []).length}
              </span>
            </h2>
            {run || settled ? (
              <IssueList
                issues={run?.issues ?? []}
                payload={inv.payload}
                canCorrect={actions.correct}
                busy={busy}
                savingPath={savingPath}
                onCorrect={onCorrect}
              />
            ) : (
              <p className="text-sm text-muted-foreground">{t("review.pending")}</p>
            )}
          </section>
        </div>

        <AuditTimeline invoiceId={id} />
      </div>
    </div>
  );
}

function Stat({ label, testId, tone, children }: { label: string; testId: string; tone?: "bad" | "warn"; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 rounded-lg bg-panel-2 px-3 py-2">
      <dt className="text-[11px] text-muted-foreground">{label}</dt>
      <dd data-testid={testId} className={cn("num text-lg font-semibold", tone === "bad" && "text-bad", tone === "warn" && "text-warn")}>
        {children}
      </dd>
    </div>
  );
}

function MetaRow({ label, value, testId }: { label: string; value: string; testId: string }) {
  const t = useTranslations("P2Invoices.review");
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(t("copied"));
    } catch {
      /* clipboard unavailable (insecure context): the value stays selectable */
    }
  };
  return (
    <div className="flex items-center gap-3">
      <dt className="w-28 shrink-0 text-xs text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 grow items-center gap-2 font-mono text-[12px]" dir="ltr">
        <span data-testid={testId} className="flex min-w-0 grow truncate rounded-sm bg-panel-2 px-2 py-1 select-all">
          {value || "—"}
        </span>
        {value && (
          <button
            type="button"
            onClick={onCopy}
            aria-label={`${t("copy")} ${label}`}
            className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-panel text-muted-foreground transition-colors hover:border-brand/40 hover:text-foreground"
          >
            <Copy className="size-3.5" strokeWidth={2} aria-hidden />
          </button>
        )}
      </dd>
    </div>
  );
}

"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, CircleAlert, Copy, FlaskConical, Loader2, Send, TriangleAlert } from "lucide-react";
import { useTranslations } from "next-intl";
import { useId, useState, useSyncExternalStore, type FormEvent, type ReactNode } from "react";
import { toast } from "sonner";
import { GeometricPattern } from "@/components/geometric-pattern";
import { StatusPill, isInvoiceStatus } from "@/components/ui/status-pill";
import { DEMO_FIXED, demoInvoice, isTerminalStatus, localDateISO, pollInterval } from "@/lib/demo";
import { HttpError, type InvoiceIssue, type InvoiceResult } from "@/lib/invoice";
import { cn } from "@/lib/utils";

type Submission = { id: string; status: string; traceId: string | null };

/** Stop polling after ~2 minutes even if the pipeline never reaches a terminal state. */
const MAX_POLLS = 120;
const VALID_TRN = "100000000000003";
const INVALID_TRN = "123";

const noopSubscribe = () => () => {};

async function submitInvoice(sellerTrn: string): Promise<Submission> {
  const res = await fetch("/api/invoices", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(demoInvoice(sellerTrn, new Date())),
  });
  const traceId = res.headers.get("x-trace-id") || null;
  if (!res.ok) throw new HttpError(res.status, traceId);
  const body = (await res.json()) as { id: string; status: string };
  return { id: body.id, status: body.status, traceId };
}

async function fetchInvoice(id: string): Promise<InvoiceResult> {
  const res = await fetch(`/api/invoices/${encodeURIComponent(id)}`, { cache: "no-store" });
  if (!res.ok) throw new HttpError(res.status, res.headers.get("x-trace-id") || null);
  return (await res.json()) as InvoiceResult;
}

/**
 * Validation demo: submits a fixed demo invoice (only the seller TRN is editable) through the
 * BFF, then polls the result every second until the validator has decided.
 */
export function DemoForm() {
  const t = useTranslations("DemoPage");
  const tDemo = useTranslations("Demo");
  const trnId = useId();
  const helpId = useId();
  const [sellerTrn, setSellerTrn] = useState(INVALID_TRN);
  // The viewer's local date; empty on the server so a prerender never shows a stale day.
  const today = useSyncExternalStore(noopSubscribe, () => localDateISO(new Date()), () => "");

  const submit = useMutation({ mutationFn: submitInvoice });
  const submission = submit.data;

  const invoice = useQuery({
    queryKey: ["invoice", submission?.id],
    queryFn: () => fetchInvoice(submission!.id),
    enabled: !!submission?.id,
    retry: false,
    staleTime: 0,
    refetchInterval: (q) => {
      if (q.state.dataUpdateCount >= MAX_POLLS) return false;
      // A 404 right after the 202 is a read-your-write race: keep polling. Other errors stop.
      if (q.state.error) return q.state.error instanceof HttpError && q.state.error.status === 404 ? 1000 : false;
      return pollInterval(q.state.data?.status);
    },
  });

  const status = invoice.data?.status ?? submission?.status;
  const error = submit.error ?? (invoice.error instanceof HttpError && invoice.error.status !== 404 ? invoice.error : null);
  const polling = !!submission && !error && !isTerminalStatus(status);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    submit.mutate(sellerTrn);
  };

  const fixed: { key: string; label: string; value: string; wide?: boolean }[] = [
    { key: "invoiceNumber", label: t("fields.invoiceNumber"), value: DEMO_FIXED.invoice_number },
    { key: "issueDate", label: t("fields.issueDate"), value: today || " " },
    { key: "buyerTrn", label: t("fields.buyerTrn"), value: DEMO_FIXED.buyer_trn, wide: true },
    { key: "currency", label: t("fields.currency"), value: DEMO_FIXED.currency },
    { key: "vat", label: t("fields.vat"), value: DEMO_FIXED.vat_amount },
    { key: "total", label: t("fields.total"), value: DEMO_FIXED.total_amount, wide: true },
  ];

  return (
    <div className="grid gap-5 xl:grid-cols-[400px_minmax(0,1fr)]">
      {/* Demo invoice (prototype Review board: extracted-data column). */}
      <section className="lift flex flex-col self-start overflow-hidden rounded-xl border bg-panel">
        <header className="flex flex-col gap-1 border-b px-[18px] py-4">
          <h2 className="text-[15px] font-semibold">{t("invoiceTitle")}</h2>
          <p className="text-xs text-muted-foreground">{t("invoiceHint")}</p>
        </header>

        <form onSubmit={onSubmit} className="flex flex-col gap-5 p-[18px]">
          <div className="flex flex-col gap-2">
            <label htmlFor={trnId} className="text-[13px] font-medium">
              {tDemo("sellerTrn")}
            </label>
            <input
              id={trnId}
              data-testid="seller-trn"
              name="seller_trn"
              value={sellerTrn}
              onChange={(e) => setSellerTrn(e.target.value)}
              dir="ltr"
              inputMode="numeric"
              autoComplete="off"
              spellCheck={false}
              aria-describedby={helpId}
              className="h-12 w-full rounded-lg border bg-panel-2 px-4 text-start font-mono text-[15px] tracking-[0.5px] transition-colors outline-none hover:border-brand/40 focus-visible:border-brand focus-visible:ring-2 focus-visible:ring-ring/40 rtl:text-end"
            />
            <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
              <span id={helpId} className="grow text-xs text-muted-foreground">
                {t("trnHelp")}
              </span>
              <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                {t("presets")}
                <PresetChip tone="warn" label={t("presetInvalid")} value={INVALID_TRN} onPick={setSellerTrn} />
                <PresetChip tone="ok" label={t("presetValid")} value={VALID_TRN} onPick={setSellerTrn} />
              </span>
            </div>
          </div>

          <dl className="grid grid-cols-2 gap-x-3 gap-y-3.5">
            {fixed.map((f) => (
              <div key={f.key} className={cn("flex flex-col gap-1.5", f.wide && "col-span-2")}>
                <dt className="text-xs text-muted-foreground">{f.label}</dt>
                <dd className="rounded-md bg-panel-2 px-2.5 py-1.5 font-mono text-sm num" dir="ltr">
                  <span className="block text-start rtl:text-end">{f.value}</span>
                </dd>
              </div>
            ))}
          </dl>

          <button
            type="submit"
            data-testid="submit"
            disabled={submit.isPending || sellerTrn.trim() === ""}
            className="group flex h-[46px] items-center justify-center gap-2.5 rounded-lg bg-primary text-sm font-bold text-primary-foreground transition-[filter,transform,opacity] hover:brightness-110 active:translate-y-px disabled:cursor-not-allowed disabled:opacity-60"
          >
            {submit.isPending ? (
              <Loader2 className="size-4 animate-spin" strokeWidth={2.2} aria-hidden />
            ) : (
              <Send className="size-4 rtl:-scale-x-100" strokeWidth={2} aria-hidden />
            )}
            {submit.isPending ? t("submitting") : tDemo("submit")}
          </button>
        </form>
      </section>

      {/* Validation result (prototype Review board: result card). */}
      <section className="lift flex flex-col gap-3.5 self-start rounded-xl border bg-panel p-[18px]">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="flex items-center gap-2.5 text-[15px] font-semibold">
            {t("resultTitle")}
            {polling && (
              <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-info">
                <span className="relative flex size-2">
                  <span className="absolute inline-flex size-full animate-ping rounded-pill bg-info opacity-60" />
                  <span className="relative inline-flex size-2 rounded-pill bg-info" />
                </span>
                {t("live")}
              </span>
            )}
          </h2>
          <span className="rounded-sm bg-panel-2 px-2 py-0.5 font-mono text-[11px] text-muted-foreground" dir="ltr">
            {invoice.data?.ruleset_version ?? t("ruleset")}
          </span>
        </div>

        {status && (
          <div className="flex items-center gap-3">
            <span className="text-xs text-muted-foreground">{tDemo("status")}</span>
            <div data-testid="status" data-status={status} aria-live="polite" className="flex items-center gap-2">
              {isInvoiceStatus(status) ? (
                <StatusPill status={status} />
              ) : (
                <span className="rounded-pill bg-panel-2 px-2.5 py-1 text-xs font-semibold">{status}</span>
              )}
              <code aria-hidden className="font-mono text-[11px] text-muted-foreground" dir="ltr">
                {status}
              </code>
            </div>
          </div>
        )}

        <div className="flex flex-col gap-3">
          {!submission && !error && <IdleState />}
          {error && <ErrorCard error={error} />}
          {submission && !error && !isTerminalStatus(status) && <PendingState status={status} />}
          {!error && status === "has_issues" && <IssueList issues={invoice.data?.issues ?? []} />}
          {!error && status === "validated" && (invoice.data?.issues.length ?? 0) === 0 && <ValidatedCard />}
          {!error && status === "validated" && (invoice.data?.issues.length ?? 0) > 0 && (
            <IssueList issues={invoice.data?.issues ?? []} />
          )}
        </div>

        {(submission || (error instanceof HttpError && error.traceId)) && (
          <dl className="grid gap-2 border-t pt-3.5 text-[13px]">
            {submission && (
              <MetaRow label={t("invoiceId")}>
                <span className="truncate">{submission.id}</span>
              </MetaRow>
            )}
            <MetaRow label={tDemo("traceId")} copy={submission?.traceId ?? (error as HttpError | null)?.traceId}>
              <span data-testid="trace-id" className="truncate">
                {submission?.traceId ?? (error as HttpError | null)?.traceId ?? "—"}
              </span>
            </MetaRow>
          </dl>
        )}
      </section>
    </div>
  );
}

function PresetChip({
  tone,
  label,
  value,
  onPick,
}: {
  tone: "ok" | "warn";
  label: string;
  value: string;
  onPick: (v: string) => void;
}) {
  return (
    <button
      type="button"
      onClick={() => onPick(value)}
      title={value}
      className={cn(
        "rounded-pill border px-2 py-0.5 text-[11px] font-medium transition-colors",
        tone === "ok"
          ? "border-ok/30 bg-ok-soft text-ok hover:border-ok/60"
          : "border-warn/30 bg-warn-soft text-warn hover:border-warn/60",
      )}
    >
      {label}
    </button>
  );
}

function IdleState() {
  const t = useTranslations("DemoPage");
  return (
    <div className="relative flex grow flex-col items-center justify-center gap-4 overflow-hidden rounded-lg border border-dashed px-6 py-14 text-center">
      <GeometricPattern className="opacity-70" tile={64} focus="50% 50%" />
      <span className="relative flex size-12 items-center justify-center rounded-lg bg-brand-soft text-brand-ink">
        <FlaskConical className="size-6" strokeWidth={1.8} aria-hidden />
      </span>
      <div className="relative flex max-w-sm flex-col gap-2">
        <h3 className="text-base font-semibold">{t("idleTitle")}</h3>
        <p className="text-sm leading-6 text-muted-foreground">{t("idleBody")}</p>
      </div>
    </div>
  );
}

const STEPS = ["uploaded", "extracted", "validated"] as const;

function PendingState({ status }: { status: string | undefined }) {
  const t = useTranslations("DemoPage");
  const tStatus = useTranslations("Status");
  const current = Math.max(0, STEPS.indexOf((status ?? "uploaded") as (typeof STEPS)[number]));
  return (
    <div className="flex flex-col gap-5 rounded-lg border border-info/30 bg-info-soft/60 p-4 animate-in fade-in slide-in-from-bottom-1 duration-300">
      <div className="flex items-center gap-3">
        <Loader2 className="size-5 shrink-0 animate-spin text-info" strokeWidth={2} aria-hidden />
        <div className="flex flex-col gap-0.5">
          <span className="text-sm font-semibold">{t("pendingTitle")}</span>
          <span className="text-xs text-muted-foreground">{t("pendingBody")}</span>
        </div>
      </div>
      <ol className="flex items-center gap-2">
        {STEPS.map((s, i) => {
          const done = i < current;
          const active = i === current;
          return (
            <li key={s} className="flex grow items-center gap-2">
              <span
                className={cn(
                  "flex size-6 shrink-0 items-center justify-center rounded-pill border text-[11px] font-semibold num",
                  done && "border-ok/40 bg-ok-soft text-ok",
                  active && "border-info/50 bg-panel text-info",
                  !done && !active && "bg-panel text-muted-foreground",
                )}
              >
                {done ? <Check className="size-3.5" strokeWidth={2.5} aria-hidden /> : i + 1}
              </span>
              <span className={cn("text-xs whitespace-nowrap", active ? "font-semibold" : "text-muted-foreground")}>
                {tStatus(s)}
              </span>
              {i < STEPS.length - 1 && (
                <span className={cn("h-px grow", done ? "bg-ok/50" : "bg-border")} aria-hidden />
              )}
            </li>
          );
        })}
      </ol>
    </div>
  );
}

function IssueList({ issues }: { issues: InvoiceIssue[] }) {
  const t = useTranslations("DemoPage");
  const tDemo = useTranslations("Demo");
  return (
    <div className="flex flex-col gap-2.5">
      <h3 className="flex items-center gap-2 text-[13px] font-semibold">
        {tDemo("issues")}
        <span className="num rounded-pill bg-warn-soft px-2 py-px text-[11px] text-warn">{issues.length}</span>
      </h3>
      <ul className="flex flex-col gap-2.5">
        {issues.map((issue, i) => {
          const warning = issue.severity === "warning";
          return (
            <li
              key={`${issue.rule_id}-${issue.path}-${i}`}
              data-testid="issue"
              className={cn(
                "flex flex-col gap-2 rounded-lg border p-3.5 animate-in fade-in slide-in-from-bottom-1 duration-300",
                warning ? "border-info/30 bg-info-soft/50" : "border-warn/35 bg-warn-soft/60",
              )}
            >
              <div className="flex flex-wrap items-center gap-2">
                <span
                  dir="ltr"
                  className={cn(
                    "rounded-sm px-2 py-0.5 font-mono text-[11px] font-medium",
                    warning ? "bg-info-soft text-info" : "bg-warn-soft text-warn",
                  )}
                >
                  {issue.rule_id}
                </span>
                <span className={cn("text-xs font-semibold", warning ? "text-info" : "text-warn")}>
                  {t(`severity.${warning ? "warning" : "error"}`)}
                </span>
              </div>
              <p className="text-sm leading-6 font-medium">{issue.message}</p>
              {issue.path && (
                <p className="text-xs text-muted-foreground">
                  {t("path")}:{" "}
                  <span dir="ltr" className="font-mono text-foreground">
                    {issue.path}
                  </span>
                </p>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function ValidatedCard() {
  const t = useTranslations("DemoPage");
  const tDemo = useTranslations("Demo");
  return (
    <div className="flex items-center gap-3 rounded-lg border border-ok/35 bg-ok-soft/70 p-4 animate-in fade-in slide-in-from-bottom-1 duration-300">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-pill bg-ok-soft text-ok">
        <Check className="size-5" strokeWidth={2.2} aria-hidden />
      </span>
      <div className="flex flex-col gap-0.5">
        <span className="text-sm font-semibold text-ok">{tDemo("none")}</span>
        <span className="text-xs text-muted-foreground">{t("validatedBody")}</span>
      </div>
    </div>
  );
}

function ErrorCard({ error }: { error: Error }) {
  const t = useTranslations("DemoPage");
  const code = error instanceof HttpError ? String(error.status) : "other";
  return (
    <div
      role="alert"
      className="flex items-start gap-3 rounded-lg border border-bad/35 bg-bad-soft/70 p-4 animate-in fade-in duration-300"
    >
      {code === "429" ? (
        <TriangleAlert className="mt-0.5 size-5 shrink-0 text-bad" strokeWidth={2} aria-hidden />
      ) : (
        <CircleAlert className="mt-0.5 size-5 shrink-0 text-bad" strokeWidth={2} aria-hidden />
      )}
      <div className="flex flex-col gap-1">
        <span className="text-sm font-semibold">
          {t("errorTitle")}
          {error instanceof HttpError && (
            <span className="num ms-2 rounded-sm bg-bad-soft px-1.5 py-px font-mono text-[11px] text-bad" dir="ltr">
              {error.status}
            </span>
          )}
        </span>
        <span className="text-[13px] text-muted-foreground">{t("errorBody", { code })}</span>
      </div>
    </div>
  );
}

function MetaRow({ label, copy, children }: { label: string; copy?: string | null; children: ReactNode }) {
  const t = useTranslations("DemoPage");
  const onCopy = async () => {
    if (!copy) return;
    try {
      await navigator.clipboard.writeText(copy);
      toast.success(t("copied"));
    } catch {
      /* clipboard unavailable (insecure context): the id stays selectable */
    }
  };
  return (
    <div className="flex items-center gap-3">
      <dt className="w-28 shrink-0 text-xs text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 grow items-center gap-2 font-mono text-[12px]" dir="ltr">
        <span className="flex min-w-0 grow rounded-sm bg-panel-2 px-2 py-1 select-all">{children}</span>
        {copy && (
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

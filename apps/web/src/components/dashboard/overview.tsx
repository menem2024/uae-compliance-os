"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowRight, FileText, Loader2 } from "lucide-react";
import { useLocale, useTranslations } from "next-intl";
import { Fragment, useMemo, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { StatusPill, isInvoiceStatus, type InvoiceStatus } from "@/components/ui/status-pill";
import { Link } from "@/i18n/navigation";
import { dateOnlyFormat, formatInt, formatMoney, parseDateOnly } from "@/lib/format";
import { DASHBOARD_CAP, fetchDashboard, type DashboardData } from "@/lib/p2/dashboard";

/** Invoice lifecycle; step 3 branches into validated / has_issues. */
const PIPELINE: InvoiceStatus[][] = [["uploaded"], ["extracted"], ["validated", "has_issues"], ["ready"]];
/** The four counts the overview headlines. */
const HEADLINE: InvoiceStatus[] = ["validated", "has_issues", "ready", "needs_review"];

/**
 * Dashboard body. The server page passes the static pieces (the empty-state hero and the countdown) so they
 * stay server-rendered; this component fills in the live counts, the pipeline and the recent invoices.
 */
export function DashboardOverview({ hero, countdown }: { hero: ReactNode; countdown: ReactNode }) {
  const t = useTranslations("Dashboard");
  const q = useQuery({ queryKey: ["dashboard"], queryFn: ({ signal }) => fetchDashboard(signal), staleTime: 15_000 });
  const data = q.data;
  const empty = data !== undefined && data.invoices.total === 0 && data.clients.count === 0;

  return (
    <>
      <div className="grid gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2 [&>section]:h-full">
          {empty ? hero : <Summary data={data} loading={q.isPending} error={q.isError} onRetry={() => void q.refetch()} />}
        </div>
        {countdown}
      </div>

      <section className="lift rounded-xl border bg-panel">
        <div className="flex flex-col gap-1 border-b px-5 py-[18px]">
          <h2 className="text-base font-semibold">{t("pipelineTitle")}</h2>
          <p className="text-[13px] text-muted-foreground">{t("pipelineBody")}</p>
        </div>
        <ol className="grid gap-3 p-5 sm:grid-cols-2 lg:grid-cols-[1fr_auto_1fr_auto_1fr_auto_1fr]">
          {PIPELINE.map((step, i) => (
            <Fragment key={step.join("|")}>
              {i > 0 && (
                <li aria-hidden className="hidden self-center lg:block">
                  <ArrowRight className="size-4 text-muted-foreground/60 rtl:-scale-x-100" />
                </li>
              )}
              <li className="flex items-center gap-3 rounded-lg bg-panel-2 px-4 py-3.5">
                <span className="num flex size-7 shrink-0 items-center justify-center rounded-pill border bg-panel text-xs font-semibold text-muted-foreground">
                  {formatInt(i + 1)}
                </span>
                <span className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                  {step.map((s) => (
                    <span key={s} className="inline-flex items-center gap-1.5">
                      <StatusPill status={s} />
                      <span data-testid={`pipeline-count-${s}`} className="num text-sm font-semibold tabular-nums">
                        {data ? formatInt(data.invoices.byStatus[s] ?? 0) : "—"}
                      </span>
                    </span>
                  ))}
                </span>
              </li>
            </Fragment>
          ))}
        </ol>
      </section>

      <Recent data={data} loading={q.isPending} error={q.isError} />
    </>
  );
}

function Summary({
  data, loading, error, onRetry,
}: { data: DashboardData | undefined; loading: boolean; error: boolean; onRetry: () => void }) {
  const t = useTranslations("Dashboard");
  const truncated = data ? data.invoices.truncated || data.clients.truncated : false;
  return (
    <section className="lift flex flex-col rounded-xl border bg-panel" data-testid="dashboard-summary">
      <div className="flex flex-col gap-1 border-b px-5 py-[18px]">
        <h2 className="text-base font-semibold">{t("overviewTitle")}</h2>
        <p className="text-[13px] text-muted-foreground">{t("overviewBody")}</p>
      </div>
      {loading && (
        <div className="flex grow items-center justify-center p-10 text-muted-foreground"><Loader2 className="size-5 animate-spin" /></div>
      )}
      {error && (
        <div role="alert" className="flex grow flex-col items-center justify-center gap-2 p-10 text-center text-sm text-bad">
          <p>{t("loadError")}</p>
          <Button variant="outline" size="sm" onClick={onRetry}>{t("retry")}</Button>
        </div>
      )}
      {data && (
        <div className="flex flex-col gap-5 p-5">
          <div className="grid grid-cols-2 gap-3">
            <Stat label={t("clients")} value={data.clients.count} testId="count-clients" big />
            <Stat label={t("invoices")} value={data.invoices.total} testId="count-invoices" big />
          </div>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            {HEADLINE.map((s) => (
              <div key={s} className="flex flex-col items-start gap-2 rounded-lg bg-panel-2 px-4 py-3">
                <StatusPill status={s} />
                <span data-testid={`count-${s}`} className="num text-2xl font-semibold tabular-nums">
                  {formatInt(data.invoices.byStatus[s] ?? 0)}
                </span>
              </div>
            ))}
          </div>
          {truncated && <p className="text-xs text-muted-foreground">{t("capped", { count: formatInt(DASHBOARD_CAP) })}</p>}
        </div>
      )}
    </section>
  );
}

function Stat({ label, value, testId, big }: { label: string; value: number; testId: string; big?: boolean }) {
  return (
    <div className="flex flex-col gap-1 rounded-lg border px-4 py-3">
      <span className="text-[13px] text-muted-foreground">{label}</span>
      <span data-testid={testId} className={big ? "num text-3xl font-semibold tabular-nums" : "num text-xl font-semibold tabular-nums"}>
        {formatInt(value)}
      </span>
    </div>
  );
}

function Recent({ data, loading, error }: { data: DashboardData | undefined; loading: boolean; error: boolean }) {
  const t = useTranslations("Dashboard");
  const locale = useLocale();
  const when = useMemo(() => dateOnlyFormat(locale), [locale]);
  const recent = data?.invoices.recent ?? [];
  return (
    <section className="lift rounded-xl border bg-panel" data-testid="dashboard-recent">
      <div className="flex items-start justify-between gap-3 border-b px-5 py-[18px]">
        <div className="flex flex-col gap-1">
          <h2 className="text-base font-semibold">{t("recentTitle")}</h2>
          <p className="text-[13px] text-muted-foreground">{t("recentBody")}</p>
        </div>
        <Link href="/invoices" className="inline-flex items-center gap-1.5 text-sm text-brand-ink underline-offset-4 hover:underline">
          {t("viewAll")}
          <ArrowRight className="size-4 rtl:-scale-x-100" aria-hidden />
        </Link>
      </div>
      {loading && <div className="flex justify-center p-6 text-muted-foreground"><Loader2 className="size-5 animate-spin" /></div>}
      {error && <p role="alert" className="p-6 text-center text-sm text-bad">{t("loadError")}</p>}
      {data && recent.length === 0 && (
        <div className="flex flex-col items-center gap-3 p-8 text-center text-muted-foreground">
          <FileText className="size-6" />
          <p className="text-sm">{t("recentEmpty")}</p>
          <Button variant="outline" size="sm" asChild>
            <Link href="/invoices">{t("recentEmptyCta")}</Link>
          </Button>
        </div>
      )}
      {recent.length > 0 && (
        <ul className="divide-y">
          {recent.map((i) => {
            const d = parseDateOnly(i.issue_date);
            return (
              <li key={i.id} data-testid="recent-invoice" className="flex flex-wrap items-center gap-x-4 gap-y-1 px-5 py-3">
                <Link
                  href={`/invoices/${encodeURIComponent(i.id)}`}
                  className="min-w-0 font-medium text-brand-ink underline-offset-4 hover:underline"
                  dir="auto"
                >
                  {i.invoice_number || "—"}
                </Link>
                <span className="flex min-w-0 flex-1 items-center gap-1.5 text-[13px] text-muted-foreground">
                  <span className="truncate" dir="auto">{i.seller_name || "—"}</span>
                  <ArrowRight className="size-3.5 shrink-0 rtl:-scale-x-100" aria-hidden />
                  <span className="truncate" dir="auto">{i.buyer_name || "—"}</span>
                </span>
                <span className="whitespace-nowrap text-sm tabular-nums" dir="ltr">{formatMoney(i.total_amount)} {i.currency}</span>
                {d && <span className="whitespace-nowrap text-xs text-muted-foreground tabular-nums">{t("issueDate", { date: when.format(d) })}</span>}
                {isInvoiceStatus(i.status) ? <StatusPill status={i.status} /> : <span className="text-xs">{i.status}</span>}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

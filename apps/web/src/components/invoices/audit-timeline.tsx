"use client";

import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useLocale, useTranslations } from "next-intl";
import { useMemo } from "react";
import { getAudit } from "@/lib/p2/api";
import { sortAudit } from "@/lib/p2/display";

/** The invoice's audit trail, oldest first. Refetched by the review view whenever the invoice changes. */
export function AuditTimeline({ invoiceId }: { invoiceId: string }) {
  const t = useTranslations("P2Invoices.audit");
  const locale = useLocale();
  const when = useMemo(
    () => new Intl.DateTimeFormat(`${locale}-u-nu-latn`, { dateStyle: "medium", timeStyle: "medium" }),
    [locale],
  );
  const audit = useQuery({
    queryKey: ["p2-audit", invoiceId],
    queryFn: ({ signal }) => getAudit(invoiceId, signal),
    staleTime: 0,
    retry: false,
  });
  const items = useMemo(() => sortAudit(audit.data?.items ?? []), [audit.data]);

  return (
    <section data-testid="audit" className="lift flex flex-col gap-3.5 rounded-xl border bg-panel p-[18px]">
      <h2 className="text-[15px] font-semibold">{t("title")}</h2>
      {audit.isPending && <Loader2 className="size-4 animate-spin text-muted-foreground" aria-hidden />}
      {audit.isError && <p role="alert" className="text-sm text-bad">{t("loadError")}</p>}
      {audit.isSuccess && items.length === 0 && <p className="text-sm text-muted-foreground">{t("empty")}</p>}
      <ol className="flex flex-col">
        {items.map((e) => {
          const who = e.agent || e.actor_id || t("system");
          const key = /^invoice\.[a-z_]+$/.test(e.action) ? e.action.slice("invoice.".length) : null;
          return (
            <li
              key={e.id}
              data-testid="audit-event"
              data-action={e.action}
              className="relative flex flex-col gap-0.5 border-s ps-4 pb-4 last:pb-0"
            >
              <span className="absolute start-0 top-1.5 size-2 -translate-x-1/2 rounded-pill bg-brand rtl:translate-x-1/2" aria-hidden />
              <span className="text-sm font-medium">{key && t.has(`actions.${key}`) ? t(`actions.${key}`) : e.action}</span>
              <span className="text-xs text-muted-foreground">
                {when.format(new Date(e.occurred_at))} · {t("by", { who })}
              </span>
              {e.reason && (
                <span className="text-xs text-muted-foreground">
                  {t("reason")}: <span dir="auto">{e.reason}</span>
                </span>
              )}
              {e.trace_id && (
                <span className="font-mono text-[11px] text-muted-foreground" dir="ltr">
                  {t("trace")} {e.trace_id}
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </section>
  );
}

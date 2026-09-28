"use client";

import { CalendarClock } from "lucide-react";
import { useTranslations } from "next-intl";
import { useDaysToMandate } from "@/components/shell/mandate-card";
import { formatInt } from "@/lib/format";

/** Large mandate countdown for the dashboard landing. */
export function CountdownCard({ serverDays }: { serverDays: number }) {
  const t = useTranslations("Dashboard");
  const tm = useTranslations("Shell.mandate");
  const days = useDaysToMandate(serverDays);
  return (
    <section className="lift relative flex flex-col overflow-hidden rounded-xl border bg-panel">
      <svg
        width="220"
        height="220"
        viewBox="0 0 40 40"
        aria-hidden="true"
        className="pointer-events-none absolute -end-16 -top-16 opacity-[0.08]"
      >
        <g fill="none" stroke="var(--brand)" strokeWidth="0.8">
          <rect x="9" y="9" width="22" height="22" />
          <rect x="9" y="9" width="22" height="22" transform="rotate(45 20 20)" />
          <circle cx="20" cy="20" r="3.5" />
        </g>
      </svg>
      <div className="relative flex grow flex-col gap-5 p-6">
        <div className="flex items-center gap-2.5">
          <span className="flex size-8 items-center justify-center rounded-md bg-brand-soft text-brand-ink">
            <CalendarClock className="size-[17px]" strokeWidth={1.8} aria-hidden />
          </span>
          <h2 className="text-[15px] font-semibold">{t("countdownTitle")}</h2>
        </div>
        <div className="flex items-baseline gap-2">
          <span className="num text-[56px] leading-none font-semibold tracking-[-1px] text-brand-ink">
            {formatInt(days)}
          </span>
          <span className="text-sm text-muted-foreground">{tm("daysLeft")}</span>
        </div>
        <p className="text-[13px] leading-6 text-muted-foreground">{t("countdownBody")}</p>
      </div>
      <dl className="relative grid grid-cols-2 border-t text-[13px]">
        <div className="flex flex-col gap-1 px-6 py-4">
          <dt className="text-xs text-muted-foreground">{t("countdownDate")}</dt>
          <dd className="num font-semibold">{tm("date")}</dd>
        </div>
        <div className="flex flex-col gap-1 border-s px-6 py-4">
          <dt className="text-xs text-muted-foreground">{t("countdownScope")}</dt>
          <dd className="font-semibold">{t("countdownScopeValue")}</dd>
        </div>
      </dl>
    </section>
  );
}

"use client";

import { useTranslations } from "next-intl";
import { GeometricPattern } from "@/components/geometric-pattern";
import { useDaysToMandate } from "@/components/shell/mandate-card";
import { formatInt } from "@/lib/format";

/**
 * Hero: headline, subcopy and the mandate countdown. Reuses the Phase 0 countdown
 * hook/formatter and the `Shell.mandate` copy instead of restating the numbers.
 */
export function Hero({ serverDays }: { serverDays: number }) {
  const t = useTranslations("P1Landing.hero");
  const tMandate = useTranslations("Shell.mandate");
  const days = useDaysToMandate(serverDays);

  return (
    <section className="relative isolate -mx-6 overflow-hidden px-6 pt-10 pb-16 md:-mx-10 md:px-10 md:pt-16">
      <GeometricPattern focus="80% 10%" />
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(90%_70%_at_85%_0%,var(--brand-soft),transparent_60%)] rtl:bg-[radial-gradient(90%_70%_at_15%_0%,var(--brand-soft),transparent_60%)]" />
      <div className="relative flex max-w-3xl flex-col gap-5">
        <span className="inline-flex w-fit items-center gap-2 rounded-pill border border-brand/35 bg-brand-soft px-3 py-1.5 text-[13px] text-brand-ink">
          <span className="size-1.5 rounded-pill bg-brand" aria-hidden />
          {t("badge")}
        </span>
        <h1 className="text-4xl leading-tight font-semibold tracking-[-0.5px] md:text-5xl">{t("title")}</h1>
        <p className="max-w-xl text-[15px] leading-7 text-muted-foreground md:text-base">{t("subtitle")}</p>
        <div className="lift mt-2 flex w-fit items-baseline gap-2 rounded-xl border bg-panel px-5 py-4">
          <span className="num text-4xl leading-none font-semibold text-brand-ink">{formatInt(days)}</span>
          <span className="flex flex-col text-xs text-muted-foreground">
            <span>{tMandate("daysLeft")}</span>
            <span>
              {tMandate("label")} · <span className="num">{tMandate("date")}</span>
            </span>
          </span>
        </div>
      </div>
    </section>
  );
}

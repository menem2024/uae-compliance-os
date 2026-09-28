"use client";

import { useTranslations } from "next-intl";
import { useSyncExternalStore } from "react";
import { formatInt } from "@/lib/format";
import { daysToMandate } from "@/lib/mandate";

const noopSubscribe = () => () => {};

/**
 * Days to the mandate, recomputed from the viewer's clock. The server value is
 * only the first paint (pages may be prerendered), so a stale build date never sticks.
 */
export function useDaysToMandate(serverDays: number): number {
  return useSyncExternalStore(noopSubscribe, () => daysToMandate(), () => serverDays);
}

/** Sidebar mandate countdown card (prototype Main board). */
export function MandateCard({ serverDays }: { serverDays: number }) {
  const t = useTranslations("Shell.mandate");
  const days = useDaysToMandate(serverDays);
  return (
    <div className="relative flex flex-col gap-2.5 overflow-hidden rounded-xl border bg-panel p-4">
      <svg
        width="160"
        height="160"
        viewBox="0 0 40 40"
        aria-hidden="true"
        className="pointer-events-none absolute -end-[50px] -top-[50px] opacity-[0.08]"
      >
        <g fill="none" stroke="var(--brand)" strokeWidth="1">
          <rect x="9" y="9" width="22" height="22" />
          <rect x="9" y="9" width="22" height="22" transform="rotate(45 20 20)" />
        </g>
      </svg>
      <span className="text-xs text-muted-foreground">{t("label")}</span>
      <span className="flex items-baseline gap-1.5">
        <span className="num text-[30px] leading-none font-semibold text-brand-ink">{formatInt(days)}</span>
        <span className="text-[13px] text-muted-foreground">{t("daysLeft")}</span>
      </span>
      <span className="text-xs text-muted-foreground">
        {t("audience")} · <span className="num">{t("date")}</span>
      </span>
    </div>
  );
}

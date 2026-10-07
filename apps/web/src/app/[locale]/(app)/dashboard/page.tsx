import { ArrowRight, FlaskConical } from "lucide-react";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { CountdownCard } from "@/components/dashboard/countdown-card";
import { DashboardOverview } from "@/components/dashboard/overview";
import { GeometricPattern } from "@/components/geometric-pattern";
import { Link } from "@/i18n/navigation";
import { dirOf } from "@/i18n/routing";
import { daysToMandate } from "@/lib/mandate";
import { requireSession } from "@/lib/server/session";

export default async function DashboardPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale);
  await requireSession(locale, "/dashboard");

  const t = await getTranslations("Dashboard");

  // Empty state with the geometric star pattern (adr/016). Shown only while the firm has no clients and no invoices; otherwise the live overview replaces it.
  const hero = (
    <section className="lift relative overflow-hidden rounded-xl border bg-panel">
      <GeometricPattern focus={dirOf(locale) === "rtl" ? "15% 15%" : "85% 15%"} />
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(90%_80%_at_85%_0%,var(--brand-soft),transparent_60%)] rtl:bg-[radial-gradient(90%_80%_at_15%_0%,var(--brand-soft),transparent_60%)]" />
      <div className="relative flex h-full flex-col items-start gap-5 p-6 md:p-8">
        <span className="inline-flex items-center gap-2 rounded-pill border border-brand/35 bg-brand-soft px-3 py-1.5 text-[13px] text-brand-ink">
          <span className="size-1.5 rounded-pill bg-brand" aria-hidden />
          {t("emptyBadge")}
        </span>
        <div className="flex max-w-xl flex-col gap-3">
          <h2 className="text-2xl leading-snug font-semibold tracking-[-0.3px] md:text-[28px]">
            {t("emptyTitle")}
          </h2>
          <p className="text-[15px] leading-7 text-muted-foreground">{t("emptyBody")}</p>
        </div>
        <Link
          href="/demo"
          className="group mt-auto inline-flex h-11 items-center gap-2.5 rounded-lg bg-primary px-5 text-sm font-bold text-primary-foreground transition-[filter,transform] hover:brightness-110 active:translate-y-px"
        >
          <FlaskConical className="size-4" strokeWidth={2} aria-hidden />
          {t("emptyCta")}
          <ArrowRight
            className="size-4 transition-transform group-hover:translate-x-0.5 rtl:-scale-x-100 rtl:group-hover:-translate-x-0.5"
            strokeWidth={2}
            aria-hidden
          />
        </Link>
      </div>
    </section>
  );

  return <DashboardOverview hero={hero} countdown={<CountdownCard serverDays={daysToMandate()} />} />;
}

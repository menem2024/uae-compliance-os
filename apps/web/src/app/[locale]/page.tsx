import { ArrowRight, FlaskConical } from "lucide-react";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { Fragment } from "react";
import { CountdownCard } from "@/components/dashboard/countdown-card";
import { GeometricPattern } from "@/components/geometric-pattern";
import { StatusPill, type InvoiceStatus } from "@/components/ui/status-pill";
import { Link } from "@/i18n/navigation";
import { dirOf } from "@/i18n/routing";
import { formatInt } from "@/lib/format";
import { daysToMandate } from "@/lib/mandate";

/** Invoice lifecycle; step 3 branches into validated / has_issues. */
const PIPELINE: InvoiceStatus[][] = [["uploaded"], ["extracted"], ["validated", "has_issues"], ["ready"]];

export default async function HomePage({ params }: PageProps<"/[locale]">) {
  const { locale } = await params;
  setRequestLocale(locale);
  const t = await getTranslations("Dashboard");

  return (
    <>
      <div className="grid gap-4 lg:grid-cols-3">
        {/* Empty state with the geometric star pattern (adr/016). */}
        <section className="relative overflow-hidden rounded-xl border bg-panel lg:col-span-2">
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

        <CountdownCard serverDays={daysToMandate()} />
      </div>

      <section className="rounded-xl border bg-panel">
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
                <span className="flex flex-wrap gap-1.5">
                  {step.map((s) => (
                    <StatusPill key={s} status={s} />
                  ))}
                </span>
              </li>
            </Fragment>
          ))}
        </ol>
      </section>
    </>
  );
}

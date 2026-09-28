import { ArrowRight, FlaskConical } from "lucide-react";
import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { GeometricPattern } from "@/components/geometric-pattern";
import { Link } from "@/i18n/navigation";

export async function generateMetadata({ params }: PageProps<"/[locale]/demo">): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "Demo" });
  return { title: t("title") };
}

/** Placeholder: story 7b renders the auth-gated <DemoForm/> here (prototype Review board styling). */
export default async function DemoPage({ params }: PageProps<"/[locale]/demo">) {
  const { locale } = await params;
  setRequestLocale(locale);
  const t = await getTranslations("DemoPage");

  return (
    <section className="flex max-w-3xl flex-col gap-3.5 rounded-xl border bg-panel p-[18px]">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-[15px] font-semibold">{t("resultTitle")}</h2>
        <span className="rounded-sm bg-panel-2 px-2 py-0.5 font-mono text-[11px] text-muted-foreground" dir="ltr">
          {t("ruleset")}
        </span>
      </div>
      <div className="relative flex flex-col items-center gap-4 overflow-hidden rounded-lg border border-dashed px-6 py-14 text-center">
        <GeometricPattern className="opacity-70" tile={64} focus="50% 50%" />
        <span className="relative flex size-12 items-center justify-center rounded-lg bg-brand-soft text-brand-ink">
          <FlaskConical className="size-6" strokeWidth={1.8} aria-hidden />
        </span>
        <div className="relative flex max-w-md flex-col gap-2">
          <h3 className="text-base font-semibold">{t("placeholderTitle")}</h3>
          <p className="text-sm leading-6 text-muted-foreground">{t("placeholderBody")}</p>
        </div>
        <Link
          href="/"
          className="relative inline-flex h-10 items-center gap-2 rounded-lg border bg-panel px-4 text-[13px] text-foreground transition-colors hover:border-brand/40"
        >
          <ArrowRight className="size-4 -scale-x-100 rtl:scale-x-100" strokeWidth={2} aria-hidden />
          {t("back")}
        </Link>
      </div>
    </section>
  );
}

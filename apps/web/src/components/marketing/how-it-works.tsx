import { useTranslations } from "next-intl";
import { formatInt } from "@/lib/format";

type Step = { title: string; body: string };

/** "How it works": the four-step pipeline a Firm sees, in plain language. */
export function HowItWorks() {
  const t = useTranslations("P1Landing.howItWorks");
  const steps = t.raw("steps") as Step[];

  return (
    <section className="flex flex-col gap-6">
      <h2 className="text-2xl font-semibold tracking-[-0.3px]">{t("title")}</h2>
      <ol className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {steps.map((step, i) => (
          <li key={step.title} className="lift flex flex-col gap-2 rounded-xl border bg-panel p-5">
            <span className="num flex size-7 items-center justify-center rounded-pill border bg-panel-2 text-xs font-semibold text-muted-foreground">
              {formatInt(i + 1)}
            </span>
            <h3 className="text-[15px] font-semibold">{step.title}</h3>
            <p className="text-[13px] leading-6 text-muted-foreground">{step.body}</p>
          </li>
        ))}
      </ol>
    </section>
  );
}

import { BadgeCheck } from "lucide-react";
import { useTranslations } from "next-intl";

/** Trust points: short, scannable reassurances (data residency, RLS, audit trail, no auto-send). */
export function TrustSection() {
  const t = useTranslations("P1Landing.trust");
  const points = t.raw("points") as string[];

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-2xl font-semibold tracking-[-0.3px]">{t("title")}</h2>
      <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {points.map((point) => (
          <li key={point} className="flex items-center gap-2.5 rounded-lg bg-panel-2 px-4 py-3.5 text-[13px]">
            <BadgeCheck className="size-4 shrink-0 text-ok" strokeWidth={2} aria-hidden />
            {point}
          </li>
        ))}
      </ul>
    </section>
  );
}

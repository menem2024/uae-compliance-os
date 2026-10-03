import { Fragment } from "react";
import { useTranslations } from "next-intl";

/** Agent roles drawn left-to-right, in pipeline order (contract agent-runtime.md §3). */
const ROLES = ["intake", "extraction", "verifier", "orchestrator"] as const;

/**
 * The animated agent-orchestra graph: a pulse travels through the pipeline nodes.
 * Pure CSS (`animate-pulse` + a per-node delay) so `motion-reduce:` alone turns it
 * into a static graph — no JS reduced-motion check (spec §5.7).
 */
export function OrchestraGraph() {
  const t = useTranslations("P1Landing.orchestra");

  return (
    <section className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-2xl font-semibold tracking-[-0.3px]">{t("title")}</h2>
        <p className="max-w-xl text-[15px] text-muted-foreground">{t("subtitle")}</p>
      </div>
      <div className="flex items-center gap-0 overflow-x-auto py-4">
        {ROLES.map((role, i) => (
          <Fragment key={role}>
            {i > 0 && <span className="h-px w-8 shrink-0 bg-brand/30 md:w-16" aria-hidden />}
            <div
              className="lift motion-reduce:animate-none flex shrink-0 flex-col items-center gap-2 rounded-xl border bg-panel px-5 py-5 text-center animate-pulse"
              style={{ animationDelay: `${i * 0.35}s`, animationDuration: "2.4s" }}
            >
              <span className="size-2.5 rounded-pill bg-brand" aria-hidden />
              <span className="max-w-[9rem] text-sm font-medium">{t(role)}</span>
            </div>
          </Fragment>
        ))}
      </div>
    </section>
  );
}

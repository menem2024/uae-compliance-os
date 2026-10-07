"use client";

import { useMutation } from "@tanstack/react-query";
import { FlaskConical, Loader2, Send } from "lucide-react";
import { useTranslations } from "next-intl";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { useRouter } from "@/i18n/navigation";
import { DEMO_SAMPLES, type DemoSample } from "@/lib/demo-samples";
import { createInvoice } from "@/lib/p2/api";
import { cn } from "@/lib/utils";
import { useErrorText } from "./use-error-text";

/** "New demo invoice": POSTs one of the canonical samples as it is, then opens its review page. */
export function SamplePicker() {
  const t = useTranslations("P2Invoices");
  const errorText = useErrorText();
  const router = useRouter();
  const [pending, setPending] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: (s: DemoSample) => createInvoice(s.body),
    onMutate: (s) => setPending(s.id),
    onSuccess: ({ id }) => router.push(`/invoices/${encodeURIComponent(id)}`),
    onSettled: () => setPending(null),
  });

  return (
    <section data-testid="sample-picker" className="lift flex flex-col gap-4 rounded-xl border bg-panel p-[18px]">
      <header className="flex flex-col gap-1">
        <h2 className="flex items-center gap-2 text-[15px] font-semibold">
          <FlaskConical className="size-4 text-brand-ink" aria-hidden />
          {t("samples.title")}
        </h2>
        <p className="text-xs text-muted-foreground">{t("samples.hint")}</p>
      </header>

      <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {DEMO_SAMPLES.map((s) => (
          <li
            key={s.id}
            data-testid={`sample-${s.id}`}
            className="flex flex-col gap-3 rounded-lg border bg-panel-2 p-3.5"
          >
            <div className="flex grow flex-col gap-1.5">
              <h3 className="text-sm font-semibold">{t(`${s.i18nKey}.title`)}</h3>
              <p className="text-xs leading-5 text-muted-foreground">{t(`${s.i18nKey}.description`)}</p>
              <span
                className={cn(
                  "mt-1 w-fit rounded-pill px-2 py-0.5 text-[11px] font-medium",
                  s.expected === "validated" ? "bg-ok-soft text-ok" : "bg-warn-soft text-warn",
                )}
              >
                {t(`samples.expected.${s.expected}`)}
              </span>
            </div>
            <Button
              type="button"
              size="sm"
              data-testid={`sample-submit-${s.id}`}
              disabled={create.isPending}
              onClick={() => create.mutate(s)}
            >
              {pending === s.id ? (
                <Loader2 className="animate-spin" aria-hidden />
              ) : (
                <Send className="rtl:-scale-x-100" aria-hidden />
              )}
              {pending === s.id ? t("samples.creating") : t("samples.create")}
            </Button>
          </li>
        ))}
      </ul>

      {create.isError && (
        <p role="alert" data-testid="sample-error" className="text-sm text-bad">
          {t("samples.createError")} {errorText(create.error)}
        </p>
      )}
    </section>
  );
}

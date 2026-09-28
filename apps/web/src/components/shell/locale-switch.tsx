"use client";

import { useLocale, useTranslations } from "next-intl";
import { useTransition } from "react";
import { usePathname, useRouter } from "@/i18n/navigation";
import { routing } from "@/i18n/routing";
import { cn } from "@/lib/utils";
import { control } from "./styles";

export function useSwitchLocale() {
  const locale = useLocale();
  const pathname = usePathname();
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const next = routing.locales.find((l) => l !== locale) ?? routing.defaultLocale;
  return {
    next,
    pending,
    switchLocale: () => startTransition(() => router.replace(pathname, { locale: next })),
  };
}

export function LocaleSwitch() {
  const t = useTranslations("Locale");
  const { next, pending, switchLocale } = useSwitchLocale();
  return (
    <button
      type="button"
      data-testid="locale-switch"
      lang={next}
      onClick={switchLocale}
      disabled={pending}
      className={cn(control, "shrink-0 px-3.5 text-[13px] font-semibold disabled:opacity-60")}
    >
      {t("switch")}
    </button>
  );
}

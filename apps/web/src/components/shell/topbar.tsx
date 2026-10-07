"use client";

import { LogOut, Search, User } from "lucide-react";
import { useLocale, useTranslations } from "next-intl";
import { signOutAction } from "@/app/actions/auth";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { usePathname } from "@/i18n/navigation";
import { cn } from "@/lib/utils";
import { LocaleSwitch } from "./locale-switch";
import { LogoMark } from "./logo";
import { activeNavItem } from "./nav-items";
import { control, iconButton } from "./styles";
import { ThemeToggle } from "./theme-toggle";
import type { ShellUser } from "./types";

/** Title and subtitle for the current route. */
function usePageHeading() {
  const pathname = usePathname();
  const item = activeNavItem(pathname);
  const t = useTranslations();
  return {
    title: t(item?.titleKey ?? item?.labelKey ?? "Dashboard.title"),
    subtitle: item?.subtitleKey ? t(item.subtitleKey) : "",
  };
}

export function Topbar({ user, onOpenPalette }: { user?: ShellUser; onOpenPalette: () => void }) {
  const t = useTranslations("Shell");
  const tHome = useTranslations("Home");
  const locale = useLocale();
  const { title, subtitle } = usePageHeading();
  const initial = user ? Array.from(user.name.trim())[0]?.toUpperCase() : undefined;

  return (
    <header className="sticky top-0 z-30 flex h-[72px] shrink-0 items-center gap-3 border-b bg-background/80 px-4 backdrop-blur-md md:gap-4 md:px-8">
      <LogoMark size={30} className="lg:hidden" />
      <div className="flex min-w-0 grow flex-col gap-0.5">
        <h1 className="truncate text-lg font-semibold md:text-xl">{title}</h1>
        <p className="hidden truncate text-xs text-muted-foreground sm:block">{subtitle}</p>
      </div>

      <button
        type="button"
        data-testid="cmdk-open"
        onClick={onOpenPalette}
        aria-label={t("search")}
        aria-keyshortcuts="Control+K Meta+K"
        className={cn(
          control,
          "inline-flex w-[42px] shrink-0 items-center justify-center gap-2.5 text-[13px] text-muted-foreground",
          "xl:w-[340px] xl:justify-start xl:px-3.5",
        )}
      >
        <Search className="size-4 shrink-0" aria-hidden />
        <span className="hidden grow truncate text-start xl:inline">{t("search")}</span>
        <kbd className="num hidden shrink-0 rounded-sm whitespace-nowrap border px-[7px] py-[3px] text-[11px] font-normal text-muted-foreground xl:inline">
          Ctrl K
        </kbd>
      </button>

      <ThemeToggle />
      <LocaleSwitch />

      <span
        role="img"
        aria-label={user?.name ?? t("account")}
        className="hidden size-[42px] shrink-0 items-center justify-center rounded-pill bg-linear-135 from-brand to-brand-deep text-sm font-bold text-brand-foreground sm:inline-flex"
      >
        {initial ?? <User className="size-[18px]" strokeWidth={2} aria-hidden />}
      </span>

      {user && (
        <form action={signOutAction} className="contents">
          <input type="hidden" name="locale" value={locale} />
          <Tooltip>
            <TooltipTrigger asChild>
              <button type="submit" data-testid="sign-out" aria-label={tHome("signOut")} className={iconButton}>
                <LogOut className="size-[18px] rtl:-scale-x-100" strokeWidth={1.8} aria-hidden />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom">{tHome("signOut")}</TooltipContent>
          </Tooltip>
        </form>
      )}
    </header>
  );
}

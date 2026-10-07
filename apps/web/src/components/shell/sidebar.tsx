"use client";

import { ChevronsUpDown } from "lucide-react";
import { useTranslations } from "next-intl";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Link, usePathname } from "@/i18n/navigation";
import { formatInt } from "@/lib/format";
import { cn } from "@/lib/utils";
import { Logo } from "./logo";
import { MandateCard } from "./mandate-card";
import { NAV, isActive, type NavItem } from "./nav";
import type { ShellFirm } from "./types";

const itemBase =
  "flex items-center gap-3 rounded-md px-3 py-2.5 text-sm transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring";

function NavLink({ item, label, active }: { item: NavItem & { href: string }; label: string; active: boolean }) {
  const Icon = item.icon;
  return (
    <Link
      href={item.href}
      aria-current={active ? "page" : undefined}
      className={cn(
        itemBase,
        active
          ? "bg-panel font-semibold text-foreground shadow-[0_1px_0_var(--border)]"
          : "text-muted-foreground hover:bg-panel/60 hover:text-foreground",
      )}
    >
      <Icon className="size-[18px] shrink-0" strokeWidth={1.8} aria-hidden />
      <span className="grow">{label}</span>
      {item.live && item.href !== null && <span className="size-1.5 rounded-pill bg-ok animate-pulse" aria-hidden />}
      {item.badge !== undefined && (
        <span className="num rounded-pill bg-brand-soft px-2 py-0.5 text-[11px] font-semibold text-brand-ink">
          {formatInt(item.badge)}
        </span>
      )}
    </Link>
  );
}

function SoonItem({ item, label, soon }: { item: NavItem; label: string; soon: string }) {
  const Icon = item.icon;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          role="link"
          aria-disabled="true"
          tabIndex={0}
          className={cn(itemBase, "cursor-not-allowed text-muted-foreground/60")}
        >
          <Icon className="size-[18px] shrink-0" strokeWidth={1.8} aria-hidden />
          <span className="grow">{label}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent side="bottom">{soon}</TooltipContent>
    </Tooltip>
  );
}

export function Sidebar({ firm, mandateDays }: { firm: ShellFirm; mandateDays: number }) {
  const t = useTranslations("Shell");
  const tHome = useTranslations("Home");
  const tAll = useTranslations();
  const pathname = usePathname();
  const initial = Array.from(firm.name.trim())[0]?.toUpperCase() ?? "·";

  return (
    <aside className="sticky top-0 hidden h-dvh w-[264px] shrink-0 flex-col gap-5 border-e bg-sidebar px-4 py-5 lg:flex">
      <Logo title={tHome("title")} latin={t("brandLatin")} />

      {/* Firm switcher (single Firm in Phase 0). */}
      {/* No aria-label: the accessible name is the visible Firm name plus an sr-only action
          suffix, so it always contains the visible label (WCAG 2.5.3 Label in Name). */}
      <button
        type="button"
        data-testid="firm-switcher"
        className="lift flex items-center gap-2.5 rounded-lg border bg-panel px-3 py-2.5 text-start text-foreground hover:border-brand/40"
      >
        <span aria-hidden className="flex size-[30px] shrink-0 items-center justify-center rounded-sm bg-brand-soft text-[13px] font-bold text-brand-ink">
          {initial}
        </span>
        <span className="flex min-w-0 grow flex-col gap-px">
          <span className="truncate text-[13px] font-semibold">{firm.name}</span>
          <span className="truncate text-[11px] text-muted-foreground">{t("firmWorkspace")}</span>
        </span>
        <span className="sr-only">{` — ${t("switchFirmSuffix")}`}</span>
        <ChevronsUpDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      </button>

      <nav aria-label={t("mainNav")} className="flex flex-col gap-1">
        {NAV.map((item) =>
          item.href === null ? (
            <SoonItem key={item.key} item={item} label={tAll(item.labelKey)} soon={t("soon")} />
          ) : (
            <NavLink
              key={item.key}
              item={{ ...item, href: item.href }}
              label={tAll(item.labelKey)}
              active={isActive(item.href, pathname)}
            />
          ),
        )}
      </nav>

      <div className="mt-auto">
        <MandateCard serverDays={mandateDays} />
      </div>
    </aside>
  );
}

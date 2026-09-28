"use client";

import { Moon, Sun } from "lucide-react";
import { useTranslations } from "next-intl";
import { useTheme } from "next-themes";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { iconButton } from "./styles";

/** Sun in dark mode, moon in light mode; icon choice is pure CSS so SSR never mismatches. */
export function ThemeToggle() {
  const t = useTranslations("Shell.theme");
  const { resolvedTheme, setTheme } = useTheme();
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          data-testid="theme-toggle"
          aria-label={t("toggle")}
          onClick={() => setTheme(resolvedTheme === "light" ? "dark" : "light")}
          className={iconButton}
        >
          <Sun className="hidden size-[18px] dark:block" strokeWidth={1.8} aria-hidden />
          <Moon className="size-[18px] dark:hidden" strokeWidth={1.8} aria-hidden />
        </button>
      </TooltipTrigger>
      <TooltipContent side="bottom">
        <span className="dark:hidden">{t("dark")}</span>
        <span className="hidden dark:inline">{t("light")}</span>
      </TooltipContent>
    </Tooltip>
  );
}

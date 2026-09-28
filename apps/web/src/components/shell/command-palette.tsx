"use client";

import { Command as CommandPrimitive } from "cmdk";
import { Languages, Search, SunMoon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useTheme } from "next-themes";
import { Dialog as DialogPrimitive } from "radix-ui";
import { useEffect, type ReactNode } from "react";
import { CommandEmpty, CommandGroup, CommandItem, CommandList } from "@/components/ui/command";
import { useRouter } from "@/i18n/navigation";
import { useSwitchLocale } from "./locale-switch";
import { NAV } from "./nav";

/** Binds mod+K (Ctrl+K / ⌘K) to toggle the palette. */
export function useCommandPaletteHotkey(toggle: () => void) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "k" && (e.metaKey || e.ctrlKey) && !e.altKey) {
        e.preventDefault();
        toggle();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [toggle]);
}

const groupClass =
  "p-0 **:[[cmdk-group-heading]]:px-2.5 **:[[cmdk-group-heading]]:pt-2.5 **:[[cmdk-group-heading]]:pb-1 **:[[cmdk-group-heading]]:text-[11px] **:[[cmdk-group-heading]]:tracking-[0.5px]";

const itemClass =
  "gap-3 rounded-md! px-3 py-2.5 text-sm text-foreground data-selected:bg-panel-2 [&_svg]:text-muted-foreground";

function Item({ icon, children, onSelect }: { icon: ReactNode; children: ReactNode; onSelect: () => void }) {
  return (
    <CommandItem onSelect={onSelect} className={itemClass}>
      {icon}
      <span className="grow">{children}</span>
    </CommandItem>
  );
}

/** Ctrl+K command palette (prototype Main board), built on cmdk via the shadcn command primitives. */
export function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const t = useTranslations("Palette");
  const tNav = useTranslations("Shell.nav");
  const router = useRouter();
  const { resolvedTheme, setTheme } = useTheme();
  const { switchLocale } = useSwitchLocale();

  const run = (fn: () => void) => () => {
    onOpenChange(false);
    fn();
  };

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-scrim backdrop-blur-[6px] data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <div className="pointer-events-none fixed inset-0 z-50 flex justify-center px-4 pt-[12vh]">
          <DialogPrimitive.Content
            aria-describedby={undefined}
            className="pointer-events-auto h-fit w-full max-w-[620px] overflow-hidden rounded-2xl border bg-panel text-foreground shadow-[0_30px_80px_var(--shadow-color)] outline-none data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-[0.98] data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-[0.98]"
          >
            <DialogPrimitive.Title className="sr-only">{t("label")}</DialogPrimitive.Title>
            <CommandPrimitive label={t("label")} className="flex flex-col">
              <div className="flex items-center gap-3 border-b px-[18px] py-4">
                <Search className="size-[18px] shrink-0 text-muted-foreground" aria-hidden />
                <CommandPrimitive.Input
                  autoFocus
                  placeholder={t("placeholder")}
                  className="grow bg-transparent text-base text-foreground outline-none placeholder:text-muted-foreground"
                />
                <DialogPrimitive.Close className="num rounded-sm border px-2 py-0.5 text-[11px] text-muted-foreground hover:text-foreground">
                  {t("esc")}
                </DialogPrimitive.Close>
              </div>
              <CommandList className="max-h-[360px] p-2.5">
                <CommandEmpty className="py-8 text-center text-sm text-muted-foreground">{t("empty")}</CommandEmpty>
                <CommandGroup heading={t("pages")} className={groupClass}>
                  {NAV.filter((n) => n.href !== null).map((n) => {
                    const Icon = n.icon;
                    return (
                      <Item
                        key={n.key}
                        icon={<Icon className="size-4" strokeWidth={1.8} aria-hidden />}
                        onSelect={run(() => router.push(n.href!))}
                      >
                        {tNav(n.key)}
                      </Item>
                    );
                  })}
                </CommandGroup>
                <CommandGroup heading={t("actions")} className={groupClass}>
                  <Item
                    icon={<SunMoon className="size-4" strokeWidth={1.8} aria-hidden />}
                    onSelect={run(() => setTheme(resolvedTheme === "light" ? "dark" : "light"))}
                  >
                    {t("toggleTheme")}
                  </Item>
                  <Item icon={<Languages className="size-4" strokeWidth={1.8} aria-hidden />} onSelect={run(switchLocale)}>
                    {t("switchLocale")}
                  </Item>
                </CommandGroup>
              </CommandList>
            </CommandPrimitive>
          </DialogPrimitive.Content>
        </div>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

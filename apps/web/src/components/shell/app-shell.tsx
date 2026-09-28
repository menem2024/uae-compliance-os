"use client";

import { useCallback, useState, type ReactNode } from "react";
import { CommandPalette, useCommandPaletteHotkey } from "./command-palette";
import { Sidebar } from "./sidebar";
import { Topbar } from "./topbar";
import type { ShellFirm, ShellUser } from "./types";

export type AppShellProps = {
  /** The current Firm. 7a passes DEMO_FIRM; 7b passes the mapped `/v1/me` result. */
  firm: ShellFirm;
  /** Signed-in user (7b). */
  user?: ShellUser;
  /** Days to the mandate at render time (first paint; the client recomputes). */
  mandateDays: number;
  children: ReactNode;
};

/**
 * Dashboard shell (prototype Main board): 264px sidebar on the inline-start side
 * (right in RTL), 72px top bar, Ctrl+K palette. The Firm accent is applied on
 * <body> by the locale layout (brandStyle) so portals inherit it as well.
 */
export function AppShell({ firm, user, mandateDays, children }: AppShellProps) {
  const [paletteOpen, setPaletteOpen] = useState(false);
  const togglePalette = useCallback(() => setPaletteOpen((o) => !o), []);
  useCommandPaletteHotkey(togglePalette);

  return (
    <div className="flex min-h-dvh bg-background text-foreground">
      <Sidebar firm={firm} mandateDays={mandateDays} />
      <div className="flex min-w-0 grow flex-col">
        <Topbar user={user} onOpenPalette={() => setPaletteOpen(true)} />
        <main id="main" className="flex grow flex-col gap-6 px-4 py-6 md:px-8 md:py-7">
          {children}
        </main>
      </div>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </div>
  );
}

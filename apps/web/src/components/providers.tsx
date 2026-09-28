"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MotionConfig } from "motion/react";
import type { Session } from "next-auth";
import { SessionProvider } from "next-auth/react";
import { ThemeProvider } from "next-themes";
import { useState, type CSSProperties, type ReactNode } from "react";
import { Toaster } from "sonner";
import { TooltipProvider } from "@/components/ui/tooltip";

/**
 * Client-side providers. The Auth.js session handed to <SessionProvider> carries only the
 * user's name/email (see auth.ts): the Zitadel access token never reaches browser JS.
 */
export function Providers({
  children,
  dir,
  session,
}: {
  children: ReactNode;
  dir: "rtl" | "ltr";
  session: Session | null;
}) {
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { staleTime: 30_000, retry: 1 } } }),
  );

  return (
    <SessionProvider session={session}>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider attribute="class" defaultTheme="dark" enableSystem disableTransitionOnChange>
        {/* reducedMotion="user": Motion skips transform animations under prefers-reduced-motion. */}
        <MotionConfig reducedMotion="user">
          <TooltipProvider delayDuration={200}>
            {children}
            <Toaster
              position="top-center"
              dir={dir}
              className="font-sans"
              style={
                {
                  "--normal-bg": "var(--panel)",
                  "--normal-text": "var(--text)",
                  "--normal-border": "var(--border)",
                  "--border-radius": "var(--radius)",
                } as CSSProperties
              }
            />
          </TooltipProvider>
        </MotionConfig>
      </ThemeProvider>
    </QueryClientProvider>
    </SessionProvider>
  );
}

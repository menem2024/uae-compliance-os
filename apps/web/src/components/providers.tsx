"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MotionConfig } from "motion/react";
import { ThemeProvider } from "next-themes";
import { useState, type CSSProperties, type ReactNode } from "react";
import { Toaster } from "sonner";
import { TooltipProvider } from "@/components/ui/tooltip";

/**
 * Client-side providers. Story 7b wraps `children` in the Auth.js
 * <SessionProvider> here (see the SESSION PROVIDER SLOT below).
 */
export function Providers({ children, dir }: { children: ReactNode; dir: "rtl" | "ltr" }) {
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { staleTime: 30_000, retry: 1 } } }),
  );

  return (
    // SESSION PROVIDER SLOT (7b): <SessionProvider> goes outermost, around QueryClientProvider.
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
  );
}

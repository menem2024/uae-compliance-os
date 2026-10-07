"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { useLocale, useTranslations } from "next-intl";
import { useMemo, useRef } from "react";
import { translateFeed } from "@/lib/feed-message";
import type { FeedLine } from "@/lib/agent-feed";
import { cn } from "@/lib/utils";

const ROW_HEIGHT = 44;

const LINE_TONE: Record<string, string> = {
  failed: "text-bad",
  retrying: "text-warn",
  succeeded: "text-foreground",
};

/**
 * The live feed, newest first, virtualized: only the visible rows are in the DOM however many lines the
 * (bounded) reducer holds. Pure view of `lines`; it keeps no state of its own.
 */
export function ActivityFeed({ lines }: { lines: readonly FeedLine[] }) {
  const t = useTranslations();
  const locale = useLocale();
  const time = useMemo(
    () => new Intl.DateTimeFormat(`${locale}-u-nu-latn`, { timeStyle: "medium" }),
    [locale],
  );
  const parentRef = useRef<HTMLDivElement>(null);
  const count = lines.length;
  // React Compiler skips memoizing this component (TanStack Virtual's functions are unmemoizable); it is cheap.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
  });

  return (
    <section aria-label={t("P1Agents.feed.title")} className="flex min-h-0 flex-col gap-3">
      <div className="flex items-baseline justify-between gap-2">
        <h2 className="text-base font-semibold">{t("P1Agents.feed.title")}</h2>
        <span className="text-xs tabular-nums text-muted-foreground">{t("P1Agents.feed.count", { count })}</span>
      </div>
      <div
        ref={parentRef}
        data-testid="agents-feed"
        role="log"
        aria-live="polite"
        className="h-96 overflow-y-auto rounded-xl border bg-panel"
      >
        {count === 0 ? (
          <p className="p-6 text-center text-sm text-muted-foreground">{t("P1Agents.feed.empty")}</p>
        ) : (
          <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
            {virtualizer.getVirtualItems().map((row) => {
              const line = lines[count - 1 - row.index]; // newest first
              if (!line) return null;
              const text = translateFeed(t, line.key, line.args);
              const at = line.at ? new Date(line.at) : null;
              return (
                <div
                  key={line.id}
                  data-testid="feed-line"
                  data-status={line.status}
                  data-index={row.index}
                  className="absolute inset-x-0 flex items-center gap-3 border-b px-4 text-sm"
                  style={{ height: row.size, transform: `translateY(${row.start}px)` }}
                >
                  <span className="w-20 shrink-0 text-xs tabular-nums text-muted-foreground" dir="ltr">
                    {at && !Number.isNaN(at.getTime()) ? time.format(at) : ""}
                  </span>
                  <span className={cn("min-w-0 flex-1 truncate", LINE_TONE[line.status])} title={text} dir="auto">
                    {text}
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </section>
  );
}

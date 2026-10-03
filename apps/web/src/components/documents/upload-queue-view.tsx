"use client";

import { AlertCircle, CheckCircle2, CircleDashed, Copy, Loader2 } from "lucide-react";
import { useTranslations } from "next-intl";
import { Button } from "@/components/ui/button";
import { formatBytes, isTerminal, type UploadItem, type UploadState } from "@/lib/uploads";
import { cn } from "@/lib/utils";

const TONE: Record<UploadState, string> = {
  hashing: "text-muted-foreground",
  requesting: "text-muted-foreground",
  uploading: "text-info",
  completing: "text-info",
  processing: "text-info",
  extracted: "text-ok",
  needs_review: "text-warn",
  not_invoice: "text-muted-foreground",
  failed: "text-bad",
  rejected: "text-bad",
  duplicate: "text-muted-foreground",
};

function StateIcon({ state }: { state: UploadState }) {
  if (state === "extracted") return <CheckCircle2 className="size-4" />;
  if (state === "failed" || state === "rejected") return <AlertCircle className="size-4" />;
  if (state === "duplicate") return <Copy className="size-4" />;
  if (isTerminal(state)) return <CircleDashed className="size-4" />;
  return <Loader2 className="size-4 animate-spin" />;
}

/** The files of this session's uploads with their live state. Nothing is stored here. */
export function UploadQueueView({ items, onClear }: { items: readonly UploadItem[]; onClear: () => void }) {
  const t = useTranslations("P1Documents");
  if (items.length === 0) return null;
  const errorText = (code: string) => (t.has(`errors.${code}`) ? t(`errors.${code}`) : t("errors.generic"));
  return (
    <section aria-label={t("queue.title")} className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium">{t("queue.title")}</h2>
        {items.some((i) => isTerminal(i.state)) && (
          <Button variant="ghost" size="sm" onClick={onClear}>{t("queue.clear")}</Button>
        )}
      </div>
      <ul className="divide-y rounded-xl border bg-panel">
        {items.map((i) => (
          <li key={i.id} data-testid="upload-row" data-state={i.state} className="flex flex-col gap-1.5 px-4 py-2.5">
            <div className="flex items-center justify-between gap-3">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium" dir="auto">{i.file.name}</p>
                <p className="text-xs text-muted-foreground tabular-nums">{formatBytes(i.file.size)}</p>
              </div>
              <span className={cn("flex shrink-0 items-center gap-1.5 text-xs font-medium", TONE[i.state])}>
                <StateIcon state={i.state} />
                {t(`state.${i.state}`)}
              </span>
            </div>
            {i.state === "uploading" && (
              <div
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={Math.round(i.progress * 100)}
                className="h-1 overflow-hidden rounded-pill bg-muted"
              >
                <div className="h-full bg-info transition-[width]" style={{ width: `${Math.round(i.progress * 100)}%` }} />
              </div>
            )}
            {i.error && <p className="text-xs text-bad">{errorText(i.error)}</p>}
          </li>
        ))}
      </ul>
    </section>
  );
}

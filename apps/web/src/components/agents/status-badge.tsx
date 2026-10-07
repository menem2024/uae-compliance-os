import { useTranslations } from "next-intl";
import { cn } from "@/lib/utils";

const TONE: Record<string, string> = {
  running: "bg-info-soft text-info",
  started: "bg-info-soft text-info",
  retrying: "bg-warn-soft text-warn",
  succeeded: "bg-ok-soft text-ok",
  failed: "bg-bad-soft text-bad",
  budget_exceeded: "bg-bad-soft text-bad",
  cancelled: "bg-muted text-muted-foreground",
  abandoned: "bg-muted text-muted-foreground",
  skipped: "bg-muted text-muted-foreground",
  pending: "bg-muted text-muted-foreground",
};

/** Run or step status; a status this build does not know renders neutral, with the raw word. */
export function StatusBadge({ status, className }: { status: string; className?: string }) {
  const t = useTranslations("P1Agents.status");
  return (
    <span
      data-status={status}
      className={cn(
        "inline-flex items-center rounded-pill px-2.5 py-1 text-xs font-semibold whitespace-nowrap",
        TONE[status] ?? "bg-muted text-muted-foreground",
        className,
      )}
    >
      {t.has(status) ? t(status) : status}
    </span>
  );
}

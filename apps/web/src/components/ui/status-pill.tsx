import { useTranslations } from "next-intl";
import { cn } from "@/lib/utils";

export type InvoiceStatus = "uploaded" | "extracted" | "validated" | "has_issues" | "ready";

/** Token colours per status: ok = validated/ready, warn = has_issues, info = in progress. */
const TONE: Record<InvoiceStatus, string> = {
  uploaded: "bg-info-soft text-info",
  extracted: "bg-info-soft text-info",
  validated: "bg-ok-soft text-ok",
  ready: "bg-ok-soft text-ok",
  has_issues: "bg-warn-soft text-warn",
};

export function isInvoiceStatus(s: unknown): s is InvoiceStatus {
  return typeof s === "string" && Object.hasOwn(TONE, s);
}

export function StatusPill({
  status,
  className,
  ...props
}: { status: InvoiceStatus } & React.ComponentProps<"span">) {
  const t = useTranslations("Status");
  return (
    <span
      data-status={status}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-pill px-2.5 py-1 text-xs font-semibold whitespace-nowrap",
        TONE[status],
        className,
      )}
      {...props}
    >
      <span className="size-1.5 rounded-pill bg-current" aria-hidden />
      {t(status)}
    </span>
  );
}

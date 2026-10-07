"use client";

import {
  createColumnHelper, createSortedRowModel, rowSortingFeature, tableFeatures, useTable,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ArrowUpDown, FileText, Loader2 } from "lucide-react";
import { useLocale, useTranslations } from "next-intl";
import { useMemo } from "react";
import { Button } from "@/components/ui/button";
import { formatInt } from "@/lib/format";
import { formatBytes, isDocumentInFlight, type DocumentRow } from "@/lib/uploads";
import { cn } from "@/lib/utils";

// Sorting runs on the loaded rows only; paging stays cursor-based ("load more"), never the table's own pager.
const features = tableFeatures({ rowSortingFeature, sortedRowModel: createSortedRowModel() });
const col = createColumnHelper<typeof features, DocumentRow>();

const STATUS_TONE: Record<string, string> = {
  pending_upload: "bg-muted text-muted-foreground",
  uploaded: "bg-info-soft text-info",
  processing: "bg-info-soft text-info",
  extracted: "bg-ok-soft text-ok",
  needs_review: "bg-warn-soft text-warn",
  not_invoice: "bg-muted text-muted-foreground",
  failed: "bg-bad-soft text-bad",
  rejected: "bg-bad-soft text-bad",
};

type Props = {
  rows: DocumentRow[];
  loading: boolean;
  error: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  onLoadMore: () => void;
};

export function DocumentsTable({ rows, loading, error, hasMore, loadingMore, onLoadMore }: Props) {
  const t = useTranslations("P1Documents");
  const locale = useLocale();
  const when = useMemo(
    () => new Intl.DateTimeFormat(`${locale}-u-nu-latn`, { dateStyle: "medium", timeStyle: "short" }),
    [locale],
  );

  const columns = useMemo(
    () =>
      col.columns([
        col.accessor("filename", {
          header: t("columns.file"),
          cell: (c) => <span className="block max-w-80 truncate font-medium" dir="auto">{c.getValue()}</span>,
        }),
        col.accessor("status", {
          header: t("columns.status"),
          cell: (c) => {
            const s = c.getValue();
            return (
              <span
                data-status={s}
                className={cn(
                  "inline-flex items-center gap-1.5 rounded-pill px-2.5 py-1 text-xs font-semibold whitespace-nowrap",
                  STATUS_TONE[s] ?? "bg-muted text-muted-foreground",
                )}
              >
                {isDocumentInFlight(s) && s !== "pending_upload" && <Loader2 className="size-3 animate-spin" aria-hidden />}
                {t.has(`status.${s}`) ? t(`status.${s}`) : s}
              </span>
            );
          },
        }),
        col.accessor("invoice_count", {
          header: t("columns.invoices"),
          cell: (c) => <span className="tabular-nums">{formatInt(c.getValue())}</span>,
        }),
        col.accessor("size_bytes", {
          header: t("columns.size"),
          cell: (c) => <span className="tabular-nums" dir="ltr">{formatBytes(c.getValue())}</span>,
        }),
        col.accessor("created_at", {
          header: t("columns.uploaded"),
          sortDescFirst: true,
          cell: (c) => <span className="whitespace-nowrap tabular-nums">{when.format(new Date(c.getValue()))}</span>,
        }),
      ]),
    [t, when],
  );

  const table = useTable({ features, columns, data: rows });

  return (
    <div className="flex flex-col gap-3">
      <div className="overflow-x-auto rounded-xl border bg-panel">
        <table className="w-full text-sm">
          <thead className="text-xs text-muted-foreground">
            {table.getHeaderGroups().map((group) => (
              <tr key={group.id} className="border-b">
                {group.headers.map((header) => {
                  const sorted = header.column.getIsSorted();
                  const Icon = sorted === "asc" ? ArrowUp : sorted === "desc" ? ArrowDown : ArrowUpDown;
                  return (
                    <th
                      key={header.id}
                      scope="col"
                      aria-sort={sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : "none"}
                      className="px-4 py-2 text-start font-medium"
                    >
                      {header.isPlaceholder ? null : (
                        <button
                          type="button"
                          onClick={header.column.getToggleSortingHandler()}
                          className="inline-flex items-center gap-1 hover:text-foreground"
                        >
                          <table.FlexRender header={header} />
                          <Icon className={cn("size-3", !sorted && "opacity-40")} aria-hidden />
                        </button>
                      )}
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {table.getRowModel().rows.map((row) => (
              <tr key={row.id} data-testid="doc-row" data-status={row.original.status} className="border-b last:border-0">
                {row.getAllCells().map((cell) => (
                  <td key={cell.id} className="px-4 py-2.5">
                    <table.FlexRender cell={cell} />
                    {cell.column.id === "status" && row.original.status_reason && (
                      <div className="mt-0.5 font-mono text-xs text-muted-foreground" dir="ltr">
                        {row.original.status_reason}
                      </div>
                    )}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
        {loading && (
          <div className="flex justify-center p-6 text-muted-foreground"><Loader2 className="size-5 animate-spin" /></div>
        )}
        {error && <p className="p-6 text-center text-sm text-bad">{t("errors.load")}</p>}
        {!loading && !error && rows.length === 0 && (
          <div className="flex flex-col items-center gap-2 p-10 text-center text-muted-foreground">
            <FileText className="size-6" />
            <p>{t("empty")}</p>
          </div>
        )}
      </div>
      {hasMore && (
        <Button variant="outline" className="self-center" disabled={loadingMore} onClick={onLoadMore}>
          {t("loadMore")}
        </Button>
      )}
    </div>
  );
}

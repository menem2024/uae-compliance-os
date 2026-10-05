"use client";

import {
  createColumnHelper, createSortedRowModel, rowSortingFeature, tableFeatures, useTable,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ArrowUpDown, FileText, Loader2 } from "lucide-react";
import { useLocale, useTranslations } from "next-intl";
import { useMemo } from "react";
import { Button } from "@/components/ui/button";
import { StatusPill, isInvoiceStatus } from "@/components/ui/status-pill";
import { Link } from "@/i18n/navigation";
import type { InvoiceListItem } from "@/lib/p2/types";
import { cn } from "@/lib/utils";

// Sorting runs on the loaded rows only; paging stays cursor-based ("load more"), never the table's own pager.
const features = tableFeatures({ rowSortingFeature, sortedRowModel: createSortedRowModel() });
const col = createColumnHelper<typeof features, InvoiceListItem>();

type Props = {
  rows: InvoiceListItem[];
  loading: boolean;
  error: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  onLoadMore: () => void;
  onRetry: () => void;
};

export function InvoicesTable({ rows, loading, error, hasMore, loadingMore, onLoadMore, onRetry }: Props) {
  const t = useTranslations("P2Invoices.list");
  const locale = useLocale();
  const when = useMemo(
    () => new Intl.DateTimeFormat(`${locale}-u-nu-latn`, { dateStyle: "medium", timeStyle: "short" }),
    [locale],
  );

  const columns = useMemo(
    () =>
      col.columns([
        col.accessor("invoice_number", {
          header: t("columns.invoice"),
          cell: (c) => (
            <Link
              href={`/invoices/${encodeURIComponent(c.row.original.id)}`}
              data-testid="invoice-link"
              className="font-medium text-brand-ink underline-offset-4 hover:underline"
              dir="auto"
            >
              {c.getValue() || t("unnamed")}
            </Link>
          ),
        }),
        col.accessor("seller_name", {
          header: t("columns.seller"),
          cell: (c) => <span className="block max-w-56 truncate" dir="auto">{c.getValue() || "—"}</span>,
        }),
        col.accessor("buyer_name", {
          header: t("columns.buyer"),
          cell: (c) => <span className="block max-w-56 truncate" dir="auto">{c.getValue() || "—"}</span>,
        }),
        col.accessor("total_amount", {
          header: t("columns.total"),
          cell: (c) => (
            <span className="whitespace-nowrap tabular-nums" dir="ltr">
              {c.getValue()} {c.row.original.currency}
            </span>
          ),
        }),
        col.accessor("status", {
          header: t("columns.status"),
          cell: (c) => {
            const s = c.getValue();
            return isInvoiceStatus(s) ? <StatusPill status={s} /> : <span className="text-xs">{s}</span>;
          },
        }),
        col.accessor("error_count", {
          header: t("columns.issues"),
          cell: (c) => {
            const { error_count: errors, warning_count: warnings } = c.row.original;
            return errors + warnings === 0 ? (
              <span className="text-muted-foreground">{t("noIssues")}</span>
            ) : (
              <span className="text-xs whitespace-nowrap tabular-nums">{t("issuesCount", { errors, warnings })}</span>
            );
          },
        }),
        col.accessor("created_at", {
          header: t("columns.created"),
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
        <table data-testid="invoices-table" className="w-full text-sm">
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
              <tr key={row.id} data-testid="invoice-row" data-status={row.original.status} className="border-b last:border-0">
                {row.getAllCells().map((cell) => (
                  <td key={cell.id} className="px-4 py-2.5">
                    <table.FlexRender cell={cell} />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
        {loading && (
          <div className="flex justify-center p-6 text-muted-foreground"><Loader2 className="size-5 animate-spin" /></div>
        )}
        {error && (
          <div role="alert" className="flex flex-col items-center gap-2 p-6 text-center text-sm text-bad">
            <p>{t("loadError")}</p>
            <Button variant="outline" size="sm" onClick={onRetry}>{t("retry")}</Button>
          </div>
        )}
        {!loading && !error && rows.length === 0 && (
          <div data-testid="invoices-empty" className="flex flex-col items-center gap-2 p-10 text-center text-muted-foreground">
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

"use client";

import {
  createColumnHelper, createSortedRowModel, rowSortingFeature, tableFeatures, useTable,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react";
import { useLocale, useTranslations } from "next-intl";
import { useMemo } from "react";
import { formatCostMicroUsd } from "@/lib/agent-roster";
import type { RunRow } from "@/lib/agent-feed";
import { formatInt } from "@/lib/format";
import { cn } from "@/lib/utils";
import { StatusBadge } from "./status-badge";

// Sorting runs on the (bounded) rows the feed reducer holds; there is no pager.
const features = tableFeatures({ rowSortingFeature, sortedRowModel: createSortedRowModel() });
const col = createColumnHelper<typeof features, RunRow>();

/** Go's zero time marshals as 0001-01-01; a run known only from its finish event has no start time. */
const startedMs = (r: RunRow) => (r.started_at && !r.started_at.startsWith("0001-") ? Date.parse(r.started_at) : 0);

type Props = { rows: readonly RunRow[]; selectedId: string | null; onOpen: (id: string) => void };

export function RunList({ rows, selectedId, onOpen }: Props) {
  const t = useTranslations("P1Agents.runs");
  const locale = useLocale();
  const when = useMemo(
    () => new Intl.DateTimeFormat(`${locale}-u-nu-latn`, { dateStyle: "medium", timeStyle: "short" }),
    [locale],
  );

  const columns = useMemo(
    () =>
      col.columns([
        col.accessor("workflow", {
          header: t("columns.workflow"),
          cell: (c) => (
            <button
              type="button"
              onClick={() => onOpen(c.row.original.id)}
              aria-label={t("open")}
              className="max-w-56 truncate text-start font-medium underline-offset-4 hover:underline"
              dir="ltr"
            >
              {c.getValue() || c.row.original.id.slice(0, 8)}
            </button>
          ),
        }),
        col.accessor((r) => `${r.subject_type}:${r.subject_id}`, {
          id: "subject",
          header: t("columns.subject"),
          cell: (c) => {
            const r = c.row.original;
            return (
              <span className="text-xs text-muted-foreground">
                {t.has(`subject.${r.subject_type}`) ? t(`subject.${r.subject_type}`) : r.subject_type}{" "}
                <span className="font-mono" dir="ltr">{r.subject_id.slice(0, 8)}</span>
              </span>
            );
          },
        }),
        col.accessor("status", { header: t("columns.status"), cell: (c) => <StatusBadge status={c.getValue()} /> }),
        col.accessor((r) => r.totals.steps, {
          id: "steps",
          header: t("columns.steps"),
          cell: (c) => <span className="tabular-nums">{formatInt(c.getValue())}</span>,
        }),
        col.accessor((r) => r.totals.cost_micro_usd, {
          id: "cost",
          header: t("columns.cost"),
          cell: (c) => <span className="tabular-nums" dir="ltr">{formatCostMicroUsd(c.getValue())}</span>,
        }),
        col.accessor(startedMs, {
          id: "started_at",
          header: t("columns.started"),
          sortDescFirst: true,
          cell: (c) => (
            <span className="whitespace-nowrap tabular-nums">{c.getValue() ? when.format(new Date(c.getValue())) : "-"}</span>
          ),
        }),
      ]),
    [t, when, onOpen],
  );

  const table = useTable({
    features,
    columns,
    data: rows as RunRow[],
    initialState: { sorting: [{ id: "started_at", desc: true }] },
  });

  return (
    <section aria-label={t("title")} className="flex flex-col gap-3">
      <h2 className="text-base font-semibold">{t("title")}</h2>
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
              <tr
                key={row.id}
                data-testid="run-row"
                data-status={row.original.status}
                data-run-id={row.original.id}
                aria-selected={row.original.id === selectedId}
                onClick={() => onOpen(row.original.id)}
                className={cn(
                  "cursor-pointer border-b last:border-0 hover:bg-muted/50",
                  row.original.id === selectedId && "bg-muted/50",
                )}
              >
                {row.getAllCells().map((cell) => (
                  <td key={cell.id} className="px-4 py-2.5">
                    <table.FlexRender cell={cell} />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
        {rows.length === 0 && <p className="p-8 text-center text-sm text-muted-foreground">{t("empty")}</p>}
      </div>
    </section>
  );
}

"use client";

import {
  createColumnHelper, createSortedRowModel, rowSortingFeature, tableFeatures, useTable,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react";
import { useTranslations } from "next-intl";
import { useMemo } from "react";
import type { ProposalRow } from "@/lib/agent-feed";
import { cn } from "@/lib/utils";

const features = tableFeatures({ rowSortingFeature, sortedRowModel: createSortedRowModel() });
const col = createColumnHelper<typeof features, ProposalRow>();

const STATE_TONE: Record<string, string> = {
  proposed: "bg-info-soft text-info",
  accepted: "bg-ok-soft text-ok",
  rejected: "bg-bad-soft text-bad",
};

/** Read-only: Phase 1 agents only propose; accepting and rejecting arrives with the review queue. */
export function ProposalsList({ rows, heading = true }: { rows: readonly ProposalRow[]; heading?: boolean }) {
  const t = useTranslations("P1Agents.proposals");
  const columns = useMemo(
    () =>
      col.columns([
        col.accessor("agent", { header: t("columns.agent"), cell: (c) => <span className="font-medium">{c.getValue()}</span> }),
        col.accessor("kind", { header: t("columns.kind"), cell: (c) => <span className="font-mono text-xs" dir="ltr">{c.getValue()}</span> }),
        col.accessor((r) => r.rationale ?? "", {
          id: "summary",
          header: t("columns.summary"),
          cell: (c) => <span className="block max-w-80 truncate" title={c.getValue()} dir="auto">{c.getValue() || "-"}</span>,
        }),
        col.accessor("confidence", {
          header: t("columns.confidence"),
          cell: (c) => <span className="tabular-nums" dir="ltr">{Math.round(c.getValue() * 100)}%</span>,
        }),
        col.accessor("state", {
          header: t("columns.state"),
          cell: (c) => (
            <span
              data-state={c.getValue()}
              className={cn(
                "inline-flex rounded-pill px-2.5 py-1 text-xs font-semibold whitespace-nowrap",
                STATE_TONE[c.getValue()] ?? "bg-muted text-muted-foreground",
              )}
            >
              {t.has(`state.${c.getValue()}`) ? t(`state.${c.getValue()}`) : c.getValue()}
            </span>
          ),
        }),
      ]),
    [t],
  );
  const table = useTable({ features, columns, data: rows as ProposalRow[] });

  return (
    <section aria-label={t("title")} className="flex flex-col gap-3">
      {heading && <h2 className="text-base font-semibold">{t("title")}</h2>}
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
              <tr key={row.id} data-testid="proposal-row" className="border-b last:border-0">
                {row.getAllCells().map((cell) => (
                  <td key={cell.id} className="px-4 py-2.5">
                    <table.FlexRender cell={cell} />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
        {rows.length === 0 && <p className="p-6 text-center text-sm text-muted-foreground">{t("empty")}</p>}
      </div>
    </section>
  );
}

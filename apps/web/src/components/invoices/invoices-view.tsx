"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { listInvoices } from "@/lib/p2/api";
import { isSettledStatus } from "@/lib/p2/actions";
import { InvoicesTable } from "./invoices-table";
import { SamplePicker } from "./sample-picker";

const POLL_MS = 3000;

/** /invoices: create a demo invoice from a sample, then browse this Firm's invoices. */
export function InvoicesView() {
  const list = useInfiniteQuery({
    queryKey: ["p2-invoices"],
    queryFn: ({ pageParam, signal }) => listInvoices(pageParam, signal),
    initialPageParam: null as string | null,
    staleTime: 0,
    getNextPageParam: (last) => last.next_cursor,
    // Poll only while some invoice is still moving through the pipeline.
    refetchInterval: (q) =>
      q.state.error || !q.state.data?.pages.some((p) => p.items.some((i) => !isSettledStatus(i.status))) ? false : POLL_MS,
  });
  const rows = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);

  return (
    <div className="flex flex-col gap-6">
      <SamplePicker />
      <InvoicesTable
        rows={rows}
        loading={list.isPending}
        error={list.isError}
        hasMore={list.hasNextPage}
        loadingMore={list.isFetchingNextPage}
        onLoadMore={() => void list.fetchNextPage()}
        onRetry={() => void list.refetch()}
      />
    </div>
  );
}

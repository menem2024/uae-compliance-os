import { requestJson } from "../api-client";
import type { ClientCompany } from "../clients";
import type { InvoiceList, InvoiceListItem } from "./types";

/** api-go has no counts endpoint, so the dashboard counts what the list endpoints return, within a bound. */
const PAGE_SIZE = 100;
const MAX_PAGES = 5;
/** Most records the dashboard counts per list. */
export const DASHBOARD_CAP = PAGE_SIZE * MAX_PAGES;

type Page<T> = { items: T[]; next_cursor: string | null };

/** Follows next_cursor for at most `maxPages` pages; `truncated` says more rows exist beyond the bound. */
export async function collectPages<T>(
  fetchPage: (cursor: string | null) => Promise<Page<T>>,
  maxPages = MAX_PAGES,
): Promise<{ items: T[]; truncated: boolean }> {
  const items: T[] = [];
  let cursor: string | null = null;
  for (let i = 0; i < maxPages; i++) {
    const page: Page<T> = await fetchPage(cursor);
    items.push(...page.items);
    if (!page.next_cursor) return { items, truncated: false };
    cursor = page.next_cursor;
  }
  return { items, truncated: true };
}

export type DashboardInvoices = {
  total: number;
  byStatus: Record<string, number>;
  /** The five newest invoices. */
  recent: InvoiceListItem[];
  truncated: boolean;
};

export function summarizeInvoices(items: InvoiceListItem[], truncated = false): DashboardInvoices {
  const byStatus: Record<string, number> = {};
  for (const i of items) byStatus[i.status] = (byStatus[i.status] ?? 0) + 1;
  const recent = [...items].sort((a, b) => (a.created_at < b.created_at ? 1 : a.created_at > b.created_at ? -1 : 0)).slice(0, 5);
  return { total: items.length, byStatus, recent, truncated };
}

export type DashboardData = { invoices: DashboardInvoices; clients: { count: number; truncated: boolean } };

export async function fetchDashboard(signal?: AbortSignal): Promise<DashboardData> {
  const [inv, cli] = await Promise.all([
    collectPages((cursor) => {
      const p = new URLSearchParams({ limit: String(PAGE_SIZE) });
      if (cursor) p.set("cursor", cursor);
      return requestJson<InvoiceList>(`/api/invoices?${p.toString()}`, { signal });
    }),
    collectPages((cursor) => {
      const p = new URLSearchParams({ status: "active", limit: String(PAGE_SIZE) });
      if (cursor) p.set("cursor", cursor);
      return requestJson<Page<ClientCompany>>(`/api/client-companies?${p.toString()}`, { signal });
    }),
  ]);
  return {
    invoices: summarizeInvoices(inv.items, inv.truncated),
    clients: { count: cli.items.length, truncated: cli.truncated },
  };
}

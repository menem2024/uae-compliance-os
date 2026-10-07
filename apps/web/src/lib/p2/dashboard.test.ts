import { describe, expect, it } from "vitest";
import { collectPages, summarizeInvoices } from "./dashboard";
import type { InvoiceListItem } from "./types";

const inv = (n: number, status: string): InvoiceListItem => ({
  id: `id-${n}`, status, payload_version: 1, latest_run_id: null, invoice_number: `INV-${n}`, issue_date: "2026-01-01",
  invoice_type_code: "380", currency: "AED", total_amount: "10", seller_name: "S", buyer_name: "B",
  error_count: 0, warning_count: 0, created_at: `2026-02-${String(n).padStart(2, "0")}T00:00:00Z`, updated_at: "",
});

describe("summarizeInvoices", () => {
  it("counts by status and keeps the 5 newest", () => {
    const items = [1, 2, 3, 4, 5, 6, 7].map((n) => inv(n, n % 2 ? "validated" : "has_issues"));
    const s = summarizeInvoices(items);
    expect(s.total).toBe(7);
    expect(s.byStatus).toEqual({ validated: 4, has_issues: 3 });
    expect(s.recent.map((i) => i.id)).toEqual(["id-7", "id-6", "id-5", "id-4", "id-3"]);
    expect(s.truncated).toBe(false);
  });

  it("handles no invoices", () => {
    expect(summarizeInvoices([])).toEqual({ total: 0, byStatus: {}, recent: [], truncated: false });
  });
});

describe("collectPages", () => {
  it("follows the cursor until the last page", async () => {
    const pages = [{ items: [1, 2], next_cursor: "a" }, { items: [3], next_cursor: null }];
    const seen: (string | null)[] = [];
    const r = await collectPages(async (c) => (seen.push(c), pages[seen.length - 1]));
    expect(r).toEqual({ items: [1, 2, 3], truncated: false });
    expect(seen).toEqual([null, "a"]);
  });

  it("stops at the bound and reports truncation", async () => {
    let n = 0;
    const r = await collectPages(async () => ({ items: [++n], next_cursor: "more" }), 3);
    expect(r).toEqual({ items: [1, 2, 3], truncated: true });
  });
});

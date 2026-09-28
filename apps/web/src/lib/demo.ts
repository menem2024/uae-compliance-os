/** Fixed demo invoice fields (Phase 0 walking skeleton); only the seller TRN is editable. */
export const DEMO_FIXED = {
  invoice_number: "INV-DEMO-1",
  buyer_trn: "100000000000003",
  currency: "AED",
  total_amount: "1050.00",
  vat_amount: "50.00",
} as const;

export type DemoInvoice = typeof DEMO_FIXED & { issue_date: string; seller_trn: string };

const pad = (n: number) => String(n).padStart(2, "0");

/** The viewer's local calendar date as `YYYY-MM-DD` (always Western digits). */
export function localDateISO(now: Date): string {
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

/** POST /api/invoices body. Money stays a decimal string, never a float. */
export function demoInvoice(sellerTrn: string, now: Date): DemoInvoice {
  return { ...DEMO_FIXED, issue_date: localDateISO(now), seller_trn: sellerTrn.trim() };
}

const TERMINAL = new Set(["validated", "has_issues"]);

export function isTerminalStatus(status: string | undefined): boolean {
  return status !== undefined && TERMINAL.has(status);
}

/** TanStack `refetchInterval`: every second until validation finished. */
export function pollInterval(status: string | undefined): number | false {
  return isTerminalStatus(status) ? false : 1000;
}

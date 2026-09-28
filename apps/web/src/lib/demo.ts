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

/** Hard cap on result polls (successes and errors combined): about 2 minutes at 1/s. */
export const MAX_POLLS = 120;

export type PollState = {
  dataUpdateCount: number;
  errorUpdateCount: number;
  status?: string;
  /** HTTP status of the last failed poll, if the last poll failed. */
  errorStatus?: number | null;
};

export function pollExhausted(s: Pick<PollState, "dataUpdateCount" | "errorUpdateCount">): boolean {
  return s.dataUpdateCount + s.errorUpdateCount >= MAX_POLLS;
}

/**
 * TanStack `refetchInterval` for the demo result. A 404 right after the 202 is a
 * read-your-write race, so it keeps polling; other errors stop. Everything is capped.
 */
export function nextPoll(s: PollState): number | false {
  if (pollExhausted(s)) return false;
  if (s.errorStatus != null) return s.errorStatus === 404 ? 1000 : false;
  return pollInterval(s.status);
}

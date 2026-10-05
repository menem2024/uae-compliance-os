import type { AuditItem } from "./types";

const ERROR_KEYS = new Set([
  "stale_payload", "old_value_mismatch", "bad_path", "not_validated", "not_ready", "revalidation_required",
  "validator_unavailable", "exporter_unavailable", "not_found", "rate_limited", "unauthenticated", "upstream_unavailable",
]);

/** Key under `P2Invoices.errors` for an `ApiError.code`; unknown codes get the generic message. */
export function errorMessageKey(code: string): string {
  return ERROR_KEYS.has(code) ? code : "generic";
}

const trim = (n: number) => String(Number(n.toFixed(2)));

/** Validation run duration (microseconds) for humans; Western digits in both locales. */
export function formatDurationUs(us: number): string {
  if (!Number.isFinite(us) || us < 0) return "—";
  if (us < 1000) return `${Math.round(us)} µs`;
  if (us < 1_000_000) return `${trim(us / 1000)} ms`;
  return `${trim(us / 1_000_000)} s`;
}

/** The audit trail oldest first (the order the review happened in); ties by id. */
export function sortAudit(items: readonly AuditItem[]): AuditItem[] {
  return [...items].sort((a, b) => {
    const t = Date.parse(a.occurred_at) - Date.parse(b.occurred_at);
    return t !== 0 ? t : a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
}

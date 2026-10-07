/**
 * Western digits in both locales (adr/016). Never format numbers with an Arabic
 * locale such as `ar-AE`, which yields Arabic-Indic digits.
 */
const integer = new Intl.NumberFormat("en-US");

export function formatInt(n: number): string {
  return integer.format(n);
}

/** Date-only formatter: invoice issue dates carry no time zone, so they are read and shown as UTC dates. */
export const dateOnlyFormat = (locale: string) =>
  new Intl.DateTimeFormat(`${locale}-u-nu-latn`, { dateStyle: "medium", timeZone: "UTC" });

/** "2026-01-12" (or a longer ISO string) to a UTC Date; null for anything else. */
export function parseDateOnly(s: string | null | undefined): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(s ?? "");
  if (!m) return null;
  const d = new Date(Date.UTC(+m[1], +m[2] - 1, +m[3]));
  return Number.isNaN(d.getTime()) || d.getUTCMonth() !== +m[2] - 1 ? null : d;
}

/** Decimal string ("69648.6") to grouped text with at least two decimals ("69,648.60"); other input is returned as is. */
export function formatMoney(s: string): string {
  const m = /^(-?)(\d+)(?:\.(\d+))?$/.exec(s);
  if (!m) return s;
  const int = m[2].replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return `${m[1]}${int}.${(m[3] ?? "").padEnd(2, "0")}`;
}

/** UAE mandatory e-invoicing go-live for SMEs (prototype copy: 1 July 2027). */
export const MANDATE_DATE = { year: 2027, monthIndex: 6, day: 1 } as const;

const DAY_MS = 86_400_000;

/** Whole calendar days from `now` (local date) to the mandate date; never negative. */
export function daysToMandate(now: Date = new Date()): number {
  const today = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate());
  const target = Date.UTC(MANDATE_DATE.year, MANDATE_DATE.monthIndex, MANDATE_DATE.day);
  return Math.max(0, Math.round((target - today) / DAY_MS));
}

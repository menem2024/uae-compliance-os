/** UAE mandatory e-invoicing go-live for SMEs (prototype copy: 1 July 2027). */
export const MANDATE_DATE = { year: 2027, monthIndex: 6, day: 1 } as const;

const DAY_MS = 86_400_000;

/** Calendar date (y, m, d) of `now` in `timeZone`, or in the runtime's local zone when omitted. */
function calendarDate(now: Date, timeZone?: string): [number, number, number] {
  if (!timeZone) return [now.getFullYear(), now.getMonth(), now.getDate()];
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone,
    year: "numeric",
    month: "numeric",
    day: "numeric",
  }).formatToParts(now);
  const get = (type: string) => Number(parts.find((p) => p.type === type)?.value);
  return [get("year"), get("month") - 1, get("day")];
}

/**
 * Whole calendar days from `now` to the mandate date; never negative. The day is taken in
 * `timeZone` (default: the runtime's local zone, i.e. the browser's after hydration).
 */
export function daysToMandate(now: Date = new Date(), timeZone?: string): number {
  const [y, m, d] = calendarDate(now, timeZone);
  const today = Date.UTC(y, m, d);
  const target = Date.UTC(MANDATE_DATE.year, MANDATE_DATE.monthIndex, MANDATE_DATE.day);
  return Math.max(0, Math.round((target - today) / DAY_MS));
}

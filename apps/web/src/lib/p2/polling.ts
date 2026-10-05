import { nextPoll, type PollState } from "../demo";
import { isSettledStatus } from "./actions";

/**
 * TanStack `refetchInterval` for the invoice detail: poll while the status is still moving, stop when it
 * settles. Reuses the demo's cap and 404 handling (`nextPoll`); only the notion of "settled" differs because
 * the review flow goes on to `ready`.
 */
export function detailRefetchInterval(s: PollState): number | false {
  if (isSettledStatus(s.status)) return false;
  return nextPoll({ ...s, status: undefined });
}

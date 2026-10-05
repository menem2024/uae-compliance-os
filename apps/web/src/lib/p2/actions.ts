/** Statuses where nothing is in flight: the state machine waits for a human (spec 5.6.1). */
const SETTLED = new Set(["validated", "has_issues", "ready", "needs_review"]);

export function isSettledStatus(status: string | undefined): boolean {
  return status !== undefined && SETTLED.has(status);
}

export type Actions = { correct: boolean; revalidate: boolean; approve: boolean; export: boolean };

/**
 * What the reviewer may do now. Mirrors the server rules: approve needs `validated` with no errors, export
 * needs `ready`; the server still enforces both (409 not_validated / not_ready).
 */
export function availableActions(status: string | undefined, errorCount: number): Actions {
  const settled = isSettledStatus(status);
  return {
    correct: settled && status !== "ready",
    revalidate: settled,
    approve: status === "validated" && errorCount === 0,
    export: status === "ready",
  };
}

import type { Issue } from "./types";

/** The validator ships both languages; show the viewer's, falling back to the other when it is empty. */
export function issueMessage(issue: Pick<Issue, "message" | "message_ar">, locale: string): string {
  const [first, second] = locale === "ar" ? [issue.message_ar, issue.message] : [issue.message, issue.message_ar];
  return first || second || "";
}

const TERM = /^([A-Za-z]+)-(\d+)/;

/** "IBT-9" before "IBT-112"; an empty term sorts after every other one. */
function compareTerm(a: string, b: string): number {
  if (a === b) return 0;
  if (a === "") return 1;
  if (b === "") return -1;
  const ma = TERM.exec(a);
  const mb = TERM.exec(b);
  if (ma && mb) {
    if (ma[1] !== mb[1]) return ma[1] < mb[1] ? -1 : 1;
    const d = Number(ma[2]) - Number(mb[2]);
    if (d !== 0) return d;
  }
  return a < b ? -1 : 1;
}

const cmp = (a: string, b: string) => (a === b ? 0 : a < b ? -1 : 1);

/** Errors first, then by business term, rule id and path. Returns a new array. */
export function sortIssues(issues: readonly Issue[]): Issue[] {
  return [...issues].sort(
    (a, b) =>
      (a.severity === b.severity ? 0 : a.severity === "error" ? -1 : 1) ||
      compareTerm(a.business_term, b.business_term) ||
      cmp(a.rule_id, b.rule_id) ||
      cmp(a.path, b.path),
  );
}

export function groupIssues(issues: readonly Issue[]): { errors: Issue[]; warnings: Issue[] } {
  const sorted = sortIssues(issues);
  return {
    errors: sorted.filter((i) => i.severity === "error"),
    warnings: sorted.filter((i) => i.severity !== "error"),
  };
}

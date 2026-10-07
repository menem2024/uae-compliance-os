import { describe, expect, it } from "vitest";
import { groupIssues, issueMessage, sortIssues } from "./issues";
import type { Issue } from "./types";

const issue = (over: Partial<Issue>): Issue => ({
  rule_id: "r", severity: "error", path: "p", business_term: "", message: "en msg", message_ar: "رسالة",
  message_args: null, fixable: false, suggested_value: "", ...over,
});

describe("issueMessage", () => {
  it("picks the Arabic message for ar and the English one otherwise", () => {
    const i = issue({});
    expect(issueMessage(i, "ar")).toBe("رسالة");
    expect(issueMessage(i, "en")).toBe("en msg");
    expect(issueMessage(i, "fr")).toBe("en msg");
  });
  it("falls back to the other language when one is empty", () => {
    expect(issueMessage(issue({ message_ar: "" }), "ar")).toBe("en msg");
    expect(issueMessage(issue({ message: "" }), "en")).toBe("رسالة");
    expect(issueMessage(issue({ message: "", message_ar: "" }), "en")).toBe("");
  });
});

describe("sortIssues", () => {
  it("puts errors first, then orders by business term numerically, empty terms last", () => {
    const list = [
      issue({ rule_id: "w", severity: "warning", business_term: "IBT-001" }),
      issue({ rule_id: "c", business_term: "" }),
      issue({ rule_id: "b", business_term: "IBT-112" }),
      issue({ rule_id: "a", business_term: "IBT-9" }),
    ];
    expect(sortIssues(list).map((i) => i.rule_id)).toEqual(["a", "b", "c", "w"]);
  });
  it("breaks ties by rule id then path and does not mutate its input", () => {
    const list = [
      issue({ rule_id: "x", path: "b", business_term: "IBT-1" }),
      issue({ rule_id: "x", path: "a", business_term: "IBT-1" }),
      issue({ rule_id: "a", path: "z", business_term: "IBT-1" }),
    ];
    const copy = [...list];
    expect(sortIssues(list).map((i) => `${i.rule_id}:${i.path}`)).toEqual(["a:z", "x:a", "x:b"]);
    expect(list).toEqual(copy);
  });
});

describe("groupIssues", () => {
  it("splits into sorted errors and warnings", () => {
    const g = groupIssues([
      issue({ rule_id: "w", severity: "warning" }),
      issue({ rule_id: "e2", business_term: "IBT-2" }),
      issue({ rule_id: "e1", business_term: "IBT-1" }),
    ]);
    expect(g.errors.map((i) => i.rule_id)).toEqual(["e1", "e2"]);
    expect(g.warnings.map((i) => i.rule_id)).toEqual(["w"]);
  });
  it("is empty for no issues", () => {
    expect(groupIssues([])).toEqual({ errors: [], warnings: [] });
  });
});

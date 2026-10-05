import valid from "./01-valid.json";
import totalsMismatch from "./02-totals-mismatch.json";
import badTrn from "./03-bad-trn.json";
import missingFields from "./04-missing-fields.json";

/**
 * The canonical demo invoices (`POST /api/invoices` bodies). They are sent exactly as they are: the
 * `issue_date` is deliberately not rewritten to today.
 */
export type DemoSampleId = "valid" | "totalsMismatch" | "badTrn" | "missingFields";

export type DemoSample = {
  id: DemoSampleId;
  /** Key under `P2Invoices.samples` (`.title`, `.description`). */
  i18nKey: `samples.${DemoSampleId}`;
  /** Status the invoice is expected to settle on after the first validation. */
  expected: "validated" | "has_issues";
  /** A rule that must fire, when the sample is built to trigger one. */
  expectedRuleId?: string;
  body: Record<string, unknown>;
};

export const DEMO_SAMPLES: readonly DemoSample[] = [
  { id: "valid", i18nKey: "samples.valid", expected: "validated", body: valid },
  { id: "totalsMismatch", i18nKey: "samples.totalsMismatch", expected: "has_issues", expectedRuleId: "ibr-co-16", body: totalsMismatch },
  { id: "badTrn", i18nKey: "samples.badTrn", expected: "has_issues", body: badTrn },
  { id: "missingFields", i18nKey: "samples.missingFields", expected: "has_issues", body: missingFields },
];

export function demoSample(id: string): DemoSample | undefined {
  return DEMO_SAMPLES.find((s) => s.id === id);
}

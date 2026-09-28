/** api-go `GET /v1/invoices/{id}` response (via the BFF). */
export type InvoiceIssue = {
  rule_id: string;
  severity: "error" | "warning";
  path: string;
  message: string;
};

export type InvoiceResult = {
  id: string;
  status: string;
  ruleset_version: string | null;
  issues: InvoiceIssue[];
};

/** A non-2xx BFF answer, keeping the status and the trace id for the UI. */
export class HttpError extends Error {
  constructor(
    readonly status: number,
    readonly traceId: string | null,
  ) {
    super(`HTTP ${status}`);
    this.name = "HttpError";
  }
}

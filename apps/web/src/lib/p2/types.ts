/** JSON shapes of the Track C invoice-review routes (api-go `trackc_routes.go`), as the BFF passes them through. */

export type Severity = "error" | "warning";

export type Issue = {
  rule_id: string;
  severity: Severity;
  path: string;
  business_term: string;
  message: string;
  message_ar: string;
  message_args: Record<string, string> | null;
  fixable: boolean;
  suggested_value: string;
};

export type Run = {
  id: string;
  payload_version: number;
  ruleset_version: string;
  trigger: string;
  error_count: number;
  warning_count: number;
  rules_evaluated: number;
  duration_us: number;
  requested_by: string;
  trace_id: string;
  created_at: string;
  issues?: Issue[];
};

export type Approval = { approved_payload_version: number; approved_by: string; approved_at: string };

/** `GET /v1/invoices/{id}/validation`. `payload` is the canonical invoice (proto field names). */
export type InvoiceDetail = {
  id: string;
  status: string;
  payload_version: number;
  payload: Record<string, unknown>;
  ruleset_version: string | null;
  approval: Approval | null;
  latest_run: Run | null;
  created_at: string;
  updated_at: string;
};

export type InvoiceListItem = {
  id: string;
  status: string;
  payload_version: number;
  latest_run_id: string | null;
  invoice_number: string;
  issue_date: string;
  invoice_type_code: string;
  currency: string;
  total_amount: string;
  seller_name: string;
  buyer_name: string;
  error_count: number;
  warning_count: number;
  created_at: string;
  updated_at: string;
};

export type InvoiceList = { items: InvoiceListItem[]; next_cursor: string | null };

export type FieldChange = { path: string; old_value: string; new_value: string };

export type CorrectionBody = { payload_version: number; changes: FieldChange[]; reason: string };

export type AuditItem = {
  id: string;
  occurred_at: string;
  actor_type: string;
  actor_id: string;
  agent: string;
  action: string;
  entity_type: string;
  changes: unknown;
  before: unknown;
  after: unknown;
  reason: string;
  trace_id: string;
};

export type ExportRecord = {
  id: string;
  invoice_id: string;
  payload_version: number;
  ruleset_version: string;
  format: string;
  sha256: string;
  size_bytes: number;
  filename: string;
  created_at: string;
};

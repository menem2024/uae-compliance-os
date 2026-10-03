/** Pure upload state for /documents: one reducer per queue, no DOM and no I/O (spec 5.7). */

export type UploadState =
  | "hashing"
  | "requesting"
  | "uploading"
  | "completing"
  | "processing"
  | "extracted"
  | "needs_review"
  | "not_invoice"
  | "failed"
  | "rejected"
  | "duplicate";

export type UploadItem = {
  id: string;
  file: File;
  state: UploadState;
  /** 0..1 of the PUT. */
  progress: number;
  documentId?: string;
  /** api-go error code or a short failure reason; never file content. */
  error?: string;
};

export type UploadAction =
  | { type: "Hashed"; id: string }
  | { type: "Requested"; id: string; documentId: string }
  | { type: "Duplicate"; id: string; documentId: string }
  | { type: "UploadProgress"; id: string; progress: number }
  | { type: "Uploaded"; id: string }
  | { type: "Completed"; id: string; status: string; error?: string }
  | { type: "StatusChanged"; id: string; status: string }
  | { type: "Errored"; id: string; error: string; terminal: "failed" | "rejected" };

const TERMINAL: ReadonlySet<UploadState> = new Set([
  "extracted", "needs_review", "not_invoice", "failed", "rejected", "duplicate",
]);

/**
 * The presigned PUT is create-only (If-None-Match: *), so a retry after a lost response, or a re-request
 * of a still-pending upload, answers 412: the object is already there. That is not a failure: "complete"
 * re-verifies size and sha256 server-side and rejects (and removes) wrong bytes.
 */
export const putSucceeded = (status: number): boolean => (status >= 200 && status < 300) || status === 412;

export const isTerminal = (s: UploadState): boolean => TERMINAL.has(s);

/** Server statuses a processing Document can settle in; each one is also an UploadState. */
const SETTLED: ReadonlySet<string> = new Set(["extracted", "needs_review", "not_invoice", "failed", "rejected"]);

function step(item: UploadItem, a: UploadAction): UploadItem {
  if (isTerminal(item.state)) return item;
  switch (a.type) {
    case "Hashed":
      return item.state === "hashing" ? { ...item, state: "requesting" } : item;
    case "Requested":
      return item.state === "requesting" ? { ...item, state: "uploading", documentId: a.documentId } : item;
    case "Duplicate":
      return item.state === "requesting" ? { ...item, state: "duplicate", documentId: a.documentId } : item;
    case "UploadProgress":
      return item.state === "uploading" ? { ...item, progress: Math.min(1, Math.max(0, a.progress)) } : item;
    case "Uploaded":
      return item.state === "uploading" ? { ...item, state: "completing", progress: 1 } : item;
    case "Completed":
      if (item.state !== "completing") return item;
      return a.status === "rejected"
        ? { ...item, state: "rejected", error: a.error }
        : { ...item, state: "processing" };
    case "StatusChanged":
      return item.state === "processing" && SETTLED.has(a.status) ? { ...item, state: a.status as UploadState } : item;
    case "Errored":
      return { ...item, state: a.terminal, error: a.error };
  }
}

export function uploadReducer(items: UploadItem[], action: UploadAction): UploadItem[] {
  let changed = false;
  const next = items.map((item) => {
    if (item.id !== action.id) return item;
    const out = step(item, action);
    if (out !== item) changed = true;
    return out;
  });
  return changed ? next : items;
}

/** api-go `documents.View` (the fields this page reads). */
export type DocumentRow = {
  id: string;
  client_company_id: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  status: string;
  status_reason: string;
  kind: string;
  review_reasons: string[];
  invoice_count: number;
  latest_run_id: string | null;
  created_at: string;
  updated_at: string;
};

const DOC_IN_FLIGHT: ReadonlySet<string> = new Set(["pending_upload", "uploaded", "processing"]);

/** A row untouched this long is treated as stuck, not as work in progress, so polling cannot run forever. */
export const POLL_WINDOW_MS = 30 * 60_000;

export const isDocumentInFlight = (status: string): boolean => DOC_IN_FLIGHT.has(status);

/** True while the list must keep polling: a recent row, or an upload we started, is not settled yet. */
export function documentsNeedPolling(
  rows: readonly DocumentRow[],
  items: readonly UploadItem[],
  now: number = Date.now(),
): boolean {
  const live = (r: DocumentRow) => isDocumentInFlight(r.status) && now - Date.parse(r.updated_at) < POLL_WINDOW_MS;
  return rows.some(live) || items.some((i) => i.state === "processing");
}

/** "1.4 MB" with Western digits (adr/016); decimal units. */
export function formatBytes(n: number): string {
  if (n < 1000) return `${n} B`;
  const units = ["KB", "MB", "GB"];
  let v = n / 1000;
  let u = 0;
  while (v >= 1000 && u < units.length - 1) {
    v /= 1000;
    u++;
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[u]}`;
}

/** Settles items waiting on the pipeline from the Document list (derive on render; no effect, no extra state). */
export function settleFromRows(items: UploadItem[], rows: readonly Pick<DocumentRow, "id" | "status">[]): UploadItem[] {
  const status = new Map(rows.map((r) => [r.id, r.status]));
  return items.reduce<UploadItem[]>((acc, i) => {
    const s = i.state === "processing" && i.documentId ? status.get(i.documentId) : undefined;
    return s ? uploadReducer(acc, { type: "StatusChanged", id: i.id, status: s }) : acc;
  }, items);
}

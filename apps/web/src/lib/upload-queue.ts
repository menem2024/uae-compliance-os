import { ApiError } from "./api-client";
import type { UploadAction, UploadItem } from "./uploads";

/** Runs worker over items with at most `concurrency` in flight. A rejection never stops the others. */
export async function runQueue<T>(
  items: readonly T[],
  concurrency: number,
  worker: (item: T) => Promise<void>,
): Promise<void> {
  let next = 0;
  const lane = async () => {
    while (next < items.length) {
      const item = items[next++] as T;
      try {
        await worker(item);
      } catch {
        // The worker reports its own failure (it dispatches Errored); the lane just moves on.
      }
    }
  };
  const lanes = Math.max(1, Math.min(Math.floor(concurrency) || 1, items.length));
  await Promise.all(Array.from({ length: lanes }, lane));
}

export const UPLOAD_CONCURRENCY = 4;
/** api-go `documents.MaxFiles`. */
export const MAX_FILES_PER_REQUEST = 100;

export type FileRequest = { filename: string; content_type: string; size_bytes: number; sha256: string };
export type PutTarget = { url: string; method: string; headers: Record<string, string>; expires_at: string };
export type UploadResponseItem = {
  document_id: string | null;
  status: string;
  deduplicated: boolean;
  upload: PutTarget | null;
  error?: string;
};
export type CompleteResponseItem = { document_id: string; status: string; error?: string };

/** The I/O seams of the pipeline; the page wires real ones, tests wire fakes. */
export type UploadDeps = {
  hash: (file: File) => Promise<string>;
  requestUploads: (clientCompanyId: string, files: FileRequest[]) => Promise<UploadResponseItem[]>;
  put: (target: PutTarget, file: File, onProgress: (fraction: number) => void) => Promise<void>;
  complete: (documentIds: string[]) => Promise<CompleteResponseItem[]>;
};

const reason = (err: unknown): string =>
  err instanceof ApiError ? err.code : typeof err === "string" ? err : "upload_failed";

const BY_EXTENSION: Readonly<Record<string, string>> = {
  csv: "text/csv",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
};

/** Browsers leave `type` empty for CSV/XLSX on some platforms; fall back to the extension. */
export function contentTypeOf(file: Pick<File, "name" | "type">): string {
  if (file.type) return file.type;
  const ext = file.name.slice(file.name.lastIndexOf(".") + 1).toLowerCase();
  return BY_EXTENSION[ext] ?? "";
}

const chunk = <T>(xs: readonly T[], n: number): T[][] =>
  Array.from({ length: Math.ceil(xs.length / n) }, (_, i) => xs.slice(i * n, (i + 1) * n));

type Hashed = { item: UploadItem; sha256: string };
type Pending = { item: UploadItem; documentId: string; target: PutTarget };

/**
 * hash (4-way) -> one signed-upload request per 100 files -> PUT (4-way) -> one complete per 100.
 * Batching keeps a 100-file drop inside the 20/min uploads limit; every failure is dispatched as Errored.
 */
export async function runUploads(opts: {
  items: readonly UploadItem[];
  clientCompanyId: string;
  dispatch: (a: UploadAction) => void;
  deps: UploadDeps;
  concurrency?: number;
}): Promise<void> {
  const { items, clientCompanyId, dispatch, deps } = opts;
  const concurrency = opts.concurrency ?? UPLOAD_CONCURRENCY;
  const fail = (id: string, err: unknown, terminal: "failed" | "rejected" = "failed") =>
    dispatch({ type: "Errored", id, error: reason(err), terminal });

  const hashed: Hashed[] = [];
  await runQueue(items, concurrency, async (item) => {
    try {
      const sha256 = await deps.hash(item.file);
      hashed.push({ item, sha256 });
      dispatch({ type: "Hashed", id: item.id });
    } catch (err) {
      fail(item.id, err);
    }
  });
  hashed.sort((a, b) => items.indexOf(a.item) - items.indexOf(b.item));

  const pending: Pending[] = [];
  for (const group of chunk(hashed, MAX_FILES_PER_REQUEST)) {
    let res: UploadResponseItem[];
    try {
      res = await deps.requestUploads(
        clientCompanyId,
        group.map(({ item, sha256 }) => ({
          filename: item.file.name,
          content_type: contentTypeOf(item.file),
          size_bytes: item.file.size,
          sha256,
        })),
      );
    } catch (err) {
      for (const { item } of group) fail(item.id, err);
      continue;
    }
    group.forEach(({ item }, i) => {
      const r = res[i];
      if (!r || r.error) return fail(item.id, r?.error ?? "upload_failed", "rejected");
      if (!r.document_id) return fail(item.id, "upload_failed");
      if (r.deduplicated || !r.upload) {
        return dispatch({ type: "Duplicate", id: item.id, documentId: r.document_id });
      }
      dispatch({ type: "Requested", id: item.id, documentId: r.document_id });
      pending.push({ item, documentId: r.document_id, target: r.upload });
    });
  }

  const uploaded: Pending[] = [];
  await runQueue(pending, concurrency, async (p) => {
    try {
      await deps.put(p.target, p.item.file, (progress) => dispatch({ type: "UploadProgress", id: p.item.id, progress }));
      dispatch({ type: "Uploaded", id: p.item.id });
      uploaded.push(p);
    } catch (err) {
      fail(p.item.id, err);
    }
  });

  for (const group of chunk(uploaded, MAX_FILES_PER_REQUEST)) {
    let res: CompleteResponseItem[];
    try {
      res = await deps.complete(group.map((p) => p.documentId));
    } catch (err) {
      for (const p of group) fail(p.item.id, err);
      continue;
    }
    const byDoc = new Map(res.map((r) => [r.document_id, r]));
    for (const p of group) {
      const r = byDoc.get(p.documentId);
      if (!r) fail(p.item.id, "upload_failed");
      else if (r.error && r.status !== "rejected") fail(p.item.id, r.error);
      else dispatch({ type: "Completed", id: p.item.id, status: r.status, error: r.error });
    }
  }
}

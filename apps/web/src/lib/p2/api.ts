import { ApiError, requestJson } from "@/lib/api-client";
import type {
  AuditItem, CorrectionBody, ExportRecord, InvoiceDetail, InvoiceList,
} from "./types";

/** Browser-side calls to the BFF for the invoice review flow. Everything goes through `/api/*`. */

const enc = encodeURIComponent;
const apiInvoice = (id: string) => `/api/invoices/${enc(id)}`;

export const listInvoices = (cursor: string | null, signal?: AbortSignal) => {
  const p = new URLSearchParams({ limit: "50" });
  if (cursor) p.set("cursor", cursor);
  return requestJson<InvoiceList>(`/api/invoices?${p.toString()}`, { signal });
};

/** Sends a canonical demo invoice exactly as it is. */
export const createInvoice = (body: Record<string, unknown>) =>
  requestJson<{ id: string; status: string }>("/api/invoices", { method: "POST", json: body });

export const getDetail = (id: string, signal?: AbortSignal) =>
  requestJson<InvoiceDetail>(`${apiInvoice(id)}/validation`, { signal });

export const getAudit = (id: string, signal?: AbortSignal) =>
  requestJson<{ items: AuditItem[] }>(`${apiInvoice(id)}/validation/audit`, { signal });

export const revalidate = (id: string) => requestJson<unknown>(`${apiInvoice(id)}/validation`, { method: "POST" });

export const postCorrection = (id: string, body: CorrectionBody) =>
  requestJson<{ payload_version: number; revalidation: "done" | "pending" }>(
    `${apiInvoice(id)}/validation/corrections`,
    { method: "POST", json: body },
  );

export const approveInvoice = (id: string, payloadVersion: number) =>
  requestJson<{ status: string; payload_version: number }>(`${apiInvoice(id)}/validation/approve`, {
    method: "POST",
    json: { payload_version: payloadVersion },
  });

export const createExport = (invoiceId: string) =>
  requestJson<ExportRecord>("/api/exports", { method: "POST", json: { invoice_id: invoiceId } });

/** Fetches the XML through the BFF and hands it to the browser as a file download. */
export async function downloadExport(exp: Pick<ExportRecord, "id" | "filename">): Promise<void> {
  const res = await fetch(`/api/exports/${enc(exp.id)}/xml`, { cache: "no-store" });
  if (!res.ok) throw new ApiError(res.status, `http_${res.status}`, res.headers.get("x-trace-id"));
  const url = URL.createObjectURL(await res.blob());
  try {
    const a = document.createElement("a");
    a.href = url;
    a.download = exp.filename || "invoice.xml";
    document.body.append(a);
    a.click();
    a.remove();
  } finally {
    setTimeout(() => URL.revokeObjectURL(url), 10_000);
  }
}

import type { NextRequest } from "next/server";
import { invoiceValidationPath } from "@/lib/bff-paths";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: GET /api/invoices/{id}/validation/audit -> the invoice's audit trail. */
export async function GET(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  return proxyJson(req, invoiceValidationPath((await params).id, "audit"), "GET");
}

import type { NextRequest } from "next/server";
import { invoiceValidationPath } from "@/lib/bff-paths";
import { proxyJson } from "@/lib/server/bff-route";

type Ctx = { params: Promise<{ id: string }> };

/** BFF: GET /api/invoices/{id}/validation -> invoice detail with the latest run and its issues. */
export async function GET(req: NextRequest, { params }: Ctx) {
  return proxyJson(req, invoiceValidationPath((await params).id), "GET");
}

/** BFF: POST /api/invoices/{id}/validation -> re-validate now. */
export async function POST(req: NextRequest, { params }: Ctx) {
  return proxyJson(req, invoiceValidationPath((await params).id), "POST");
}

import type { NextRequest } from "next/server";
import { invoiceValidationPath } from "@/lib/bff-paths";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: POST /api/invoices/{id}/validation/corrections -> api-go (JSON body passed through). */
export async function POST(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  return proxyJson(req, invoiceValidationPath((await params).id, "corrections"), "POST");
}

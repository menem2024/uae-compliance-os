import type { NextRequest } from "next/server";
import { clientCompanyPath } from "@/lib/bff-paths";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: POST /api/client-companies/{id}/restore. */
export async function POST(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  return proxyJson(req, clientCompanyPath((await params).id, "restore"), "POST");
}

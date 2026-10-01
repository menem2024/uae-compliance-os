import type { NextRequest } from "next/server";
import { clientCompanyPath } from "@/lib/bff-paths";
import { proxyJson } from "@/lib/server/bff-route";

type Ctx = { params: Promise<{ id: string }> };

/** BFF: GET/PATCH /api/client-companies/{id}. */
export async function GET(req: NextRequest, { params }: Ctx) {
  return proxyJson(req, clientCompanyPath((await params).id), "GET");
}
export async function PATCH(req: NextRequest, { params }: Ctx) {
  return proxyJson(req, clientCompanyPath((await params).id), "PATCH");
}

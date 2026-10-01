import type { NextRequest } from "next/server";
import { documentPath } from "@/lib/bff-paths";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: GET /api/documents/{id}. */
export async function GET(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  return proxyJson(req, documentPath((await params).id), "GET");
}

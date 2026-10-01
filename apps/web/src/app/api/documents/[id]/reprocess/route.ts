import type { NextRequest } from "next/server";
import { documentPath } from "@/lib/bff-paths";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: POST /api/documents/{id}/reprocess. */
export async function POST(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  return proxyJson(req, documentPath((await params).id, "reprocess"), "POST");
}

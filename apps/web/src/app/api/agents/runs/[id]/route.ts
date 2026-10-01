import type { NextRequest } from "next/server";
import { runPath } from "@/lib/bff-paths";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: GET /api/agents/runs/{id} (run, steps, proposals). */
export async function GET(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  return proxyJson(req, runPath((await params).id), "GET");
}

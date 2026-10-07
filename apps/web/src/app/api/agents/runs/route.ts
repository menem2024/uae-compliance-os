import type { NextRequest } from "next/server";
import { withQuery } from "@/lib/bff-paths";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: GET /api/agents/runs -> api-go GET /v1/agents/runs. */
export const GET = (req: NextRequest) =>
  proxyJson(req, withQuery("/v1/agents/runs", req.nextUrl.searchParams, ["subject_type", "subject_id", "status", "limit", "cursor"]), "GET");

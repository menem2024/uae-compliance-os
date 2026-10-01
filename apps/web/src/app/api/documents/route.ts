import type { NextRequest } from "next/server";
import { withQuery } from "@/lib/bff-paths";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: GET /api/documents -> api-go GET /v1/documents. */
export const GET = (req: NextRequest) =>
  proxyJson(req, withQuery("/v1/documents", req.nextUrl.searchParams, ["client_company_id", "status", "limit", "cursor"]), "GET");

import type { NextRequest } from "next/server";
import { withQuery } from "@/lib/bff-paths";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: GET (list) and POST (create) /api/client-companies -> api-go /v1/client-companies. */
export const GET = (req: NextRequest) =>
  proxyJson(req, withQuery("/v1/client-companies", req.nextUrl.searchParams, ["status", "q", "limit", "cursor"]), "GET");
export const POST = (req: NextRequest) => proxyJson(req, "/v1/client-companies", "POST");

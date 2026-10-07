import type { NextRequest } from "next/server";
import { withQuery } from "@/lib/bff-paths";
import { forwardToApi } from "@/lib/bff";
import { getAccessToken } from "@/lib/server/access-token";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: GET /api/invoices -> api-go GET /v1/invoices (this Firm's invoices; allowed query params only). */
export const GET = (req: NextRequest) =>
  proxyJson(req, withQuery("/v1/invoices", req.nextUrl.searchParams, ["status", "q", "limit", "cursor"]), "GET");

/** BFF: POST /api/invoices -> api-go POST /v1/invoices (202 + X-Trace-Id passed through). */
export async function POST(req: NextRequest) {
  return forwardToApi({
    path: "/v1/invoices",
    method: "POST",
    body: await req.text(),
    accessToken: await getAccessToken(req.headers),
  });
}

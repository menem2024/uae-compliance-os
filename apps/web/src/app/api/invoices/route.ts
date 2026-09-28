import type { NextRequest } from "next/server";
import { forwardToApi } from "@/lib/bff";
import { getAccessToken } from "@/lib/server/access-token";

/** BFF: POST /api/invoices -> api-go POST /v1/invoices (202 + X-Trace-Id passed through). */
export async function POST(req: NextRequest) {
  return forwardToApi({
    path: "/v1/invoices",
    method: "POST",
    body: await req.text(),
    accessToken: await getAccessToken(req.headers),
  });
}

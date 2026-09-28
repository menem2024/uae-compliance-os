import type { NextRequest } from "next/server";
import { forwardToApi } from "@/lib/bff";
import { getAccessToken } from "@/lib/server/access-token";

/** BFF: GET /api/me -> api-go GET /v1/me. */
export async function GET(req: NextRequest) {
  return forwardToApi({ path: "/v1/me", method: "GET", accessToken: await getAccessToken(req.headers) });
}

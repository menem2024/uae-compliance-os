import type { NextRequest } from "next/server";
import { forwardToApi, invoicePath } from "@/lib/bff";
import { getAccessToken } from "@/lib/server/access-token";

/** BFF: GET /api/invoices/{id} -> api-go GET /v1/invoices/{id}. */
export async function GET(req: NextRequest, { params }: RouteContext<"/api/invoices/[id]">) {
  const { id } = await params;
  return forwardToApi({
    path: invoicePath(id),
    method: "GET",
    accessToken: await getAccessToken(req.headers),
  });
}

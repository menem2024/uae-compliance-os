import type { NextRequest } from "next/server";
import { forwardDownload } from "@/lib/bff-download";
import { exportXmlPath } from "@/lib/bff-paths";
import { getAccessToken } from "@/lib/server/access-token";

/** BFF: GET /api/exports/{id}/xml -> the PINT-AE XML as an attachment (bytes and disposition forwarded, not JSON). */
export async function GET(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  return forwardDownload({ path: exportXmlPath((await params).id), accessToken: await getAccessToken(req.headers) });
}

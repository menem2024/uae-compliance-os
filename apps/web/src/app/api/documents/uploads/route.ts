import type { NextRequest } from "next/server";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: POST /api/documents/uploads -> api-go (presigned PUT URLs; the bytes never pass through here). */
export const POST = (req: NextRequest) => proxyJson(req, "/v1/documents/uploads", "POST");

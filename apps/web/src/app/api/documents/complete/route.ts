import type { NextRequest } from "next/server";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: POST /api/documents/complete -> api-go. */
export const POST = (req: NextRequest) => proxyJson(req, "/v1/documents/complete", "POST");

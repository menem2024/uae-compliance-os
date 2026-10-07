import type { NextRequest } from "next/server";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: POST /api/exports {invoice_id} -> api-go POST /v1/exports (201; needs a `ready` invoice). */
export const POST = (req: NextRequest) => proxyJson(req, "/v1/exports", "POST");

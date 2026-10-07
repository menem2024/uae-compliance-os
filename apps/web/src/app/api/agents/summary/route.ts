import type { NextRequest } from "next/server";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: GET /api/agents/summary. */
export const GET = (req: NextRequest) => proxyJson(req, "/v1/agents/summary", "GET");

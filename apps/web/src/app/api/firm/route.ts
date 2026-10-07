import type { NextRequest } from "next/server";
import { proxyJson } from "@/lib/server/bff-route";

/** BFF: GET/PATCH /api/firm -> api-go /v1/firm. */
export const GET = (req: NextRequest) => proxyJson(req, "/v1/firm", "GET");
export const PATCH = (req: NextRequest) => proxyJson(req, "/v1/firm", "PATCH");

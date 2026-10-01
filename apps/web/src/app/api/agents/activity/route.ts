import type { NextRequest } from "next/server";
import { forwardEventStream } from "@/lib/bff-stream";
import { getAccessToken } from "@/lib/server/access-token";

// A streamed body must not be cached or statically optimised, and needs the Node runtime (spec 11).
export const dynamic = "force-dynamic";
export const runtime = "nodejs";

/** BFF: GET /api/agents/activity -> api-go SSE stream "نشاط الوكلاء" (per-Firm agent events). */
export async function GET(req: NextRequest) {
  return forwardEventStream({
    path: "/v1/agents/activity",
    accessToken: await getAccessToken(req.headers),
    lastEventId: req.headers.get("last-event-id") ?? req.nextUrl.searchParams.get("last_event_id"),
    signal: req.signal,
  });
}

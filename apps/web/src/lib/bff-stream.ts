import { buildApiRequest } from "./api";

type StreamInit = {
  /** Must start with `/v1/` (buildApiRequest enforces it). */
  path: string;
  accessToken: string | undefined;
  /** The browser's Last-Event-ID; forwarded only when it has the api-go id shape. */
  lastEventId?: string | null;
  /** The incoming request's signal: a closed browser tab aborts the upstream stream. */
  signal?: AbortSignal;
};

/** api-go SSE event ids are `<unix_ms>-<uuid>` (spec section 5.4). */
const EVENT_ID = /^[0-9]{1,16}-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

export const SSE_HEADERS: Readonly<Record<string, string>> = {
  "content-type": "text/event-stream; charset=utf-8",
  "cache-control": "no-cache, no-transform",
  "x-accel-buffering": "no",
};

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json", "cache-control": "no-store" } });

/**
 * BFF for a server-sent event stream: opens the upstream stream with the caller's bearer token and pipes
 * its body through unbuffered. Upstream errors pass through as JSON with their status (e.g. 429
 * too_many_streams); only the SSE headers above are set, so upstream headers never leak.
 */
export async function forwardEventStream(
  init: StreamInit,
  fetchImpl: (req: Request) => Promise<Response> = fetch,
): Promise<Response> {
  if (!init.accessToken) return json(401, { error: "unauthenticated" });
  const base = buildApiRequest(init.path, { method: "GET", accessToken: init.accessToken });
  const headers = new Headers(base.headers);
  headers.set("accept", "text/event-stream");
  if (init.lastEventId && EVENT_ID.test(init.lastEventId)) headers.set("last-event-id", init.lastEventId);

  let upstream: Response;
  try {
    upstream = await fetchImpl(new Request(base.url, { method: "GET", headers, signal: init.signal }));
  } catch {
    return json(502, { error: "upstream_unavailable" });
  }
  if (upstream.status !== 200 || !upstream.body) {
    return new Response(upstream.body, {
      status: upstream.status === 200 ? 502 : upstream.status,
      headers: { "content-type": "application/json", "cache-control": "no-store" },
    });
  }
  return new Response(upstream.body, { status: 200, headers: SSE_HEADERS });
}

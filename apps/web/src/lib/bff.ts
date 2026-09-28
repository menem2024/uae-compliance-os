import { buildApiRequest } from "./api";

type ForwardInit = {
  /** Must start with `/v1/` (enforced by buildApiRequest: the SSRF guard). */
  path: string;
  method: "GET" | "POST";
  body?: string;
  /** Read server-side from the Auth.js JWT; never comes from the browser. */
  accessToken: string | undefined;
};

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** api-go resource path for one invoice; the id is encoded so it cannot escape the resource. */
export function invoicePath(id: string): string {
  return `/v1/invoices/${encodeURIComponent(id)}`;
}

/**
 * BFF core: forwards a request to api-go with the caller's bearer token and passes the
 * upstream status, JSON body and `X-Trace-Id` through. Only those are copied, so upstream
 * headers such as `set-cookie` never leak to the browser.
 */
export async function forwardToApi(
  init: ForwardInit,
  fetchImpl: (req: Request) => Promise<Response> = fetch,
): Promise<Response> {
  if (!init.accessToken) return json(401, { error: "unauthenticated" });
  const request = buildApiRequest(init.path, {
    method: init.method,
    body: init.body,
    accessToken: init.accessToken,
  });

  let upstream: Response;
  try {
    upstream = await fetchImpl(request);
  } catch {
    return json(502, { error: "upstream_unavailable" });
  }

  const headers = new Headers({ "content-type": "application/json", "cache-control": "no-store" });
  const traceId = upstream.headers.get("x-trace-id");
  if (traceId) headers.set("x-trace-id", traceId);
  return new Response(upstream.body, { status: upstream.status, headers });
}

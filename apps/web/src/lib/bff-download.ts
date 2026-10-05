import { buildApiRequest } from "./api";

type DownloadInit = {
  /** Must start with `/v1/` (enforced by buildApiRequest: the SSRF guard). */
  path: string;
  /** Read server-side from the Auth.js JWT; never comes from the browser. */
  accessToken: string | undefined;
};

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json", "cache-control": "no-store" } });

/**
 * BFF for a file download (the exported PINT-AE XML): a bodyless GET with the caller's bearer token whose
 * bytes stream through untouched. Only the content type and `content-disposition` of a success are copied
 * (plus `x-trace-id`), so upstream headers such as `set-cookie` never leak. An upstream error is passed
 * through as JSON with its status, never with a download disposition.
 */
export async function forwardDownload(
  init: DownloadInit,
  fetchImpl: (req: Request) => Promise<Response> = fetch,
): Promise<Response> {
  if (!init.accessToken) return json(401, { error: "unauthenticated" });
  const request = buildApiRequest(init.path, { method: "GET", accessToken: init.accessToken });

  let upstream: Response;
  try {
    upstream = await fetchImpl(request);
  } catch {
    return json(502, { error: "upstream_unavailable" });
  }

  const headers = new Headers({ "cache-control": "no-store" });
  const traceId = upstream.headers.get("x-trace-id");
  if (traceId) headers.set("x-trace-id", traceId);
  if (!upstream.ok) {
    headers.set("content-type", "application/json");
    return new Response(upstream.body, { status: upstream.status, headers });
  }
  headers.set("content-type", upstream.headers.get("content-type") ?? "application/octet-stream");
  const disposition = upstream.headers.get("content-disposition");
  if (disposition) headers.set("content-disposition", disposition);
  headers.set("x-content-type-options", "nosniff");
  return new Response(upstream.body, { status: upstream.status, headers });
}

/**
 * Builds a server-side request to api-go (the only backend the web tier may reach).
 * Used exclusively by BFF route handlers; the access token never reaches browser JS.
 */
export function buildApiRequest(
  path: string,
  init: { method: string; body?: string; accessToken: string },
): Request {
  if (!path.startsWith("/v1/")) throw new Error(`refusing non-/v1 path: ${path}`);
  const base = process.env.API_URL;
  if (!base) throw new Error("API_URL not set");
  const headers = new Headers({ authorization: `Bearer ${init.accessToken}` });
  if (init.body !== undefined) headers.set("content-type", "application/json");
  return new Request(new URL(path, base), { method: init.method, body: init.body, headers });
}

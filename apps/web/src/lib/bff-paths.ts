/** api-go resource paths for Track B BFF routes. Ids are encoded so they cannot escape the resource. */

const seg = (s: string) => encodeURIComponent(s);

export const clientCompanyPath = (id: string, action?: "archive" | "restore"): string =>
  `/v1/client-companies/${seg(id)}${action ? `/${action}` : ""}`;

export const documentPath = (id: string, action?: "download" | "reprocess"): string =>
  `/v1/documents/${seg(id)}${action ? `/${action}` : ""}`;

export const runPath = (id: string): string => `/v1/agents/runs/${seg(id)}`;

/** Copies only the allowed query parameters (first value each) onto path; everything else is dropped. */
export function withQuery(path: string, search: URLSearchParams, allowed: readonly string[]): string {
  const out = new URLSearchParams();
  for (const key of allowed) {
    const v = search.get(key);
    if (v !== null && v !== "") out.set(key, v);
  }
  const qs = out.toString();
  return qs ? `${path}?${qs}` : path;
}

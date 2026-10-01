/** Browser-side JSON calls to the BFF (`/api/*`). Server errors become ApiError with the api-go code. */

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly traceId: string | null,
  ) {
    super(code);
    this.name = "ApiError";
  }
}

type RequestInitJson = { method?: "GET" | "POST" | "PATCH"; json?: unknown; signal?: AbortSignal };

export async function requestJson<T>(
  url: string,
  init: RequestInitJson = {},
  fetchImpl: typeof fetch = fetch,
): Promise<T> {
  const headers = new Headers();
  if (init.json !== undefined) headers.set("content-type", "application/json");
  const res = await fetchImpl(url, {
    method: init.method ?? "GET",
    headers,
    body: init.json === undefined ? undefined : JSON.stringify(init.json),
    cache: "no-store",
    signal: init.signal,
  });
  const text = await res.text();
  if (!res.ok) {
    let code = `http_${res.status}`;
    try {
      const body = JSON.parse(text) as { error?: unknown };
      if (typeof body.error === "string") code = body.error;
    } catch {
      // not JSON: keep the generic code
    }
    throw new ApiError(res.status, code, res.headers.get("x-trace-id"));
  }
  return (text ? JSON.parse(text) : null) as T;
}

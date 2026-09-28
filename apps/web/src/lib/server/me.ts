import { headers } from "next/headers";
import { buildApiRequest } from "@/lib/api";
import { parseMe, type Me } from "@/lib/me";
import { getAccessToken } from "./access-token";

/** Server-side `GET /v1/me` for the current session; null when signed out or api-go fails. */
export async function fetchMe(): Promise<Me | null> {
  const accessToken = await getAccessToken(await headers());
  if (!accessToken) return null;
  try {
    const res = await fetch(buildApiRequest("/v1/me", { method: "GET", accessToken }), {
      cache: "no-store",
      signal: AbortSignal.timeout(3000),
    });
    if (!res.ok) {
      console.warn(`GET /v1/me -> ${res.status} (trace ${res.headers.get("x-trace-id") ?? "-"})`);
      return null;
    }
    return parseMe(await res.json());
  } catch (err) {
    console.warn("GET /v1/me failed", err instanceof Error ? err.message : err);
    return null;
  }
}

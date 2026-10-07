import type { NextRequest } from "next/server";
import { forwardToApi } from "@/lib/bff";
import { getAccessToken } from "@/lib/server/access-token";

/**
 * One BFF route handler body: forwards `req` to the api-go `path` with the caller's token. A GET never
 * forwards a body; an empty POST body is sent as no body.
 */
export async function proxyJson(req: NextRequest, path: string, method: "GET" | "POST" | "PATCH"): Promise<Response> {
  const text = method === "GET" ? "" : await req.text();
  return forwardToApi({
    path,
    method,
    body: text === "" ? undefined : text,
    accessToken: await getAccessToken(req.headers),
  });
}

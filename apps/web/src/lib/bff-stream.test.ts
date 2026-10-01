import { beforeEach, describe, expect, it, vi } from "vitest";
import { SSE_HEADERS, forwardEventStream } from "./bff-stream";

const ID = "1759140000000-6f1c9a52-6c55-4c2e-9d0f-3b7f0b1f5e21";

describe("forwardEventStream", () => {
  beforeEach(() => {
    process.env.API_URL = "http://api-go:8080";
  });

  it("returns 401 without calling upstream when there is no access token", async () => {
    const fetchImpl = vi.fn();
    const res = await forwardEventStream({ path: "/v1/agents/activity", accessToken: undefined }, fetchImpl);
    expect(res.status).toBe(401);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("forwards the token, accept header, a well-formed Last-Event-ID and the abort signal", async () => {
    const fetchImpl = vi.fn<(req: Request) => Promise<Response>>(
      async () => new Response("data: x\n\n", { status: 200, headers: { "set-cookie": "leak=1" } }),
    );
    const ac = new AbortController();
    const res = await forwardEventStream(
      { path: "/v1/agents/activity", accessToken: "tok", lastEventId: ID, signal: ac.signal },
      fetchImpl,
    );
    const req = fetchImpl.mock.calls[0][0];
    expect(req.url).toBe("http://api-go:8080/v1/agents/activity");
    expect(req.headers.get("authorization")).toBe("Bearer tok");
    expect(req.headers.get("accept")).toBe("text/event-stream");
    expect(req.headers.get("last-event-id")).toBe(ID);
    ac.abort();
    expect(req.signal.aborted).toBe(true);
    expect(res.status).toBe(200);
    for (const [k, v] of Object.entries(SSE_HEADERS)) expect(res.headers.get(k)).toBe(v);
    expect(res.headers.get("set-cookie")).toBeNull();
    expect(await res.text()).toBe("data: x\n\n");
  });

  it("drops a Last-Event-ID that does not have the api-go shape", async () => {
    const fetchImpl = vi.fn<(req: Request) => Promise<Response>>(async () => new Response("", { status: 200 }));
    await forwardEventStream(
      { path: "/v1/agents/activity", accessToken: "t", lastEventId: "1\r\nx-evil: 1" },
      fetchImpl,
    );
    expect(fetchImpl.mock.calls[0][0].headers.get("last-event-id")).toBeNull();
  });

  it("passes upstream errors through as JSON and maps a failed connect to 502", async () => {
    const tooMany = vi.fn(async () => new Response('{"error":"too_many_streams"}', { status: 429 }));
    const res = await forwardEventStream({ path: "/v1/agents/activity", accessToken: "t" }, tooMany);
    expect(res.status).toBe(429);
    expect(res.headers.get("content-type")).toBe("application/json");
    expect(await res.json()).toEqual({ error: "too_many_streams" });

    const down = vi.fn(async () => {
      throw new TypeError("fetch failed");
    });
    const res2 = await forwardEventStream({ path: "/v1/agents/activity", accessToken: "t" }, down);
    expect(res2.status).toBe(502);
    expect(await res2.json()).toEqual({ error: "upstream_unavailable" });
  });
});

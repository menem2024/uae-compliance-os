import { beforeEach, describe, expect, it, vi } from "vitest";
import { forwardDownload } from "./bff-download";

describe("forwardDownload", () => {
  beforeEach(() => {
    process.env.API_URL = "http://api-go:8080";
  });

  it("returns 401 without calling upstream when there is no access token", async () => {
    const fetchImpl = vi.fn();
    const res = await forwardDownload({ path: "/v1/exports/e1/xml", accessToken: undefined }, fetchImpl);
    expect(res.status).toBe(401);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("sends the bearer token with a bodyless GET and streams the bytes with the download headers", async () => {
    const fetchImpl = vi.fn<(req: Request) => Promise<Response>>(
      async () =>
        new Response('<?xml version="1.0"?><Invoice/>', {
          status: 200,
          headers: {
            "content-type": "application/xml",
            "content-disposition": 'attachment; filename="inv.xml"',
            "x-trace-id": "abc",
            "set-cookie": "leak=1",
          },
        }),
    );
    const res = await forwardDownload({ path: "/v1/exports/e1/xml", accessToken: "tok" }, fetchImpl);
    const req = fetchImpl.mock.calls[0][0];
    expect(req.method).toBe("GET");
    expect(req.headers.get("authorization")).toBe("Bearer tok");
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("application/xml");
    expect(res.headers.get("content-disposition")).toBe('attachment; filename="inv.xml"');
    expect(res.headers.get("x-content-type-options")).toBe("nosniff");
    expect(res.headers.get("cache-control")).toBe("no-store");
    expect(res.headers.get("x-trace-id")).toBe("abc");
    expect(res.headers.get("set-cookie")).toBeNull();
    expect(await res.text()).toBe('<?xml version="1.0"?><Invoice/>');
  });

  it("passes an upstream error through as JSON without a download disposition", async () => {
    const fetchImpl = vi.fn(async () => new Response('{"error":"not_found"}', { status: 404 }));
    const res = await forwardDownload({ path: "/v1/exports/e1/xml", accessToken: "t" }, fetchImpl);
    expect(res.status).toBe(404);
    expect(res.headers.get("content-disposition")).toBeNull();
    expect(res.headers.get("content-type")).toBe("application/json");
    expect(await res.json()).toEqual({ error: "not_found" });
  });

  it("returns 502 when api-go is unreachable", async () => {
    const fetchImpl = vi.fn(async () => {
      throw new TypeError("fetch failed");
    });
    const res = await forwardDownload({ path: "/v1/exports/e1/xml", accessToken: "t" }, fetchImpl);
    expect(res.status).toBe(502);
  });
});

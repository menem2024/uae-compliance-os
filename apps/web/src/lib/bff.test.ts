import { beforeEach, describe, expect, it, vi } from "vitest";
import { forwardToApi } from "./bff";

describe("forwardToApi", () => {
  beforeEach(() => {
    process.env.API_URL = "http://api-go:8080";
  });

  it("returns 401 without calling upstream when there is no access token", async () => {
    const fetchImpl = vi.fn();
    const res = await forwardToApi({ path: "/v1/me", method: "GET", accessToken: undefined }, fetchImpl);
    expect(res.status).toBe(401);
    expect(await res.json()).toEqual({ error: "unauthenticated" });
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("forwards method, body and bearer token to api-go", async () => {
    const fetchImpl = vi.fn<(req: Request) => Promise<Response>>(async () => new Response("{}", { status: 202 }));
    await forwardToApi(
      { path: "/v1/invoices", method: "POST", body: '{"a":"1"}', accessToken: "tok" },
      fetchImpl,
    );
    const req = fetchImpl.mock.calls[0][0];
    expect(req.url).toBe("http://api-go:8080/v1/invoices");
    expect(req.method).toBe("POST");
    expect(req.headers.get("authorization")).toBe("Bearer tok");
    expect(await req.text()).toBe('{"a":"1"}');
  });

  it("forwards PATCH with its JSON body", async () => {
    const fetchImpl = vi.fn<(req: Request) => Promise<Response>>(async () => new Response("{}", { status: 200 }));
    await forwardToApi({ path: "/v1/firm", method: "PATCH", body: '{"name":"A"}', accessToken: "tok" }, fetchImpl);
    const req = fetchImpl.mock.calls[0][0];
    expect(req.method).toBe("PATCH");
    expect(req.headers.get("content-type")).toBe("application/json");
    expect(await req.text()).toBe('{"name":"A"}');
  });

  it("passes through upstream status, body and X-Trace-Id", async () => {
    const fetchImpl = vi.fn(
      async () =>
        new Response('{"id":"inv-1","status":"uploaded"}', {
          status: 202,
          headers: { "x-trace-id": "4bf92f3577b34da6a3ce929d0e0e4736", "set-cookie": "leak=1" },
        }),
    );
    const res = await forwardToApi({ path: "/v1/invoices", method: "POST", body: "{}", accessToken: "t" }, fetchImpl);
    expect(res.status).toBe(202);
    expect(res.headers.get("x-trace-id")).toBe("4bf92f3577b34da6a3ce929d0e0e4736");
    expect(res.headers.get("content-type")).toBe("application/json");
    expect(res.headers.get("set-cookie")).toBeNull();
    expect(await res.json()).toEqual({ id: "inv-1", status: "uploaded" });
  });

  it("passes through error statuses unchanged", async () => {
    for (const status of [400, 401, 403, 404, 429]) {
      const fetchImpl = vi.fn(async () => new Response('{"error":"x"}', { status }));
      const res = await forwardToApi({ path: "/v1/invoices/abc", method: "GET", accessToken: "t" }, fetchImpl);
      expect(res.status).toBe(status);
      expect(res.headers.get("x-trace-id")).toBeNull();
    }
  });

  it("returns 502 when api-go is unreachable", async () => {
    const fetchImpl = vi.fn(async () => {
      throw new TypeError("fetch failed");
    });
    const res = await forwardToApi({ path: "/v1/me", method: "GET", accessToken: "t" }, fetchImpl);
    expect(res.status).toBe(502);
    expect(await res.json()).toEqual({ error: "upstream_unavailable" });
  });

  it("refuses paths outside /v1 (SSRF guard)", async () => {
    const fetchImpl = vi.fn();
    await expect(
      forwardToApi({ path: "http://evil/x", method: "GET", accessToken: "t" }, fetchImpl),
    ).rejects.toThrow();
    expect(fetchImpl).not.toHaveBeenCalled();
  });
});

describe("invoicePath", () => {
  it("encodes the id so it cannot escape the invoice resource", async () => {
    const { invoicePath } = await import("./bff");
    expect(invoicePath("abc-123")).toBe("/v1/invoices/abc-123");
    expect(invoicePath("../me")).toBe("/v1/invoices/..%2Fme");
    expect(invoicePath("a?b#c")).toBe("/v1/invoices/a%3Fb%23c");
  });
});

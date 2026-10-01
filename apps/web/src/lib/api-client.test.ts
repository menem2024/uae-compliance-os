import { describe, expect, it, vi } from "vitest";
import { ApiError, requestJson } from "./api-client";

describe("requestJson", () => {
  it("sends JSON and returns the parsed body", async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => new Response('{"id":"c1"}', { status: 201 }));
    const out = await requestJson<{ id: string }>("/api/client-companies", { method: "POST", json: { name: "A" } }, fetchImpl);
    expect(out).toEqual({ id: "c1" });
    const [url, init] = fetchImpl.mock.calls[0];
    expect(url).toBe("/api/client-companies");
    expect(init?.method).toBe("POST");
    expect(new Headers(init?.headers).get("content-type")).toBe("application/json");
    expect(init?.body).toBe('{"name":"A"}');
    expect(init?.cache).toBe("no-store");
  });

  it("throws ApiError with the server code, status and trace id", async () => {
    const fetchImpl = vi.fn<typeof fetch>(
      async () => new Response('{"error":"trn_taken"}', { status: 409, headers: { "x-trace-id": "abc" } }),
    );
    const err = await requestJson("/api/client-companies", { method: "POST", json: {} }, fetchImpl).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 409, code: "trn_taken", traceId: "abc" });
  });

  it("uses a generic code when the error body is not JSON, and handles 202/204 without a body", async () => {
    const bad = vi.fn<typeof fetch>(async () => new Response("<html>", { status: 502 }));
    await expect(requestJson("/api/x", {}, bad)).rejects.toMatchObject({ code: "http_502" });
    const empty = vi.fn<typeof fetch>(async () => new Response(null, { status: 202 }));
    await expect(requestJson("/api/x", { method: "POST" }, empty)).resolves.toBeNull();
  });
});

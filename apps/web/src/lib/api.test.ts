import { describe, expect, it, beforeEach } from "vitest";
import { buildApiRequest } from "./api";

describe("buildApiRequest", () => {
  beforeEach(() => { process.env.API_URL = "http://api-go:8080"; });

  it("targets api-go with bearer token and json", () => {
    const r = buildApiRequest("/v1/invoices", { method: "POST", body: "{}", accessToken: "tok" });
    expect(r.url).toBe("http://api-go:8080/v1/invoices");
    expect(r.headers.get("authorization")).toBe("Bearer tok");
    expect(r.headers.get("content-type")).toBe("application/json");
  });

  it("rejects paths that escape /v1", () => {
    expect(() => buildApiRequest("http://evil/x", { method: "GET", accessToken: "t" })).toThrow();
    expect(() => buildApiRequest("/admin", { method: "GET", accessToken: "t" })).toThrow();
  });
});

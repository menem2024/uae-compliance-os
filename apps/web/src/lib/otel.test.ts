import { describe, expect, it } from "vitest";
import { propagateContextUrls } from "./otel";

const matches = (res: RegExp[], url: string) => res.some((r) => r.test(url));

describe("propagateContextUrls", () => {
  it("always covers the compose and local api-go hosts", () => {
    const res = propagateContextUrls(undefined);
    expect(matches(res, "http://api-go:8080/v1/invoices")).toBe(true);
    expect(matches(res, "http://localhost:8080/v1/me")).toBe(true);
  });
  it("adds the configured API_URL host, escaped", () => {
    const res = propagateContextUrls("http://api.internal.example:9000");
    expect(matches(res, "http://api.internal.example:9000/v1/me")).toBe(true);
    expect(matches(res, "http://apixinternalxexample:9000/v1/me")).toBe(false);
  });
  it("never propagates trace context to Zitadel or other third parties", () => {
    const res = propagateContextUrls("http://api-go:8080");
    expect(matches(res, "http://zitadel.localhost:8085/oauth/v2/token")).toBe(false);
    expect(matches(res, "https://fonts.googleapis.com/css2")).toBe(false);
  });
  it("ignores a malformed API_URL", () => {
    expect(() => propagateContextUrls("not a url")).not.toThrow();
  });
});

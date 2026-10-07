import { describe, expect, it } from "vitest";
import { clientCompanyPath, documentPath, exportXmlPath, invoiceValidationPath, runPath, withQuery } from "./bff-paths";

describe("bff paths", () => {
  it("encode ids so they cannot escape the resource", () => {
    expect(clientCompanyPath("abc")).toBe("/v1/client-companies/abc");
    expect(clientCompanyPath("../firm", "archive")).toBe("/v1/client-companies/..%2Ffirm/archive");
    expect(documentPath("a?b", "download")).toBe("/v1/documents/a%3Fb/download");
    expect(documentPath("d1", "reprocess")).toBe("/v1/documents/d1/reprocess");
    expect(runPath("r#1")).toBe("/v1/agents/runs/r%231");
  });

  it("copy only allowed, non-empty query parameters (first value each)", () => {
    const search = new URLSearchParams("status=all&q=oasis&q=x&evil=1&cursor=&limit=20");
    expect(withQuery("/v1/client-companies", search, ["status", "q", "limit", "cursor"])).toBe(
      "/v1/client-companies?status=all&q=oasis&limit=20",
    );
    expect(withQuery("/v1/documents", new URLSearchParams("evil=1"), ["status"])).toBe("/v1/documents");
  });

  it("build the invoice review and export paths with encoded ids", () => {
    expect(invoiceValidationPath("i1")).toBe("/v1/invoices/i1/validation");
    expect(invoiceValidationPath("i1", "corrections")).toBe("/v1/invoices/i1/validation/corrections");
    expect(invoiceValidationPath("../x", "approve")).toBe("/v1/invoices/..%2Fx/validation/approve");
    expect(invoiceValidationPath("i1", "audit")).toBe("/v1/invoices/i1/validation/audit");
    expect(exportXmlPath("e/1")).toBe("/v1/exports/e%2F1/xml");
  });
});

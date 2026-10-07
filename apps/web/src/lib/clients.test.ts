import { describe, expect, it } from "vitest";
import {
  type ClientCompany,
  displayName,
  errorField,
  formFrom,
  normalizeTrnInput,
  toCreateBody,
  toPatchBody,
  validateClientForm,
} from "./clients";

const base: ClientCompany = {
  id: "c1",
  name: "Oasis Trading LLC",
  name_ar: "شركة الواحة",
  trn: "100123456789003",
  emirate: "DXB",
  status: "active",
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
};

describe("client company form", () => {
  it("normalises Arabic-Indic digits, spaces and hyphens in a TRN", () => {
    expect(normalizeTrnInput("١٠٠ ١٢٣-٤٥٦٧٨٩٠٠٣")).toBe("100123456789003");
    expect(normalizeTrnInput("۱۰۰۱۲۳۴۵۶۷۸۹۰۰۳")).toBe("100123456789003");
  });

  it("validates like api-go clients.Merge", () => {
    expect(validateClientForm({ name: " ", nameAr: "", trn: "", emirate: "" })).toEqual({ name: "invalid_name" });
    expect(validateClientForm({ name: "A", nameAr: "", trn: "12345", emirate: "" })).toEqual({ trn: "invalid_trn" });
    expect(validateClientForm({ name: "A".repeat(201), nameAr: "ب".repeat(201), trn: "", emirate: "" })).toEqual({
      name: "invalid_name",
      nameAr: "invalid_name_ar",
    });
    expect(validateClientForm({ name: "A", nameAr: "", trn: "١٠٠١٢٣٤٥٦٧٨٩٠٠٣", emirate: "SHJ" })).toEqual({});
  });

  it("builds a create body without empty optional fields", () => {
    expect(toCreateBody({ name: "  Oasis \n Trading ", nameAr: "", trn: "", emirate: "" })).toEqual({
      name: "Oasis Trading",
    });
    expect(toCreateBody({ name: "A", nameAr: "ب", trn: "100-123456789003", emirate: "AUH" })).toEqual({
      name: "A",
      name_ar: "ب",
      trn: "100123456789003",
      emirate: "AUH",
    });
  });

  it("builds a patch body with only changed fields and clears emptied ones with an empty string", () => {
    expect(toPatchBody(base, formFrom(base))).toEqual({});
    expect(toPatchBody(base, { ...formFrom(base), trn: "", emirate: "" })).toEqual({ trn: "", emirate: "" });
    expect(toPatchBody(base, { ...formFrom(base), name: "Oasis  Trading  LLC" })).toEqual({});
    expect(toPatchBody(base, { ...formFrom(base), name: "Oasis FZE" })).toEqual({ name: "Oasis FZE" });
  });

  it("maps server error codes to fields", () => {
    expect(errorField("trn_taken")).toBe("trn");
    expect(errorField("invalid_name_ar")).toBe("nameAr");
    expect(errorField("internal")).toBeNull();
  });

  it("picks the display name for the locale", () => {
    expect(displayName(base, "ar")).toBe("شركة الواحة");
    expect(displayName(base, "en")).toBe("Oasis Trading LLC");
    expect(displayName({ name: "X", name_ar: "" }, "ar")).toBe("X");
  });
});

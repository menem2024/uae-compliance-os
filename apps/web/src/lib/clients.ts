/** ClientCompany form logic (mirrors api-go `clients.Merge`; the server stays authoritative). */

export const EMIRATES = ["AUH", "DXB", "SHJ", "UAQ", "FUJ", "AJM", "RAK"] as const;
export type Emirate = (typeof EMIRATES)[number];

export type ClientCompany = {
  id: string;
  name: string;
  name_ar: string;
  trn: string | null;
  emirate: Emirate | null;
  status: "active" | "archived";
  documents_total?: number;
  documents_needs_review?: number;
  created_at: string;
  updated_at: string;
};

export type ClientForm = { name: string; nameAr: string; trn: string; emirate: "" | Emirate };
export type ClientField = keyof ClientForm;
export type FieldError = "invalid_name" | "invalid_name_ar" | "invalid_trn" | "invalid_emirate" | "trn_taken";

const ARABIC_DIGITS = /[٠-٩۰-۹]/g;

/** Arabic-Indic and Eastern Arabic-Indic digits to Western; spaces and hyphens removed. */
export function normalizeTrnInput(s: string): string {
  return s
    .replace(ARABIC_DIGITS, (d) => String((d.charCodeAt(0) & 0xf) % 10))
    .replace(/[\s-]/g, "");
}

const clean = (s: string) => s.replace(/\p{Cc}/gu, "").replace(/\s+/g, " ").trim();

export function formFrom(c: ClientCompany): ClientForm {
  return { name: c.name, nameAr: c.name_ar, trn: c.trn ?? "", emirate: c.emirate ?? "" };
}

export function validateClientForm(f: ClientForm): Partial<Record<ClientField, FieldError>> {
  const errors: Partial<Record<ClientField, FieldError>> = {};
  const name = clean(f.name);
  if (name === "" || [...name].length > 200) errors.name = "invalid_name";
  if ([...clean(f.nameAr)].length > 200) errors.nameAr = "invalid_name_ar";
  const trn = normalizeTrnInput(f.trn);
  if (trn !== "" && !/^[0-9]{15}$/.test(trn)) errors.trn = "invalid_trn";
  if (f.emirate !== "" && !EMIRATES.includes(f.emirate)) errors.emirate = "invalid_emirate";
  return errors;
}

/** POST body: optional fields omitted when empty. */
export function toCreateBody(f: ClientForm): Record<string, string> {
  const body: Record<string, string> = { name: clean(f.name) };
  if (clean(f.nameAr)) body.name_ar = clean(f.nameAr);
  if (normalizeTrnInput(f.trn)) body.trn = normalizeTrnInput(f.trn);
  if (f.emirate) body.emirate = f.emirate;
  return body;
}

/** PATCH body: only changed fields; an emptied optional field is sent as "" (clears it). */
export function toPatchBody(orig: ClientCompany, f: ClientForm): Record<string, string> {
  const before = formFrom(orig);
  const body: Record<string, string> = {};
  if (clean(f.name) !== before.name) body.name = clean(f.name);
  if (clean(f.nameAr) !== before.nameAr) body.name_ar = clean(f.nameAr);
  if (normalizeTrnInput(f.trn) !== before.trn) body.trn = normalizeTrnInput(f.trn);
  if (f.emirate !== before.emirate) body.emirate = f.emirate;
  return body;
}

/** The form field a server error code belongs to (null = show as a form-level error). */
export function errorField(code: string): ClientField | null {
  switch (code) {
    case "invalid_name":
      return "name";
    case "invalid_name_ar":
      return "nameAr";
    case "invalid_trn":
    case "trn_taken":
      return "trn";
    case "invalid_emirate":
      return "emirate";
    default:
      return null;
  }
}

/** Display name for the current locale, falling back to the other language. */
export function displayName(c: Pick<ClientCompany, "name" | "name_ar">, locale: string): string {
  return locale === "ar" && c.name_ar ? c.name_ar : c.name;
}

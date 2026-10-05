import type { CorrectionBody } from "./types";

const SEGMENT = /^([a-z][a-z0-9_]*)(?:\[([0-9]+)\])?$/;

/**
 * The value of a string/bool field of the canonical invoice payload by api-go's field path
 * (`seller.postal_address.city`, `lines[0].item.name`), as the server reads it: "" when the field or a parent
 * is absent. Used as the `old_value` guard of a correction, so it must agree with the server's `fieldpath.Get`.
 */
export function valueAtPath(payload: unknown, path: string): string {
  if (path === "") return "";
  let cur: unknown = payload;
  for (const part of path.split(".")) {
    const m = SEGMENT.exec(part);
    if (!m || typeof cur !== "object" || cur === null || Array.isArray(cur)) return "";
    cur = (cur as Record<string, unknown>)[m[1]];
    if (m[2] !== undefined) {
      if (!Array.isArray(cur)) return "";
      cur = cur[Number(m[2])];
    }
    if (cur === undefined || cur === null) return "";
  }
  return typeof cur === "string" || typeof cur === "number" || typeof cur === "boolean" ? String(cur) : "";
}

/**
 * `POST /v1/invoices/{id}/validation/corrections` body for setting one field. `null` when there is nothing
 * valid to send (blank path, no change, or a payload version below 1).
 */
export function buildCorrection(input: {
  payload: unknown;
  payloadVersion: number;
  path: string;
  value: string;
  reason: string;
}): CorrectionBody | null {
  const path = input.path.trim();
  if (path === "" || !Number.isInteger(input.payloadVersion) || input.payloadVersion < 1) return null;
  const old_value = valueAtPath(input.payload, path);
  if (old_value === input.value) return null;
  return {
    payload_version: input.payloadVersion,
    changes: [{ path, old_value, new_value: input.value }],
    reason: input.reason,
  };
}

import type { ShellFirm } from "@/components/shell/types";

/** api-go `GET /v1/me` response. */
export type Me = {
  firm_id: string;
  firm_name: string;
  /** `#RRGGBB` or null for the platform default accent. */
  brand_color: string | null;
};

const HEX = /^#[0-9A-Fa-f]{6}$/;

/** Validates the /v1/me payload; a malformed brand colour is dropped (CSS injection guard). */
export function parseMe(data: unknown): Me | null {
  if (typeof data !== "object" || data === null) return null;
  const d = data as Record<string, unknown>;
  if (typeof d.firm_id !== "string" || typeof d.firm_name !== "string") return null;
  const brand = typeof d.brand_color === "string" && HEX.test(d.brand_color) ? d.brand_color : null;
  return { firm_id: d.firm_id, firm_name: d.firm_name, brand_color: brand };
}

/** Maps /v1/me onto the shell Firm; a neutral Firm when api-go could not answer. */
export function toShellFirm(me: Me | null, fallbackName: string): ShellFirm {
  return me ? { name: me.firm_name, brandColor: me.brand_color } : { name: fallbackName, brandColor: null };
}

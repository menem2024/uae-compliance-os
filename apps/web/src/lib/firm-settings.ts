/** Firm settings form logic (api-go PATCH /v1/firm). */

export type FirmSettings = { id: string; name: string; brand_color: string | null; updated_at: string };
export type FirmForm = { name: string; brandColor: string };
export type FirmPatch = { name?: string; brand_color?: string | null };

const HEX6 = /^#?([0-9A-Fa-f]{6})$/;

/** "#c8a45d" or "c8a45d" -> "#C8A45D"; anything else -> null. */
export function normalizeHex(input: string): string | null {
  const m = HEX6.exec(input.trim());
  return m ? `#${m[1].toUpperCase()}` : null;
}

export function firmForm(f: FirmSettings): FirmForm {
  return { name: f.name, brandColor: f.brand_color ?? "" };
}

/** "" = platform default accent (brand_color null). Returns null when the form is invalid. */
export function toFirmPatch(orig: FirmSettings, form: FirmForm): FirmPatch | null {
  const name = form.name.replace(/\s+/g, " ").trim();
  if (name === "" || [...name].length > 200) return null;
  const color = form.brandColor.trim() === "" ? null : normalizeHex(form.brandColor);
  if (form.brandColor.trim() !== "" && color === null) return null;
  const patch: FirmPatch = {};
  if (name !== orig.name) patch.name = name;
  if (color !== orig.brand_color) patch.brand_color = color;
  return patch;
}

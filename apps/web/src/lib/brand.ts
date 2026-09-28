/**
 * Per-Firm white-label accent (adr/016).
 *
 * A Firm's brand colour is untrusted input: it is validated as a strict 6-digit
 * hex (which also blocks CSS injection through the style attribute) and then
 * adjusted so text drawn in it meets WCAG AA (4.5:1) on the theme's panel colour.
 */

export type ThemeMode = "light" | "dark";
export type BrandVars = Record<"--brand" | "--brand-soft" | "--brand-ink" | "--brand-foreground", string>;

export const DEFAULT_BRAND = "#C8A45D";

const HEX = /^#[0-9A-Fa-f]{6}$/;
const AA = 4.5;
const STEP = 0.05;
/** Panel surface the brand ink is read against, per mode (matches globals.css --panel). */
const PANEL: Record<ThemeMode, string> = { light: "#FFFFFF", dark: "#12151A" };
/** Candidate text colours drawn on top of a solid brand fill. */
const ON_BRAND = ["#16110A", "#FFFFFF"] as const;

type Rgb = [number, number, number];

function toRgb(hex: string): Rgb {
  const n = Number.parseInt(hex.slice(1), 16);
  return [(n >> 16) & 0xff, (n >> 8) & 0xff, n & 0xff];
}

function toHex([r, g, b]: Rgb): string {
  return `#${[r, g, b].map((c) => c.toString(16).padStart(2, "0")).join("").toUpperCase()}`;
}

function channel(c: number): number {
  const s = c / 255;
  return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

/** WCAG 2.x relative luminance. */
function luminance(hex: string): number {
  const [r, g, b] = toRgb(hex);
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

/** WCAG 2.x contrast ratio between two #RRGGBB colours (1–21). */
export function contrastRatio(a: string, b: string): number {
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

function mix(from: Rgb, to: Rgb, t: number): Rgb {
  return from.map((c, i) => Math.round(c + (to[i] - c) * t)) as Rgb;
}

/** Mixes `hex` toward black (light) or white (dark) in 5% steps until it reads at AA on the panel. */
function inkFor(hex: string, mode: ThemeMode): string {
  const base = toRgb(hex);
  const target: Rgb = mode === "light" ? [0, 0, 0] : [255, 255, 255];
  for (let k = 0; k <= 1 / STEP; k++) {
    const candidate = toHex(mix(base, target, Math.min(1, k * STEP)));
    if (contrastRatio(candidate, PANEL[mode]) >= AA) return candidate;
  }
  return toHex(target);
}

/** CSS custom properties for a Firm's accent. `null` or malformed input yields the platform gold. */
export function brandVars(hex: string | null, mode: ThemeMode): BrandVars {
  const brand = hex && HEX.test(hex) ? hex.toUpperCase() : DEFAULT_BRAND;
  const [r, g, b] = toRgb(brand);
  const foreground = ON_BRAND.reduce((best, c) =>
    contrastRatio(c, brand) > contrastRatio(best, brand) ? c : best,
  );
  return {
    "--brand": brand,
    "--brand-soft": `rgb(${r} ${g} ${b} / .12)`,
    "--brand-ink": inkFor(brand, mode),
    "--brand-foreground": foreground,
  };
}

/**
 * Inline style for the element that scopes a Firm's brand (the <body>, so Radix
 * portals such as the command palette inherit it). Both modes' ink values are
 * emitted; globals.css picks `--brand-ink` from them according to the `.dark`
 * class, so switching theme never needs a re-render and never flashes.
 */
export function brandStyle(hex: string | null): Record<string, string> {
  const light = brandVars(hex, "light");
  const dark = brandVars(hex, "dark");
  return {
    "--brand": light["--brand"],
    "--brand-soft": light["--brand-soft"],
    "--brand-foreground": light["--brand-foreground"],
    "--brand-ink-light": light["--brand-ink"],
    "--brand-ink-dark": dark["--brand-ink"],
  };
}

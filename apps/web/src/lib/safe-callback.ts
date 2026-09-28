/**
 * Post-sign-in destination from a `callbackUrl` query param. Only same-origin absolute
 * paths are kept (no scheme, no protocol-relative `//`, no backslash tricks); anything
 * else returns to the locale home, so the sign-in flow cannot become an open redirect.
 */
export function safeCallbackPath(raw: string | string[] | null | undefined, locale: string): string {
  const value = Array.isArray(raw) ? raw[0] : raw;
  const fallback = `/${locale}`;
  if (!value || !value.startsWith("/") || value.startsWith("//")) return fallback;
  let decoded: string;
  try {
    decoded = decodeURIComponent(value);
  } catch {
    return fallback;
  }
  if (decoded.startsWith("//") || decoded.includes("\\") || /[\u0000-\u001f]/.test(decoded)) return fallback;
  return value;
}

/** Hosts api-go is reachable on (compose service name, local dev port). */
const DEFAULTS = [/^https?:\/\/api-go(:\d+)?\//, /^https?:\/\/localhost:8080\//];

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/**
 * URLs outgoing fetches add W3C `traceparent` to: api-go only (the configured API_URL
 * host plus the known defaults). Zitadel and third parties never receive trace context.
 */
export function propagateContextUrls(apiUrl: string | undefined): RegExp[] {
  const out = [...DEFAULTS];
  if (apiUrl) {
    try {
      const { protocol, host } = new URL(apiUrl);
      out.push(new RegExp(`^${escape(`${protocol}//${host}`)}/`));
    } catch {
      /* malformed API_URL: keep the defaults */
    }
  }
  return out;
}

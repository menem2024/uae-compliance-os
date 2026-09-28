/**
 * Grafana Tempo HTTP query client (grafana/otel-lgtm bundles Tempo behind
 * http://localhost:3200). Only what the smoke suite needs: turn a W3C trace id into the set
 * of `service.name` resource attributes that reported a span in that trace.
 *
 * Tempo's `/api/traces/{traceId}` JSON shape has moved between otel-lgtm/Tempo releases:
 * some builds return an OTLP-style `{ batches: [...] }` payload, others wrap OTLP resource
 * spans as `{ resourceSpans: [...] }` (optionally nested under a top-level `trace` key, e.g.
 * `{ trace: { resourceSpans: [...] } }`). This module tries every shape it has seen documented
 * for the pinned otel-lgtm version, so the suite does not need to be re-pinned to one exact
 * response format when the host runs it against the live stack.
 */

type Attribute = { key: string; value?: { stringValue?: string } };
type ResourceSpanLike = { resource?: { attributes?: Attribute[] } };

/** Pulls the array of resource-span-like objects out of whichever Tempo JSON shape appeared. */
function resourceSpansOf(body: unknown): ResourceSpanLike[] {
  if (!body || typeof body !== "object") return [];
  const root = body as Record<string, unknown>;
  const trace = (root.trace ?? root) as Record<string, unknown>;
  const candidates = [trace.batches, trace.resourceSpans, root.batches, root.resourceSpans];
  for (const candidate of candidates) {
    if (Array.isArray(candidate)) return candidate as ResourceSpanLike[];
  }
  return [];
}

/** Extracts every distinct `service.name` resource attribute present in a Tempo trace JSON body. */
export function serviceNamesOf(body: unknown): Set<string> {
  const names = new Set<string>();
  for (const rs of resourceSpansOf(body)) {
    for (const attr of rs.resource?.attributes ?? []) {
      if (attr.key === "service.name" && attr.value?.stringValue) names.add(attr.value.stringValue);
    }
  }
  return names;
}

export type PollOptions = {
  /** Total time to keep polling before giving up. Default 30s: Tempo/otel-lgtm batches spans. */
  timeoutMs?: number;
  /** Delay between polls. Default 2s. */
  intervalMs?: number;
  /** Stop as soon as this many distinct service names are seen. Default 4 (web/api-go/ai-py/validator-rs). */
  minServices?: number;
  tempoBaseUrl?: string;
};

/**
 * Polls Tempo for a trace until at least `minServices` distinct service names have reported a
 * span, or `timeoutMs` elapses. Throws with the best information gathered so far (last seen
 * service names, last HTTP/network error) rather than silently returning an incomplete set, so
 * a failure here points straight at what is missing.
 */
export async function serviceNamesInTrace(traceId: string, options: PollOptions = {}): Promise<Set<string>> {
  const { timeoutMs = 30_000, intervalMs = 2_000, minServices = 4, tempoBaseUrl = "http://localhost:3200" } = options;
  const deadline = Date.now() + timeoutMs;
  let names = new Set<string>();
  let lastError: string | undefined;

  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${tempoBaseUrl}/api/traces/${traceId}`);
      if (res.ok) {
        names = serviceNamesOf(await res.json());
        if (names.size >= minServices) return names;
        lastError = undefined;
      } else if (res.status !== 404) {
        // 404 just means Tempo hasn't ingested the trace yet; anything else is worth surfacing.
        lastError = `HTTP ${res.status} ${res.statusText}`;
      }
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
    }
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }

  const seen = names.size ? [...names].sort().join(", ") : "(none)";
  throw new Error(
    `Tempo trace ${traceId} did not reach ${minServices} distinct service names within ${timeoutMs}ms ` +
      `(queried ${tempoBaseUrl}/api/traces/${traceId}). Services seen: [${seen}].` +
      (lastError ? ` Last error: ${lastError}.` : ""),
  );
}

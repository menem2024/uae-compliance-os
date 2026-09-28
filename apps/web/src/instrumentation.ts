import { registerOTel } from "@vercel/otel";
import { propagateContextUrls } from "@/lib/otel";

/**
 * OpenTelemetry for the web tier (service.name "web"). The exporter is configured by env:
 * OTEL_EXPORTER_OTLP_ENDPOINT (e.g. http://otel-lgtm:4318) + OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf.
 * Outgoing fetches to api-go (and only api-go) carry W3C `traceparent`.
 */
export async function register() {
  registerOTel({
    serviceName: "web",
    instrumentationConfig: { fetch: { propagateContextUrls: propagateContextUrls(process.env.API_URL) } },
  });
  if (process.env.NEXT_RUNTIME === "nodejs") {
    const { installShutdownHooks } = await import("@/lib/server/shutdown");
    installShutdownHooks();
  }
}
